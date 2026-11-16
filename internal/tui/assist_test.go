package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pkarc/haro/core"
)

const fixedCfg = "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend web\n    bind *:80\n    default_backend nope\n\nbackend app\n    server a1 10.0.0.1:80 check\n"

// fakeAssist es un Options.Assist guionizado; recuerda lo que recibe.
type fakeAssist struct {
	mu    sync.Mutex
	calls [][]core.AssistMessage
	langs []core.Lang
	// chunks se entregan con onText; después devuelve out/err.
	chunks []string
	out    AssistOutcome
	err    error
	// block hace que espere hasta que se cancele el contexto (para probar Esc).
	block bool
}

func (f *fakeAssist) fn(ctx context.Context, msgs []core.AssistMessage, lang core.Lang, onText func(string)) (AssistOutcome, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]core.AssistMessage{}, msgs...))
	f.langs = append(f.langs, lang)
	f.mu.Unlock()
	for _, c := range f.chunks {
		onText(c)
	}
	if f.block {
		<-ctx.Done()
		return AssistOutcome{}, ctx.Err()
	}
	return f.out, f.err
}

func assistModelWith(t *testing.T, f *fakeAssist, save func([]core.FileChange) error) Model {
	t.Helper()
	m := New(Options{Load: loader(fixedCfg), NoWatch: true, Editor: "vi", Assist: f.fn, Save: save, AssistModel: "claude-test", Lang: core.LangES})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	tm, _ = tm.Update(m.load())
	return tm.(Model)
}

// step envía un mensaje y ejecuta en cadena los comandos que devuelve (el
// bucle de Bubble Tea, pero síncrono), hasta que ya no hay más trabajo.
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	var tm tea.Model = m
	var cmds []tea.Cmd
	tm, c := tm.Update(msg)
	cmds = append(cmds, c)
	for len(cmds) > 0 {
		c := cmds[0]
		cmds = cmds[1:]
		if c == nil {
			continue
		}
		out := c()
		switch o := out.(type) {
		case nil:
		case tea.BatchMsg:
			cmds = append(cmds, o...)
		default:
			var next tea.Cmd
			tm, next = tm.Update(o)
			cmds = append(cmds, next)
		}
	}
	return tm.(Model)
}

func keyMsg(k string) tea.Msg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// type_ escribe un texto en el campo, sin ejecutar comandos.
func type_(t *testing.T, m Model, text string) Model {
	t.Helper()
	var tm tea.Model = m
	for _, r := range text {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return tm.(Model)
}

// openAssistView abre la vista con la tecla a.
func openAssistView(t *testing.T, m Model) Model {
	t.Helper()
	var tm tea.Model = m
	tm, _ = tm.Update(keyMsg("a"))
	return tm.(Model)
}

// enter envía con Enter y deja correr el turno hasta el final.
func enter(t *testing.T, m Model) Model {
	t.Helper()
	var tm tea.Model = m
	tm, cmd := tm.Update(keyMsg("enter"))
	for cmd != nil {
		msg := cmd()
		tm, cmd = tm.Update(msg)
		if _, ok := msg.(assistDoneMsg); ok {
			break
		}
	}
	return tm.(Model)
}

const fixReply = "Cambio el backend por defecto a `app`.\n\n```haproxy\n" +
	"defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend web\n    bind *:80\n    default_backend app\n\nbackend app\n    server a1 10.0.0.1:80 check\n" +
	"```\n"

func fixOutcome() AssistOutcome {
	p := core.ExtractProposal(fixReply, []core.Source{{Path: "/tmp/x.cfg", Text: fixedCfg}})
	return AssistOutcome{Text: fixReply, Proposal: p, StopReason: "end_turn"}
}

func TestAssistDisabledWithoutFlag(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "a")
	if m.view != viewSections {
		t.Error("sin --assistant la tecla a no debe abrir la vista")
	}
	if !strings.Contains(m.View(), "Asistente desactivado: inicia con --assistant (envía la configuración a la API de Anthropic)") {
		t.Errorf("falta el aviso:\n%s", m.View())
	}
	// También desde otras vistas.
	m = press(t, m, "p", "a")
	if m.view != viewProblems || !strings.Contains(m.status, "--assistant") {
		t.Errorf("view=%v status=%q", m.view, m.status)
	}
}

func TestAssistStreamsAndSavesAfterConfirm(t *testing.T) {
	f := &fakeAssist{chunks: strings.SplitAfter(fixReply, "\n"), out: fixOutcome()}
	var saved []core.FileChange
	m := assistModelWith(t, f, func(ch []core.FileChange) error { saved = ch; return nil })
	m = openAssistView(t, m)
	if m.view != viewAssist {
		t.Fatal("a debe abrir la vista del asistente")
	}
	v := m.View()
	for _, want := range []string{"Asistente · claude-test", "Se envía la configuración a la API de Anthropic (modelo claude-test)"} {
		if !strings.Contains(v, want) {
			t.Errorf("falta %q:\n%s", want, v)
		}
	}
	// q, p, e… son texto en el campo, no atajos.
	m = type_(t, m, "arregla el backend qpe")
	if m.view != viewAssist || m.assist.input.Value() != "arregla el backend qpe" {
		t.Fatalf("la entrada debe aceptar cualquier tecla: %q view=%v", m.assist.input.Value(), m.view)
	}
	m = enter(t, m)

	// Turno terminado: se envió la petición con el idioma, se ve la respuesta sin el bloque de configuración y el diff.
	if len(f.calls) != 1 || f.calls[0][0].Content != "arregla el backend qpe" || f.langs[0] != core.LangES {
		t.Fatalf("llamada: %+v %v", f.calls, f.langs)
	}
	if m.assist.mode != assistConfirm {
		t.Fatalf("debe pedir confirmación, modo %v", m.assist.mode)
	}
	v = m.View()
	for _, want := range []string{"Tú", "arregla el backend qpe", "Cambio el backend por defecto a app", "[configuración propuesta:", "── propuesta ──", "-    default_backend nope", "+    default_backend app", "Tras aplicarla: 0 errores", "¿Aplicar y guardar? [y/N]"} {
		if !strings.Contains(v, want) {
			t.Errorf("falta %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "frontend web\n    bind") && strings.Contains(v, "timeout connect 5s") && !strings.Contains(v, "@@") {
		t.Error("el bloque de configuración no debe mostrarse entero")
	}
	if saved != nil {
		t.Fatal("no debe guardar antes de confirmar")
	}

	m = step(t, m, keyMsg("y"))
	if len(saved) != 1 || saved[0].Path != "/tmp/x.cfg" || !strings.Contains(saved[0].Text, "default_backend app") {
		t.Fatalf("Save recibió %+v", saved)
	}
	if m.assist.mode != assistInput || m.assist.out != nil {
		t.Error("tras guardar vuelve al campo de entrada")
	}
	if !strings.Contains(m.View(), "✓ Guardado") {
		t.Errorf("falta la confirmación:\n%s", m.View())
	}
	// Conserva la conversación: segunda petición lleva los 3 mensajes.
	f.chunks, f.out = nil, AssistOutcome{Text: "Hecho.", StopReason: "end_turn"}
	m = type_(t, m, "gracias")
	m = enter(t, m)
	if n := len(f.calls[1]); n != 3 || f.calls[1][1].Role != "assistant" || f.calls[1][2].Content != "gracias" {
		t.Errorf("la conversación debe conservarse: %+v", f.calls[1])
	}
}

func TestAssistShowsStreamWhileArriving(t *testing.T) {
	f := &fakeAssist{block: true, chunks: []string{"Estoy ", "pensando"}}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "hola")
	var tm tea.Model = m
	tm, cmd := tm.Update(keyMsg("enter"))
	for i := 0; i < 2; i++ { // dos trozos
		msg := cmd()
		tm, cmd = tm.Update(msg)
	}
	m = tm.(Model)
	if m.assist.mode != assistStreaming || !strings.Contains(m.View(), "Estoy pensando") {
		t.Fatalf("debe verse el texto parcial:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "Esc cancela") {
		t.Error("debe avisar de que Esc cancela")
	}
	// limpieza: cancela para que la goroutine termine
	tm, _ = tm.Update(keyMsg("esc"))
}

func TestAssistEscCancelsStream(t *testing.T) {
	f := &fakeAssist{block: true, chunks: []string{"parcial"}}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "algo largo")
	var tm tea.Model = m
	tm, cmd := tm.Update(keyMsg("enter"))
	tm, cmd = tm.Update(cmd()) // llega «parcial»
	tm, cmd = tm.Update(keyMsg("esc"))
	_ = cmd
	// el goroutine termina por el contexto y manda assistDoneMsg
	m = tm.(Model)
	for m.assist.mode == assistStreaming {
		msg := waitAssist(m.assist.run)()
		tm, _ = m.Update(msg)
		m = tm.(Model)
	}
	if m.assist.mode != assistInput || m.assist.input.Value() != "algo largo" {
		t.Errorf("tras cancelar, la petición vuelve al campo: modo %v %q", m.assist.mode, m.assist.input.Value())
	}
	if len(m.assist.msgs) != 0 {
		t.Errorf("un turno cancelado no entra en la conversación: %+v", m.assist.msgs)
	}
	if !strings.Contains(m.View(), "Cancelado") {
		t.Errorf("falta el aviso de cancelación:\n%s", m.View())
	}
}

func TestAssistEscGoesBackToSections(t *testing.T) {
	m := assistModelWith(t, &fakeAssist{}, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "borrador")
	m = step(t, m, keyMsg("esc"))
	if m.view != viewSections {
		t.Fatal("esc desde la vista del asistente debe volver a las secciones")
	}
	// Al volver, el borrador sigue.
	m = openAssistView(t, m)
	if m.assist.input.Value() != "borrador" {
		t.Error("el borrador debe conservarse")
	}
}

func TestAssistDeclinedProposalSavesNothing(t *testing.T) {
	f := &fakeAssist{out: fixOutcome()}
	called := false
	m := assistModelWith(t, f, func([]core.FileChange) error { called = true; return nil })
	m = openAssistView(t, m)
	m = type_(t, m, "cambia")
	m = enter(t, m)
	m = step(t, m, keyMsg("n"))
	if called || m.assist.mode != assistInput {
		t.Fatalf("n no debe guardar (called=%v modo=%v)", called, m.assist.mode)
	}
	if !strings.Contains(m.View(), "no se guardó nada") {
		t.Error("debe avisar de que no se guardó")
	}
	if len(m.assist.msgs) != 2 {
		t.Error("la conversación se conserva aunque se rechace la propuesta")
	}
}

func TestAssistSaveRejectedShowsReason(t *testing.T) {
	f := &fakeAssist{out: fixOutcome()}
	m := assistModelWith(t, f, func([]core.FileChange) error { return errors.New("no se guardó: línea 7: directiva desconocida") })
	m = openAssistView(t, m)
	m = type_(t, m, "cambia")
	m = enter(t, m)
	m = step(t, m, keyMsg("y"))
	v := m.View()
	if !strings.Contains(v, "No se guardó:") || !strings.Contains(v, "directiva desconocida") {
		t.Errorf("falta el motivo:\n%s", v)
	}
	if strings.Contains(v, "✓ Guardado") {
		t.Error("no debe decir que guardó")
	}
}

func TestAssistErrorKeepsRequestForRetry(t *testing.T) {
	f := &fakeAssist{err: errors.New("la API de Anthropic rechazó las credenciales")}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "hola")
	m = enter(t, m)
	v := m.View()
	if !strings.Contains(v, "rechazó las credenciales") || !strings.Contains(v, "Enter para reintentar") {
		t.Errorf("falta el error:\n%s", v)
	}
	if m.assist.input.Value() != "hola" || len(m.assist.msgs) != 0 {
		t.Errorf("la petición vuelve al campo y no queda en el historial: %q %+v", m.assist.input.Value(), m.assist.msgs)
	}
	f.err, f.out = nil, AssistOutcome{Text: "ahora sí", StopReason: "end_turn"}
	m = enter(t, m)
	if !strings.Contains(m.View(), "ahora sí") || len(m.assist.msgs) != 2 {
		t.Errorf("el reintento debe funcionar:\n%s", m.View())
	}
}

func TestAssistRefusalAndMaxTokens(t *testing.T) {
	f := &fakeAssist{out: AssistOutcome{StopReason: "refusal", RefusalCategory: "cyber"}}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "x")
	m = enter(t, m)
	if !strings.Contains(m.View(), "declinó la petición (categoría: cyber)") {
		t.Errorf("falta el rechazo:\n%s", m.View())
	}
	f.out = AssistOutcome{Text: "corta", StopReason: "max_tokens"}
	m = enter(t, m)
	if !strings.Contains(m.View(), "límite de longitud") {
		t.Errorf("falta el aviso de max_tokens:\n%s", m.View())
	}
}

func TestAssistRefusesToSaveIfFilesChanged(t *testing.T) {
	f := &fakeAssist{out: fixOutcome()}
	called := false
	m := assistModelWith(t, f, func([]core.FileChange) error { called = true; return nil })
	m = openAssistView(t, m)
	m = type_(t, m, "cambia")
	m = enter(t, m)
	// el archivo cambia en disco antes de confirmar (recarga por el vigilante)
	var tm tea.Model = m
	tm, _ = tm.Update(loadedMsg{srcs: []core.Source{{Path: "/tmp/x.cfg", Text: fixedCfg + "\n# editado\n"}}})
	m = step(t, tm.(Model), keyMsg("y"))
	if called || !strings.Contains(m.View(), "cambió desde que se hizo la propuesta") {
		t.Errorf("no debe guardar si el archivo cambió (called=%v):\n%s", called, m.View())
	}
}

func TestRenderAssistTextHidesConfigBlocks(t *testing.T) {
	st := newStyles()
	rows := strings.Join(renderAssistText("Uso `x`.\n\n```haproxy path=a.cfg\nglobal\n    maxconn 1\n```\n```bash\nls -l\n```\n", st, 80, false), "\n")
	if strings.Contains(rows, "maxconn") || !strings.Contains(rows, "[configuración propuesta: 2 líneas") {
		t.Errorf("el bloque de configuración no debe mostrarse:\n%s", rows)
	}
	if !strings.Contains(rows, "ls -l") {
		t.Errorf("otros bloques de código sí:\n%s", rows)
	}
	open := strings.Join(renderAssistText("```haproxy\nglobal", st, 80, true), "\n")
	if strings.Contains(open, "global") || !strings.Contains(open, "escribiendo") {
		t.Errorf("bloque abierto en streaming:\n%s", open)
	}
}

func TestHelpMentionsAssistant(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "?")
	if v := m.View(); !strings.Contains(v, "asistente") {
		t.Errorf("la ayuda debe mencionar el asistente:\n%s", v)
	}
}
