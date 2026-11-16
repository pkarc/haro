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

// AI assistant in the TUI. This layer only presents: the Anthropic API call
// and the safe save arrive through Options (Assist and Save), wired up in
// cmd/haro/tui.go; the SDK is not imported here.

// AssistOutcome is the result of an assistant turn.
type AssistOutcome struct {
	// Text is the full reply (including configuration blocks).
	Text string
	// Proposal are the proposed files, with their full text.
	Proposal core.Proposal
	// IssuesAfter are the configuration's issues with the proposal applied.
	IssuesAfter []core.Issue
	// StopReason and RefusalCategory say how it ended (end_turn, max_tokens, refusal…).
	StopReason      string
	RefusalCategory string
}

// msgDisabled is what the `a` key says when the TUI was opened without --assistant.
func (m Model) msgDisabled() string {
	return m.t("Assistant disabled: start with --assistant (sends the configuration to the Anthropic API)", "Asistente desactivado: inicia con --assistant (envía la configuración a la API de Anthropic)")
}

type assistMode int

const (
	assistInput     assistMode = iota // typing the request
	assistStreaming                   // the reply is arriving
	assistConfirm                     // Apply and save? [y/N]
)

// assistRun is an in-flight request; the channel carries its messages to the Bubble Tea loop.
type assistRun struct {
	ch        chan tea.Msg
	cancel    context.CancelFunc
	cancelled bool // the user asked to cancel (Esc)
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

	// msgs is the full conversation of finished turns (alternates roles and
	// ends on an assistant turn); it is kept for the whole session.
	msgs []core.AssistMessage
	// request is the current request (already sent), to put it back in the field if it fails or is cancelled.
	request string
	partial string // reply arriving so far
	run     *assistRun

	out   *AssistOutcome // proposal pending confirmation
	base  []core.Source  // files the request was made with
	diff  string
	notes []string // notices at the end of the log (errors, cancellations, save result)
}

func newAssistState(lang core.Lang) assistState {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = pick(lang, "Ask for a change or ask a question…", "Pide un cambio o haz una pregunta…")
	in.CharLimit = 4000
	in.Cursor.SetMode(cursor.CursorStatic) // no blinking: needs no clock and does not interfere with the message flow
	return assistState{input: in, vp: viewport.New(0, 0)}
}

// openAssist opens the assistant view (or says it is disabled).
func (m Model) openAssist() (tea.Model, tea.Cmd) {
	if m.opts.Assist == nil {
		m.status = m.msgDisabled()
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

// keyAssist handles the keys of the assistant view. It runs before the global
// shortcuts: while typing, q, p, m… are text.
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
			a.run.cancel() // the notice and the return to the field arrive with assistDoneMsg
		}
		return m, nil

	case assistConfirm:
		if msg.String() == "y" || msg.String() == "Y" {
			return m.saveProposal()
		}
		// Any other key rejects the proposal but keeps the conversation.
		a.notes = append(a.notes, m.st.dim.Render(m.t("Proposal discarded: nothing was saved.", "Propuesta descartada: no se guardó nada.")))
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

// sendAssist sends the request and starts the turn in the background.
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
			err = ctx.Err() // cancelled: what was received is discarded
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

// updateAssistDone closes the turn: error, cancellation, reply without changes or proposal.
func (m Model) updateAssistDone(msg assistDoneMsg) (tea.Model, tea.Cmd) {
	a := &m.assist
	if msg.run != a.run {
		return m, nil
	}
	msg.run.cancel()
	a.run = nil
	back := func() (tea.Model, tea.Cmd) { // the turn does not count: the request goes back to the field to retry
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
			a.notes = append(a.notes, m.st.dim.Render(m.t("Cancelled. Your request is still in the field.", "Cancelado. Tu petición sigue en el campo.")))
		} else {
			a.notes = append(a.notes, m.st.err.Render(m.t("Error: ", "Error: "))+msg.err.Error()+"\n"+m.st.dim.Render(m.t("Your request is still in the field: Enter to retry.", "Tu petición sigue en el campo: Enter para reintentar.")))
		}
		return back()
	}
	out := msg.out
	if out.Text == "" {
		out.Text = a.partial
	}
	if strings.TrimSpace(out.Text) == "" {
		if out.StopReason == "refusal" {
			a.notes = append(a.notes, m.st.warn.Render(m.t("The model declined the request", "El modelo declinó la petición")+m.refusalSuffix(out.RefusalCategory)+"."))
			return back()
		}
		a.notes = append(a.notes, m.st.warn.Render(m.t("The assistant did not reply with anything.", "El asistente no respondió nada.")))
		return back()
	}
	a.msgs = append(a.msgs, core.AssistMessage{Role: "assistant", Content: out.Text})
	a.partial = ""
	switch out.StopReason {
	case "refusal":
		a.notes = append(a.notes, m.st.warn.Render(m.t("The model declined the request", "El modelo declinó la petición")+m.refusalSuffix(out.RefusalCategory)+"."))
	case "max_tokens":
		a.notes = append(a.notes, m.st.warn.Render(m.t("The reply was cut off by the length limit: ask it to continue or to do it in parts.", "La respuesta se cortó por el límite de longitud: pide que continúe o que lo haga por partes.")))
	}
	p := out.Proposal
	if p.Truncated {
		a.notes = append(a.notes, m.st.warn.Render(m.t("A configuration block was cut off and discarded.", "Un bloque de configuración quedó cortado y se descartó.")))
	}
	if p.Unmatched > 0 {
		a.notes = append(a.notes, m.st.warn.Render(fmt.Sprintf(m.t("%s could not be matched to a file and was discarded.", "%s no se pudo asociar a un archivo y se descartó."), count(p.Unmatched, m.t("configuration block", "bloque de configuración"), m.t("configuration blocks", "bloques de configuración")))))
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

// count formats "n word" with the right plural.
func count(n int, one, many string) string { return fmt.Sprintf("%d %s", n, plural(n, one, many)) }

func (m Model) refusalSuffix(cat string) string {
	if cat == "" {
		return ""
	}
	return m.t(" (category: ", " (categoría: ") + cat + ")"
}

// proposalDiff is the unified diff of each proposed file.
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

// saveProposal saves the proposal with Options.Save (validates, atomic, .bak).
func (m Model) saveProposal() (tea.Model, tea.Cmd) {
	a := &m.assist
	if m.opts.Save == nil {
		a.notes = append(a.notes, m.st.err.Render(m.t("Cannot save: this interface has no write permission.", "No se puede guardar: esta interfaz no tiene permiso de escritura.")))
		a.out, a.diff, a.mode = nil, "", assistInput
		m.refreshAssist(true)
		return m, a.input.Focus()
	}
	// If the files changed since the request, the proposal would overwrite those changes.
	for _, ch := range a.out.Proposal.Changes {
		for _, base := range a.base {
			if base.Path != ch.Path {
				continue
			}
			for _, cur := range m.srcs {
				if cur.Path == ch.Path && cur.Text != base.Text {
					a.notes = append(a.notes, m.st.err.Render(fmt.Sprintf(m.t("%s changed since the proposal was made: nothing was saved. Repeat the request.", "%s cambió desde que se hizo la propuesta: no se guardó nada. Repite la petición."), cur.Path)))
					a.out, a.diff, a.mode = nil, "", assistInput
					m.refreshAssist(true)
					return m, a.input.Focus()
				}
			}
		}
	}
	changes := append([]core.FileChange{}, a.out.Proposal.Changes...)
	save := m.opts.Save
	a.notes = append(a.notes, m.st.dim.Render(m.t("Saving…", "Guardando…")))
	m.refreshAssist(true)
	return m, func() tea.Msg { return assistSavedMsg{save(changes)} }
}

func (m Model) updateAssistSaved(msg assistSavedMsg) (tea.Model, tea.Cmd) {
	a := &m.assist
	a.notes = a.notes[:len(a.notes)-1] // remove "Saving…"
	cmds := []tea.Cmd{}
	if msg.err != nil {
		// Validation rejected the configuration: the reason is shown and the proposal is dropped.
		a.notes = append(a.notes, m.st.err.Render(m.t("Not saved: ", "No se guardó: "))+msg.err.Error())
		a.out, a.diff, a.mode = nil, "", assistInput
	} else {
		n := len(a.out.Proposal.Changes)
		a.notes = append(a.notes, m.st.ok.Render(fmt.Sprintf(m.t("✓ Saved (%s). A .bak copy is kept.", "✓ Guardado (%s). Se conserva una copia .bak."), count(n, m.t("file", "archivo"), m.t("files", "archivos")))))
		a.out, a.diff, a.mode = nil, "", assistInput
		cmds = append(cmds, m.load) // reload the saved configuration
	}
	m.refreshAssist(true)
	cmds = append(cmds, a.input.Focus())
	return m, tea.Batch(cmds...)
}

// ---------- view ----------

// assistTitle is the pane title.
func (m Model) assistTitle() string {
	t := m.t("Assistant", "Asistente")
	if m.opts.AssistModel != "" {
		t += " · " + m.opts.AssistModel
	}
	return t
}

// refreshAssist rebuilds the log content; with `bottom`, or if it was already
// at the end, it scrolls down to the last message.
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
			st.dim.Render(m.t("Describe what you need; the assistant knows your configuration and proposes changes you will see as a diff before saving.", "Describe lo que necesitas; el asistente conoce tu configuración y propone cambios que verás como diff antes de guardar.")),
			st.dim.Render(m.t("Examples: \"Review my configuration and tell me what you would improve\" · \"Limit to 20 requests per second per IP\"", "Ejemplos: «Revisa mi configuración y dime qué mejorarías» · «Limita a 20 peticiones por segundo por IP»")), "")
		rows = wrapAll(rows, w)
	}
	for _, msg := range a.msgs {
		if msg.Role == "user" {
			rows = append(rows, wrapIndent(st.title.Render(m.t("You ", "Tú "))+"", msg.Content, w)...)
		} else {
			rows = append(rows, st.tag.Render(m.t("Assistant", "Asistente")))
			rows = append(rows, renderAssistText(msg.Content, st, w, false)...)
		}
		rows = append(rows, "")
	}
	if a.mode == assistStreaming {
		rows = append(rows, st.tag.Render(m.t("Assistant", "Asistente")))
		if a.partial == "" {
			rows = append(rows, st.dim.Render(m.t("…waiting for the reply (Esc cancels)", "…esperando respuesta (Esc cancela)")))
		} else {
			rows = append(rows, renderAssistText(a.partial, st, w, true)...)
		}
	}
	if a.diff != "" {
		rows = append(rows, st.title.Render(m.t("── proposal ──", "── propuesta ──")))
		for _, l := range strings.Split(strings.TrimRight(a.diff, "\n"), "\n") {
			rows = append(rows, wrap(colorDiff(l, st), w)...)
		}
		e := m.errorsAfter()
		line := fmt.Sprintf(m.t("After applying it: %s.", "Tras aplicarla: %s."), count(e, m.t("error", "error"), m.t("errors", "errores")))
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

// configFence are the fence languages the core takes as configuration (core/assist.go).
var configFence = map[string]bool{"": true, "haproxy": true, "cfg": true, "conf": true, "haproxy.cfg": true}

// renderAssistText paints a reply: the minimal markdown styled, code blocks
// as they are and, in place of full configuration blocks, a line pointing to
// the proposal diff.
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
				rows = append(rows, st.dim.Render(st.t("[writing the proposed configuration…]", "[escribiendo la configuración propuesta…]")))
			case open:
				rows = append(rows, st.dim.Render(st.t("[configuration cut off: discarded]", "[configuración cortada: se descarta]")))
			default:
				rows = append(rows, st.dim.Render(fmt.Sprintf(st.t("[proposed configuration: %d lines; shown as a diff]", "[configuración propuesta: %d líneas; se muestra como diff]"), len(body))))
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

// viewAssist is the whole pane: a scrolling log and, below, the input line or the confirmation question.
func (m Model) viewAssist(h int) string {
	a := m.assist
	var foot string
	switch a.mode {
	case assistConfirm:
		foot = m.st.warn.Render(m.t("Apply and save? [y/N]", "¿Aplicar y guardar? [y/N]"))
	case assistStreaming:
		foot = m.st.dim.Render(m.t("Receiving the reply… Esc cancels", "Recibiendo la respuesta… Esc cancela"))
	default:
		foot = a.input.View()
	}
	notice := m.st.dim.Render(m.t("The configuration is sent to the Anthropic API", "Se envía la configuración a la API de Anthropic") + m.modelSuffix(m.opts.AssistModel) + ".")
	body := a.vp.View() + "\n" + notice + "\n" + foot
	return m.st.paneFocus.Width(m.width - 2).Height(h + 1).Render(m.titled(m.assistTitle(), body))
}

func (m Model) modelSuffix(model string) string {
	if model == "" {
		return ""
	}
	return m.t(" (model ", " (modelo ") + model + ")"
}

// assistStatus is the footer help in the assistant view.
func (m Model) assistStatus() string {
	switch m.assist.mode {
	case assistConfirm:
		return m.st.dim.Render(m.t("y apply and save · any other key: discard · ↑/↓ scroll", "y aplicar y guardar · otra tecla: descartar · ↑/↓ desplazar"))
	case assistStreaming:
		return m.st.dim.Render(m.t("esc cancel · ↑/↓ scroll", "esc cancelar · ↑/↓ desplazar"))
	}
	return m.st.dim.Render(m.t("enter send · esc back · ↑/↓ PgUp/PgDn scroll · ctrl+c quit", "enter enviar · esc volver · ↑/↓ PgUp/PgDn desplazar · ctrl+c salir"))
}
