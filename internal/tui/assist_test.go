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

// fakeAssist is a scripted Options.Assist; it remembers what it receives.
type fakeAssist struct {
	mu    sync.Mutex
	calls [][]core.AssistMessage
	langs []core.Lang
	// chunks are delivered with onText; then it returns out/err.
	chunks []string
	out    AssistOutcome
	err    error
	// block makes it wait until the context is cancelled (to test Esc).
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
	return assistModelLang(t, f, save, core.LangEN)
}

func assistModelLang(t *testing.T, f *fakeAssist, save func([]core.FileChange) error, lang core.Lang) Model {
	t.Helper()
	m := New(Options{Load: loader(fixedCfg), NoWatch: true, Editor: "vi", Assist: f.fn, Save: save, AssistModel: "claude-test", Lang: lang})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	tm, _ = tm.Update(m.load())
	return tm.(Model)
}

// step sends a message and chains the commands it returns (the Bubble Tea
// loop, but synchronous), until there is no more work.
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

// type_ types text into the field, without running commands.
func type_(t *testing.T, m Model, text string) Model {
	t.Helper()
	var tm tea.Model = m
	for _, r := range text {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return tm.(Model)
}

// openAssistView opens the view with the a key.
func openAssistView(t *testing.T, m Model) Model {
	t.Helper()
	var tm tea.Model = m
	tm, _ = tm.Update(keyMsg("a"))
	return tm.(Model)
}

// enter sends with Enter and lets the turn run to the end.
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
		t.Error("without --assistant the a key must not open the view")
	}
	if !strings.Contains(m.View(), "Assistant disabled: start with --assistant (sends the configuration to the Anthropic API)") {
		t.Errorf("missing notice:\n%s", m.View())
	}
	// Also from other views.
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
	for _, want := range []string{"Assistant · claude-test", "The configuration is sent to the Anthropic API (model claude-test)"} {
		if !strings.Contains(v, want) {
			t.Errorf("falta %q:\n%s", want, v)
		}
	}
	// q, p, e… are text in the field, not shortcuts.
	m = type_(t, m, "arregla el backend qpe")
	if m.view != viewAssist || m.assist.input.Value() != "arregla el backend qpe" {
		t.Fatalf("la entrada debe aceptar cualquier tecla: %q view=%v", m.assist.input.Value(), m.view)
	}
	m = enter(t, m)

	// Turn finished: the request was sent with the language, the reply is shown without the configuration block, and the diff.
	if len(f.calls) != 1 || f.calls[0][0].Content != "arregla el backend qpe" || f.langs[0] != core.LangEN {
		t.Fatalf("llamada: %+v %v", f.calls, f.langs)
	}
	if m.assist.mode != assistConfirm {
		t.Fatalf("must ask for confirmation, mode %v", m.assist.mode)
	}
	v = m.View()
	for _, want := range []string{"You", "arregla el backend qpe", "Cambio el backend por defecto a app", "[proposed configuration:", "── proposal ──", "-    default_backend nope", "+    default_backend app", "After applying it: 0 errors", "Apply and save? [y/N]"} {
		if !strings.Contains(v, want) {
			t.Errorf("falta %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "frontend web\n    bind") && strings.Contains(v, "timeout connect 5s") && !strings.Contains(v, "@@") {
		t.Error("the configuration block must not be shown in full")
	}
	if saved != nil {
		t.Fatal("no debe guardar antes de confirmar")
	}

	m = step(t, m, keyMsg("y"))
	if len(saved) != 1 || saved[0].Path != "/tmp/x.cfg" || !strings.Contains(saved[0].Text, "default_backend app") {
		t.Fatalf("Save received %+v", saved)
	}
	if m.assist.mode != assistInput || m.assist.out != nil {
		t.Error("after saving it returns to the input field")
	}
	if !strings.Contains(m.View(), "✓ Saved") {
		t.Errorf("missing confirmation:\n%s", m.View())
	}
	// Keeps the conversation: the second request carries the 3 messages.
	f.chunks, f.out = nil, AssistOutcome{Text: "Hecho.", StopReason: "end_turn"}
	m = type_(t, m, "gracias")
	m = enter(t, m)
	if n := len(f.calls[1]); n != 3 || f.calls[1][1].Role != "assistant" || f.calls[1][2].Content != "gracias" {
		t.Errorf("the conversation must be kept: %+v", f.calls[1])
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
		t.Fatalf("the partial text must be visible:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "Esc cancels") {
		t.Error("must say Esc cancels")
	}
	// cleanup: cancel so the goroutine finishes
	tm, _ = tm.Update(keyMsg("esc"))
}

func TestAssistEscCancelsStream(t *testing.T) {
	f := &fakeAssist{block: true, chunks: []string{"parcial"}}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "algo largo")
	var tm tea.Model = m
	tm, cmd := tm.Update(keyMsg("enter"))
	tm, cmd = tm.Update(cmd()) // "partial" arrives
	tm, cmd = tm.Update(keyMsg("esc"))
	_ = cmd
	// the goroutine ends through the context and sends assistDoneMsg
	m = tm.(Model)
	for m.assist.mode == assistStreaming {
		msg := waitAssist(m.assist.run)()
		tm, _ = m.Update(msg)
		m = tm.(Model)
	}
	if m.assist.mode != assistInput || m.assist.input.Value() != "algo largo" {
		t.Errorf("after cancelling, the request returns to the field: mode %v %q", m.assist.mode, m.assist.input.Value())
	}
	if len(m.assist.msgs) != 0 {
		t.Errorf("a cancelled turn does not enter the conversation: %+v", m.assist.msgs)
	}
	if !strings.Contains(m.View(), "Cancelled") {
		t.Errorf("missing cancellation notice:\n%s", m.View())
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
	// On returning, the draft is still there.
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
	if !strings.Contains(m.View(), "nothing was saved") {
		t.Error("must say nothing was saved")
	}
	if len(m.assist.msgs) != 2 {
		t.Error("the conversation is kept even if the proposal is rejected")
	}
}

func TestAssistSaveRejectedShowsReason(t *testing.T) {
	f := &fakeAssist{out: fixOutcome()}
	m := assistModelWith(t, f, func([]core.FileChange) error { return errors.New("not saved: line 7: unknown directive") })
	m = openAssistView(t, m)
	m = type_(t, m, "cambia")
	m = enter(t, m)
	m = step(t, m, keyMsg("y"))
	v := m.View()
	if !strings.Contains(v, "Not saved:") || !strings.Contains(v, "unknown directive") {
		t.Errorf("falta el motivo:\n%s", v)
	}
	if strings.Contains(v, "✓ Saved") {
		t.Error("must not say it saved")
	}
}

func TestAssistErrorKeepsRequestForRetry(t *testing.T) {
	f := &fakeAssist{err: errors.New("the Anthropic API rejected the credentials")}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "hola")
	m = enter(t, m)
	v := m.View()
	if !strings.Contains(v, "rejected the credentials") || !strings.Contains(v, "Enter to retry") {
		t.Errorf("falta el error:\n%s", v)
	}
	if m.assist.input.Value() != "hola" || len(m.assist.msgs) != 0 {
		t.Errorf("the request returns to the field and is not in the history: %q %+v", m.assist.input.Value(), m.assist.msgs)
	}
	f.err, f.out = nil, AssistOutcome{Text: "now it works", StopReason: "end_turn"}
	m = enter(t, m)
	if !strings.Contains(m.View(), "now it works") || len(m.assist.msgs) != 2 {
		t.Errorf("el reintento debe funcionar:\n%s", m.View())
	}
}

func TestAssistRefusalAndMaxTokens(t *testing.T) {
	f := &fakeAssist{out: AssistOutcome{StopReason: "refusal", RefusalCategory: "cyber"}}
	m := assistModelWith(t, f, nil)
	m = openAssistView(t, m)
	m = type_(t, m, "x")
	m = enter(t, m)
	if !strings.Contains(m.View(), "declined the request (category: cyber)") {
		t.Errorf("falta el rechazo:\n%s", m.View())
	}
	f.out = AssistOutcome{Text: "corta", StopReason: "max_tokens"}
	m = enter(t, m)
	if !strings.Contains(m.View(), "length limit") {
		t.Errorf("missing notice de max_tokens:\n%s", m.View())
	}
}

func TestAssistRefusesToSaveIfFilesChanged(t *testing.T) {
	f := &fakeAssist{out: fixOutcome()}
	called := false
	m := assistModelWith(t, f, func([]core.FileChange) error { called = true; return nil })
	m = openAssistView(t, m)
	m = type_(t, m, "cambia")
	m = enter(t, m)
	// the file changes on disk before confirming (reload by the watcher)
	var tm tea.Model = m
	tm, _ = tm.Update(loadedMsg{srcs: []core.Source{{Path: "/tmp/x.cfg", Text: fixedCfg + "\n# editado\n"}}})
	m = step(t, tm.(Model), keyMsg("y"))
	if called || !strings.Contains(m.View(), "changed since the proposal was made") {
		t.Errorf("must not save if the file changed (called=%v):\n%s", called, m.View())
	}
}

func TestRenderAssistTextHidesConfigBlocks(t *testing.T) {
	st := newStyles(core.LangEN)
	rows := strings.Join(renderAssistText("Uso `x`.\n\n```haproxy path=a.cfg\nglobal\n    maxconn 1\n```\n```bash\nls -l\n```\n", st, 80, false), "\n")
	if strings.Contains(rows, "maxconn") || !strings.Contains(rows, "[proposed configuration: 2 lines") {
		t.Errorf("the configuration block must not be shown:\n%s", rows)
	}
	if !strings.Contains(rows, "ls -l") {
		t.Errorf("other code blocks are shown:\n%s", rows)
	}
	open := strings.Join(renderAssistText("```haproxy\nglobal", st, 80, true), "\n")
	if strings.Contains(open, "global") || !strings.Contains(open, "writing") {
		t.Errorf("open block while streaming:\n%s", open)
	}
}

func TestHelpMentionsAssistant(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "?")
	if v := m.View(); !strings.Contains(v, "assistant") {
		t.Errorf("the help debe mencionar el asistente:\n%s", v)
	}
}

// With Lang=es the interface texts come out in Spanish.
func TestSpanishInterfaceTexts(t *testing.T) {
	// Without the assistant: disabled notice, key help, issues view labels.
	m := New(Options{Load: loader(problems), NoWatch: true, Editor: "vi", Lang: core.LangES})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	tm, _ = tm.Update(m.load())
	m = tm.(Model)
	if v := m.View(); !strings.Contains(v, "Secciones") || !strings.Contains(v, "Detalle") || !strings.Contains(v, "asistente") {
		t.Errorf("sections view must be in Spanish:\n%s", v)
	}
	m = press(t, m, "a")
	if !strings.Contains(m.View(), "Asistente desactivado: inicia con --assistant") {
		t.Errorf("missing Spanish disabled notice:\n%s", m.View())
	}
	m = press(t, m, "p")
	if v := m.View(); !strings.Contains(v, "Problemas · todos") || !strings.Contains(v, "aviso") && !strings.Contains(v, "sugerencia") && !strings.Contains(v, "error") {
		t.Errorf("issues view must be in Spanish:\n%s", v)
	}
	if got := newStyles(core.LangES).sevLabel(core.SevWarn); got != "aviso" {
		t.Errorf("sevLabel(warn) = %q", got)
	}
	m = press(t, m, "m")
	if !strings.Contains(m.View(), "Mapa del tráfico") {
		t.Errorf("map title must be in Spanish:\n%s", m.View())
	}

	// Assistant view.
	am := assistModelLang(t, &fakeAssist{}, nil, core.LangES)
	am = press(t, am, "a")
	for _, want := range []string{"Asistente · claude-test", "Se envía la configuración a la API de Anthropic (modelo claude-test)", "Describe lo que necesitas"} {
		if !strings.Contains(am.View(), want) {
			t.Errorf("missing %q:\n%s", want, am.View())
		}
	}
}
