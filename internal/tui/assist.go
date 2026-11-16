package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pkarc/haro/core"
)

// Asistente con IA en la TUI. Esta capa solo presenta: la llamada a la API de
// Anthropic y el guardado seguro llegan por Options (Assist y Save), conectados
// en cmd/haro/tui.go; aquí no se importa el SDK.

// AssistOutcome es el resultado de un turno del asistente.
type AssistOutcome struct {
	// Text es la respuesta completa (incluidos los bloques de configuración).
	Text string
	// Proposal son los archivos propuestos, con el texto completo.
	Proposal core.Proposal
	// IssuesAfter son los problemas de la configuración con la propuesta aplicada.
	IssuesAfter []core.Issue
	// StopReason y RefusalCategory dicen cómo terminó (end_turn, max_tokens, refusal…).
	StopReason      string
	RefusalCategory string
}

// MsgDisabled es lo que dice la tecla `a` cuando la TUI se abrió sin --assistant.
const MsgDisabled = "Asistente desactivado: inicia con --assistant (envía la configuración a la API de Anthropic)"

type assistMode int

const (
	assistInput     assistMode = iota // escribiendo la petición
	assistStreaming                   // llega la respuesta
	assistConfirm                     // ¿Aplicar y guardar? [y/N]
)

// assistRun es una petición en vuelo; el canal lleva sus mensajes al bucle de Bubble Tea.
type assistRun struct {
	ch        chan tea.Msg
	cancel    context.CancelFunc
	cancelled bool // el usuario pidió cancelar (Esc)
}

type assistTextMsg struct {
	run  *assistRun
	text string
}
type assistDoneMsg struct {
	run *assistRun
	out AssistOutcome
	err error
}
type assistSavedMsg struct{ err error }

type assistState struct {
	input textinput.Model
	vp    viewport.Model
	mode  assistMode

	// msgs es la conversación completa de los turnos terminados (alterna roles y
	// termina en un turno del asistente); se conserva mientras dure la sesión.
	msgs []core.AssistMessage
	// request es la petición en curso (ya enviada), para devolverla al campo si falla o se cancela.
	request string
	partial string // respuesta que va llegando
	run     *assistRun

	out   *AssistOutcome // propuesta pendiente de confirmar
	base  []core.Source  // archivos con los que se hizo la petición
	diff  string
	notes []string // avisos al final del registro (errores, cancelaciones, resultado del guardado)
}

func newAssistState() assistState {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "Pide un cambio o haz una pregunta…"
	in.CharLimit = 4000
	in.Cursor.SetMode(cursor.CursorStatic) // sin parpadeo: no necesita reloj ni interfiere con el flujo de mensajes
	return assistState{input: in, vp: viewport.New(0, 0)}
}

// openAssist abre la vista del asistente (o avisa de que está desactivado).
func (m Model) openAssist() (tea.Model, tea.Cmd) {
	if m.opts.Assist == nil {
		m.status = MsgDisabled
		return m, nil
	}
	m.view = viewAssist
	m.layoutAssist()
	m.refreshAssist(true)
	if m.assist.mode == assistInput {
		return m, m.assist.input.Focus()
	}
	return m, nil
}

func (m *Model) layoutAssist() {
	if m.width == 0 {
		return
	}
	m.assist.vp.Width = max(10, m.width-4)
	m.assist.vp.Height = max(2, m.contentHeight()-2)
	m.assist.input.Width = max(10, m.width-8)
}

// keyAssist gestiona las teclas de la vista del asistente. Va antes de los
// atajos globales: mientras se escribe, q, p, m… son texto.
func (m Model) keyAssist(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := &m.assist
	switch msg.String() {
	case "ctrl+c":
		if a.run != nil {
			a.run.cancelled = true
			a.run.cancel()
		}
		if m.w != nil {
			m.w.Close()
		}
		return m, tea.Quit
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		a.vp, cmd = a.vp.Update(tea.KeyMsg{Type: map[string]tea.KeyType{"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "ctrl+u": tea.KeyPgUp, "ctrl+d": tea.KeyPgDown}[msg.String()]})
		return m, cmd
	case "up", "down":
		if a.mode != assistInput || a.input.Value() == "" {
			var cmd tea.Cmd
			a.vp, cmd = a.vp.Update(msg)
			return m, cmd
		}
	}
	switch a.mode {
	case assistStreaming:
		if msg.String() == "esc" {
			a.run.cancelled = true
			a.run.cancel() // el aviso y la vuelta al campo llegan con assistDoneMsg
		}
		return m, nil

	case assistConfirm:
		if msg.String() == "y" || msg.String() == "Y" {
			return m.saveProposal()
		}
		// Cualquier otra tecla rechaza la propuesta, pero conserva la conversación.
		a.notes = append(a.notes, m.st.dim.Render("Propuesta descartada: no se guardó nada."))
		a.out, a.diff, a.mode = nil, "", assistInput
		m.refreshAssist(true)
		return m, a.input.Focus()
	}

	switch msg.String() {
	case "esc":
		m.view = viewSections
		a.input.Blur()
		return m, nil
	case "enter":
		text := strings.TrimSpace(a.input.Value())
		if text == "" {
			return m, nil
		}
		return m.sendAssist(text)
	}
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return m, cmd
}

// sendAssist envía la petición y arranca el turno en segundo plano.
func (m Model) sendAssist(text string) (tea.Model, tea.Cmd) {
	a := &m.assist
	msgs := append(append([]core.AssistMessage{}, a.msgs...), core.AssistMessage{Role: "user", Content: text})
	ctx, cancel := context.WithCancel(context.Background())
	run := &assistRun{ch: make(chan tea.Msg, 64), cancel: cancel}
	a.run, a.request, a.partial, a.mode = run, text, "", assistStreaming
	a.msgs = msgs
	a.out, a.diff, a.notes = nil, "", nil
	a.base = append([]core.Source{}, m.srcs...)
	a.input.SetValue("")
	a.input.Blur()
	lang, assist := m.opts.Lang, m.opts.Assist
	go func() {
		out, err := assist(ctx, msgs, lang, func(t string) {
			select {
			case run.ch <- assistTextMsg{run, t}:
			case <-ctx.Done():
			}
		})
		if ctx.Err() != nil && err == nil {
			err = ctx.Err() // cancelado: se descarta lo recibido
		}
		run.ch <- assistDoneMsg{run, out, err}
	}()
	m.refreshAssist(true)
	return m, waitAssist(run)
}

func waitAssist(r *assistRun) tea.Cmd {
	return func() tea.Msg { return <-r.ch }
}

func (m Model) updateAssistText(msg assistTextMsg) (tea.Model, tea.Cmd) {
	a := &m.assist
	if msg.run != a.run {
		return m, nil
	}
	a.partial += msg.text
	m.refreshAssist(false)
	return m, waitAssist(msg.run)
}

// updateAssistDone cierra el turno: error, cancelación, respuesta sin cambios o propuesta.
func (m Model) updateAssistDone(msg assistDoneMsg) (tea.Model, tea.Cmd) {
	a := &m.assist
	if msg.run != a.run {
		return m, nil
	}
	msg.run.cancel()
	a.run = nil
	back := func() (tea.Model, tea.Cmd) { // el turno no cuenta: la petición vuelve al campo para reintentar
		a.msgs = a.msgs[:len(a.msgs)-1]
		a.input.SetValue(a.request)
		a.input.CursorEnd()
		a.mode = assistInput
		a.partial = ""
		m.refreshAssist(true)
		return m, a.input.Focus()
	}
	if msg.err != nil {
		if msg.run.cancelled {
			a.notes = append(a.notes, m.st.dim.Render("Cancelado. Tu petición sigue en el campo."))
		} else {
			a.notes = append(a.notes, m.st.err.Render("Error: ")+msg.err.Error()+"\n"+m.st.dim.Render("Tu petición sigue en el campo: Enter para reintentar."))
		}
		return back()
	}
	out := msg.out
	if out.Text == "" {
		out.Text = a.partial
	}
	if strings.TrimSpace(out.Text) == "" {
		if out.StopReason == "refusal" {
			a.notes = append(a.notes, m.st.warn.Render("El modelo declinó la petición"+refusalSuffix(out.RefusalCategory)+"."))
			return back()
		}
		a.notes = append(a.notes, m.st.warn.Render("El asistente no respondió nada."))
		return back()
	}
	a.msgs = append(a.msgs, core.AssistMessage{Role: "assistant", Content: out.Text})
	a.partial = ""
	switch out.StopReason {
	case "refusal":
		a.notes = append(a.notes, m.st.warn.Render("El modelo declinó la petición"+refusalSuffix(out.RefusalCategory)+"."))
	case "max_tokens":
		a.notes = append(a.notes, m.st.warn.Render("La respuesta se cortó por el límite de longitud: pide que continúe o que lo haga por partes."))
	}
	p := out.Proposal
	if p.Truncated {
		a.notes = append(a.notes, m.st.warn.Render("Un bloque de configuración quedó cortado y se descartó."))
	}
	if p.Unmatched > 0 {
		a.notes = append(a.notes, m.st.warn.Render(fmt.Sprintf("%s no se pudo asociar a un archivo y se descartó.", count(p.Unmatched, "bloque de configuración", "bloques de configuración"))))
	}
	a.mode = assistInput
	if len(p.Changes) > 0 {
		a.out = &out
		a.diff = m.proposalDiff(a.base, p.Changes)
		a.mode = assistConfirm
		a.input.Blur()
		m.refreshAssist(true)
		return m, nil
	}
	m.refreshAssist(true)
	return m, a.input.Focus()
}

// count formatea «n palabra» con el plural correcto.
func count(n int, one, many string) string { return fmt.Sprintf("%d %s", n, plural(n, one, many)) }

func refusalSuffix(cat string) string {
	if cat == "" {
		return ""
	}
	return " (categoría: " + cat + ")"
}

// proposalDiff es el diff unificado de cada archivo propuesto y cuántos errores quedarían.
func (m Model) proposalDiff(base []core.Source, changes []core.FileChange) string {
	var b strings.Builder
	for _, ch := range changes {
		before := ""
		for _, s := range base {
			if s.Path == ch.Path {
				before = s.Text
			}
		}
		b.WriteString(core.UnifiedDiff(ch.Path, before, ch.Text, 3))
	}
	return b.String()
}

func (m Model) errorsAfter() int {
	n := 0
	if m.assist.out != nil {
		for _, i := range m.assist.out.IssuesAfter {
			if i.Severity == core.SevError {
				n++
			}
		}
	}
	return n
}

// saveProposal guarda la propuesta con Options.Save (valida, atómico, .bak).
func (m Model) saveProposal() (tea.Model, tea.Cmd) {
	a := &m.assist
	if m.opts.Save == nil {
		a.notes = append(a.notes, m.st.err.Render("No se puede guardar: esta interfaz no tiene permiso de escritura."))
		a.out, a.diff, a.mode = nil, "", assistInput
		m.refreshAssist(true)
		return m, a.input.Focus()
	}
	// Si los archivos cambiaron desde la petición, la propuesta pisaría esos cambios.
	for _, ch := range a.out.Proposal.Changes {
		for _, base := range a.base {
			if base.Path != ch.Path {
				continue
			}
			for _, cur := range m.srcs {
				if cur.Path == ch.Path && cur.Text != base.Text {
					a.notes = append(a.notes, m.st.err.Render(fmt.Sprintf("%s cambió desde que se hizo la propuesta: no se guardó nada. Repite la petición.", cur.Path)))
					a.out, a.diff, a.mode = nil, "", assistInput
					m.refreshAssist(true)
					return m, a.input.Focus()
				}
			}
		}
	}
	changes := append([]core.FileChange{}, a.out.Proposal.Changes...)
	save := m.opts.Save
	a.notes = append(a.notes, m.st.dim.Render("Guardando…"))
	m.refreshAssist(true)
	return m, func() tea.Msg { return assistSavedMsg{save(changes)} }
}

func (m Model) updateAssistSaved(msg assistSavedMsg) (tea.Model, tea.Cmd) {
	a := &m.assist
	a.notes = a.notes[:len(a.notes)-1] // quita «Guardando…»
	cmds := []tea.Cmd{}
	if msg.err != nil {
		// La validación rechazó la configuración: el motivo se muestra y la propuesta sigue pendiente de otra petición.
		a.notes = append(a.notes, m.st.err.Render("No se guardó: ")+msg.err.Error())
		a.out, a.diff, a.mode = nil, "", assistInput
	} else {
		n := len(a.out.Proposal.Changes)
		a.notes = append(a.notes, m.st.ok.Render(fmt.Sprintf("✓ Guardado (%s). Se conserva una copia .bak.", count(n, "archivo", "archivos"))))
		a.out, a.diff, a.mode = nil, "", assistInput
		cmds = append(cmds, m.load) // recarga la configuración guardada
	}
	m.refreshAssist(true)
	cmds = append(cmds, a.input.Focus())
	return m, tea.Batch(cmds...)
}

// ---------- vista ----------

// assistTitle es el título del panel.
func (m Model) assistTitle() string {
	t := "Asistente"
	if m.opts.AssistModel != "" {
		t += " · " + m.opts.AssistModel
	}
	return t
}

// refreshAssist reconstruye el contenido del registro; con `bottom` o si ya
// estaba al final, baja hasta el último mensaje.
func (m *Model) refreshAssist(bottom bool) {
	a := &m.assist
	if m.width == 0 {
		return
	}
	atBottom := a.vp.AtBottom()
	a.vp.SetContent(strings.Join(m.assistLines(), "\n"))
	if bottom || atBottom {
		a.vp.GotoBottom()
	}
}

func (m Model) assistLines() []string {
	a, st, w := m.assist, m.st, max(10, m.width-4)
	var rows []string
	if len(a.msgs) == 0 {
		rows = append(rows,
			st.dim.Render("Describe lo que necesitas; el asistente conoce tu configuración y propone cambios que verás como diff antes de guardar."),
			st.dim.Render("Ejemplos: «Revisa mi configuración y dime qué mejorarías» · «Limita a 20 peticiones por segundo por IP»"), "")
		rows = wrapAll(rows, w)
	}
	for _, msg := range a.msgs {
		if msg.Role == "user" {
			rows = append(rows, wrapIndent(st.title.Render("Tú ")+"", msg.Content, w)...)
		} else {
			rows = append(rows, st.tag.Render("Asistente"))
			rows = append(rows, renderAssistText(msg.Content, st, w, false)...)
		}
		rows = append(rows, "")
	}
	if a.mode == assistStreaming {
		rows = append(rows, st.tag.Render("Asistente"))
		if a.partial == "" {
			rows = append(rows, st.dim.Render("…esperando respuesta (Esc cancela)"))
		} else {
			rows = append(rows, renderAssistText(a.partial, st, w, true)...)
		}
	}
	if a.diff != "" {
		rows = append(rows, st.title.Render("── propuesta ──"))
		for _, l := range strings.Split(strings.TrimRight(a.diff, "\n"), "\n") {
			rows = append(rows, wrap(colorDiff(l, st), w)...)
		}
		e := m.errorsAfter()
		line := fmt.Sprintf("Tras aplicarla: %s.", count(e, "error", "errores"))
		if e > 0 {
			line = st.err.Render(line)
		} else {
			line = st.ok.Render(line)
		}
		rows = append(rows, "", line)
	}
	for _, n := range a.notes {
		rows = append(rows, wrap(n, w)...)
	}
	return rows
}

func wrapAll(rows []string, w int) []string {
	var out []string
	for _, r := range rows {
		out = append(out, wrap(r, w)...)
	}
	return out
}

func colorDiff(l string, st styles) string {
	switch {
	case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		return st.title.Render(l)
	case strings.HasPrefix(l, "@@"):
		return st.tag.Render(l)
	case strings.HasPrefix(l, "+"):
		return st.ok.Render(l)
	case strings.HasPrefix(l, "-"):
		return st.err.Render(l)
	}
	return l
}

// configFence son los lenguajes de bloque que el núcleo toma por configuración (core/assist.go).
var configFence = map[string]bool{"": true, "haproxy": true, "cfg": true, "conf": true, "haproxy.cfg": true}

// renderAssistText pinta una respuesta: el markdown mínimo con estilos, los
// bloques de código tal cual y, en lugar de los bloques de configuración
// completa, una línea que remite al diff de la propuesta.
func renderAssistText(text string, st styles, width int, streaming bool) []string {
	var rows []string
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if !strings.HasPrefix(t, "```") {
			rows = append(rows, wrap(st.rich(lines[i]), width)...)
			continue
		}
		info := strings.Fields(strings.TrimPrefix(t, "```"))
		lang := ""
		if len(info) > 0 && !strings.HasPrefix(info[0], "path=") {
			lang = strings.ToLower(info[0])
		}
		j := i + 1
		for j < len(lines) && strings.TrimRight(lines[j], " \t") != "```" {
			j++
		}
		open := j >= len(lines)
		body := lines[i+1 : min(j, len(lines))]
		if configFence[lang] {
			switch {
			case open && streaming:
				rows = append(rows, st.dim.Render("[escribiendo la configuración propuesta…]"))
			case open:
				rows = append(rows, st.dim.Render("[configuración cortada: se descarta]"))
			default:
				rows = append(rows, st.dim.Render(fmt.Sprintf("[configuración propuesta: %d líneas; se muestra como diff]", len(body))))
			}
		} else {
			for _, b := range body {
				rows = append(rows, "    "+st.code.Render(b))
			}
		}
		i = j
	}
	return rows
}

// viewAssist es el panel completo: registro con scroll y, debajo, la línea de entrada o la pregunta de confirmación.
func (m Model) viewAssist(h int) string {
	a := m.assist
	var foot string
	switch a.mode {
	case assistConfirm:
		foot = m.st.warn.Render("¿Aplicar y guardar? [y/N]")
	case assistStreaming:
		foot = m.st.dim.Render("Recibiendo la respuesta… Esc cancela")
	default:
		foot = a.input.View()
	}
	notice := m.st.dim.Render("Se envía la configuración a la API de Anthropic" + modelSuffix(m.opts.AssistModel) + ".")
	body := a.vp.View() + "\n" + notice + "\n" + foot
	return m.st.paneFocus.Width(m.width - 2).Height(h + 1).Render(m.titled(m.assistTitle(), body))
}

func modelSuffix(model string) string {
	if model == "" {
		return ""
	}
	return " (modelo " + model + ")"
}

// assistStatus es la ayuda del pie en la vista del asistente.
func (m Model) assistStatus() string {
	switch m.assist.mode {
	case assistConfirm:
		return m.st.dim.Render("y aplicar y guardar · otra tecla: descartar · ↑/↓ desplazar")
	case assistStreaming:
		return m.st.dim.Render("esc cancelar · ↑/↓ desplazar")
	}
	return m.st.dim.Render("enter enviar · esc volver · ↑/↓ PgUp/PgDn desplazar · ctrl+c salir")
}
