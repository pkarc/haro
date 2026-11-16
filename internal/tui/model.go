package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/pkarc/haro/core"
)

// Options configures the interface.
type Options struct {
	// Load reads the configuration (in load order). It is called again on
	// reload. Source.Path must be the real path of the file.
	Load func() ([]core.Source, error)
	// Dirs are the directories passed as arguments: they are watched to detect
	// new or deleted files.
	Dirs []string
	// Editor overrides $VISUAL/$EDITOR (empty = from the environment).
	Editor string
	// Lang is the language of the interface, explanations and detector
	// messages (empty = English).
	Lang core.Lang
	// HAProxyVersion is the target HAProxy version (empty = none).
	HAProxyVersion string
	// NoColor disables colors.
	NoColor bool
	// NoWatch disables automatic reloading.
	NoWatch bool
	// Assist is the AI assistant; nil if the TUI was opened without --assistant.
	// It is called in the background, delivers the reply in chunks through
	// onText and must return when ctx is cancelled. Sending the configuration
	// to the Anthropic API is the implementer's job (cmd/haro), not this package's.
	Assist func(ctx context.Context, msgs []core.AssistMessage, lang core.Lang, onText func(string)) (AssistOutcome, error)
	// AssistModel is the assistant's model, for display only.
	AssistModel string
	// Save writes the proposed files (validates the full configuration, atomic
	// write and .bak copy); it returns the reason if rejected.
	Save func([]core.FileChange) error
}

type view int

const (
	viewSections view = iota
	viewProblems
	viewMap
	viewAssist
)

// focus says which pane receives the scroll keys in the sections view.
type focus int

const (
	focusList focus = iota
	focusDetail
)

type keyMap struct {
	Up, Down, Top, Bottom, Tab, Problems, Map, Edit, Filter, Enter, Back, Help, Quit, Reload, Assist key.Binding
}

func newKeys(lang core.Lang) keyMap {
	t := func(en, es string) string { return pick(lang, en, es) }
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", t("up", "arriba"))),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", t("down", "abajo"))),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", t("top", "inicio"))),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", t("bottom", "final"))),
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", t("pane", "panel"))),
		Problems: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", t("problems", "problemas"))),
		Map:      key.NewBinding(key.WithKeys("m"), key.WithHelp("m", t("map", "mapa"))),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", t("edit", "editar"))),
		Filter:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", t("filter severity", "filtrar gravedad"))),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", t("go to section", "ir a la sección"))),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", t("back", "volver"))),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", t("help", "ayuda"))),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", t("quit", "salir"))),
		Reload:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", t("reload", "recargar"))),
		Assist:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", t("assistant", "asistente"))),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Problems, k.Map, k.Edit, k.Assist, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.Tab},
		{k.Problems, k.Filter, k.Enter, k.Map, k.Back},
		{k.Edit, k.Assist, k.Reload, k.Help, k.Quit},
	}
}

// item is a row of the sections list: a file header (section = -1) or a
// section.
type item struct {
	section int
	file    int
	label   string
}

// Model is the Bubble Tea model.
type Model struct {
	opts   Options
	st     styles
	keys   keyMap
	help   help.Model
	width  int
	height int

	srcs     []core.Source
	analysis core.Analysis
	loadErr  error

	view     view
	focus    focus
	showHelp bool

	items  []item
	cursor int // position in items
	offset int // first visible row of the list

	detail viewport.Model
	hl     *lineRef

	issueCursor int
	issueOffset int
	sevFilter   int // 0 all, 1 errors, 2 warnings, 3 suggestions

	mapView viewport.Model

	assist assistState

	status string
	w      *watcher
}

// New creates the model; the configuration is loaded in Init.
func New(opts Options) Model {
	applyColorProfile(opts.NoColor)
	if opts.Editor == "" {
		opts.Editor = defaultEditor()
	}
	h := help.New()
	return Model{opts: opts, st: newStyles(opts.Lang), keys: newKeys(opts.Lang), help: h, detail: viewport.New(0, 0), mapView: viewport.New(0, 0), assist: newAssistState(opts.Lang)}
}

type loadedMsg struct {
	srcs []core.Source
	err  error
}
type changedMsg struct{}
type editorDoneMsg struct{ err error }

func (m Model) load() tea.Msg {
	srcs, err := m.opts.Load()
	return loadedMsg{srcs, err}
}

func (m *Model) waitChange() tea.Cmd {
	w := m.w
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-w.Events:
			return changedMsg{}
		case <-w.done:
			return nil
		}
	}
}

func (m Model) Init() tea.Cmd { return m.load }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case loadedMsg:
		var cmd tea.Cmd
		m.loadErr = msg.err
		if msg.err == nil {
			m.setSources(msg.srcs)
			if m.w == nil && !m.opts.NoWatch {
				files := make([]string, len(msg.srcs))
				for i, s := range msg.srcs {
					files[i] = s.Path
				}
				if w, err := newWatcher(files, m.opts.Dirs); err == nil {
					m.w = w
					cmd = m.waitChange()
				}
			}
		}
		return m, cmd

	case changedMsg:
		return m, tea.Batch(m.load, m.waitChange())

	case editorDoneMsg:
		if msg.err != nil {
			m.status = m.t("The editor exited with an error: ", "El editor terminó con error: ") + msg.err.Error()
		}
		return m, m.load

	case assistTextMsg:
		return m.updateAssistText(msg)
	case assistDoneMsg:
		return m.updateAssistDone(msg)
	case assistSavedMsg:
		return m.updateAssistSaved(msg)

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

// setSources analyzes the configuration and keeps, if it can, the selected
// section (by type and name).
func (m *Model) setSources(srcs []core.Source) {
	prevKey := ""
	if s := m.currentSection(); s != nil {
		prevKey = s.Type + "\x00" + s.Name
	}
	m.srcs = srcs
	m.analysis = core.AnalyzeConfigWith(srcs, core.LintOptions{Lang: m.opts.Lang, HAProxyVersion: m.opts.HAProxyVersion})
	m.items = buildItems(m.analysis)
	m.cursor = 0
	for i, it := range m.items {
		if it.section >= 0 && m.analysis.Doc.Sections[it.section].Type+"\x00"+m.analysis.Doc.Sections[it.section].Name == prevKey && prevKey != "" {
			m.cursor = i
			break
		}
	}
	m.skipHeader(1)
	m.hl = nil
	if n := len(m.filteredIssues()); m.issueCursor >= n {
		m.issueCursor = max(0, n-1)
	}
	m.layout()
}

// buildItems builds the list rows; with several files it groups by file.
func buildItems(a core.Analysis) []item {
	var items []item
	multi := len(a.Doc.Files) > 1
	cur := -1
	for i, s := range a.Doc.Sections {
		if multi && s.File != cur {
			cur = s.File
			items = append(items, item{section: -1, file: s.File, label: fileLabel(a, s.File)})
		}
		items = append(items, item{section: i, file: s.File, label: strings.TrimSpace(s.Type + " " + s.Name)})
	}
	return items
}

func (m Model) currentSection() *core.Section {
	if m.cursor < 0 || m.cursor >= len(m.items) || m.items[m.cursor].section < 0 {
		return nil
	}
	return m.analysis.Doc.Sections[m.items[m.cursor].section]
}

// skipHeader moves the cursor off the file headers, in the given direction.
func (m *Model) skipHeader(dir int) {
	for m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].section < 0 {
		m.cursor += dir
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
		m.skipHeader(-1)
	}
	if m.cursor < 0 {
		m.cursor = 0
		m.skipHeader(1)
	}
}

func (m *Model) moveCursor(delta int) {
	if len(m.items) == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	prev := m.cursor
	m.cursor = clamp(m.cursor+delta, 0, len(m.items)-1)
	m.skipHeader(dir)
	if m.items[m.cursor].section < 0 { // no more sections in that direction
		m.cursor = prev
	}
	m.hl = nil
	m.syncScroll()
	m.refreshDetail()
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// follow returns the first visible element of a scrolling list so that the
// cursor stays inside the window, moving it as little as possible.
func follow(cursor, offset, visible int) int {
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+visible {
		return cursor - visible + 1
	}
	return offset
}

// syncScroll remembers the lists' scroll position after a cursor move.
func (m *Model) syncScroll() {
	visible := m.contentHeight()
	m.offset = follow(m.cursor, m.offset, visible)
	m.issueOffset = follow(m.issueCursor, m.issueOffset, visible)
}

// sevFilterName is the name of the current severity filter.
func (m Model) sevFilterName() string {
	if m.opts.Lang == core.LangES {
		return [...]string{"todos", "errores", "avisos", "sugerencias"}[m.sevFilter]
	}
	return [...]string{"all", "errors", "warnings", "suggestions"}[m.sevFilter]
}

func (m Model) filteredIssues() []core.Issue {
	var out []core.Issue
	for _, i := range m.analysis.Issues {
		switch m.sevFilter {
		case 1:
			if i.Severity != core.SevError {
				continue
			}
		case 2:
			if i.Severity != core.SevWarn {
				continue
			}
		case 3:
			if i.Severity != core.SevInfo {
				continue
			}
		}
		out = append(out, i)
	}
	return out
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.view == viewAssist && !m.showHelp {
		return m.keyAssist(msg)
	}
	if key.Matches(msg, k.Quit) {
		if m.w != nil {
			m.w.Close()
		}
		return m, tea.Quit
	}
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}
	m.status = ""
	switch {
	case key.Matches(msg, k.Help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, k.Reload):
		return m, m.load
	case key.Matches(msg, k.Problems):
		if m.view == viewProblems {
			m.view = viewSections
		} else {
			m.view = viewProblems
		}
		return m, nil
	case key.Matches(msg, k.Map):
		if m.view == viewMap {
			m.view = viewSections
		} else {
			m.view = viewMap
			m.refreshMap()
		}
		return m, nil
	case key.Matches(msg, k.Back):
		m.view = viewSections
		return m, nil
	case key.Matches(msg, k.Edit):
		return m.edit()
	case key.Matches(msg, k.Assist):
		return m.openAssist()
	}

	switch m.view {
	case viewProblems:
		return m.keyProblems(msg)
	case viewMap:
		var cmd tea.Cmd
		m.mapView, cmd = m.mapView.Update(msg)
		return m, cmd
	}
	return m.keySections(msg)
}

func (m Model) keySections(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if key.Matches(msg, k.Tab) {
		m.focus = 1 - m.focus
		return m, nil
	}
	if m.focus == focusDetail {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	switch {
	case key.Matches(msg, k.Up):
		m.moveCursor(-1)
	case key.Matches(msg, k.Down):
		m.moveCursor(1)
	case key.Matches(msg, k.Top):
		m.moveCursor(-len(m.items))
	case key.Matches(msg, k.Bottom):
		m.moveCursor(len(m.items))
	case msg.String() == "pgup":
		m.moveCursor(-10)
	case msg.String() == "pgdown":
		m.moveCursor(10)
	}
	return m, nil
}

func (m Model) keyProblems(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	issues := m.filteredIssues()
	switch {
	case key.Matches(msg, k.Up):
		m.issueCursor = max(0, m.issueCursor-1)
	case key.Matches(msg, k.Down):
		m.issueCursor = min(max(0, len(issues)-1), m.issueCursor+1)
	case key.Matches(msg, k.Top):
		m.issueCursor = 0
	case key.Matches(msg, k.Bottom):
		m.issueCursor = max(0, len(issues)-1)
	case key.Matches(msg, k.Filter):
		m.sevFilter = (m.sevFilter + 1) % 4
		m.issueCursor, m.issueOffset = 0, 0
	case key.Matches(msg, k.Enter):
		if m.issueCursor < len(issues) {
			m.jumpToIssue(issues[m.issueCursor])
		}
	}
	m.syncScroll()
	return m, nil
}

// jumpToIssue opens the issue's section and highlights its line.
func (m *Model) jumpToIssue(i core.Issue) {
	if i.Section < 0 {
		m.status = fmt.Sprintf(m.t("The issue is outside any section (%s:%d)", "El problema está fuera de cualquier sección (%s:%d)"), fileLabel(m.analysis, i.File), i.Line)
		return
	}
	for n, it := range m.items {
		if it.section == i.Section {
			m.cursor = n
		}
	}
	m.view, m.focus = viewSections, focusList
	m.hl = &lineRef{file: i.File, line: i.Line}
	m.syncScroll()
	m.refreshDetail()
}

// edit opens the editor at the selected file and line.
func (m Model) edit() (tea.Model, tea.Cmd) {
	file, line := -1, 1
	switch m.view {
	case viewProblems:
		if issues := m.filteredIssues(); m.issueCursor < len(issues) {
			file, line = issues[m.issueCursor].File, issues[m.issueCursor].Line
		}
	default:
		if s := m.currentSection(); s != nil {
			file, line = s.File, s.Line
			if m.hl != nil {
				file, line = m.hl.file, m.hl.line
			}
		}
	}
	if file < 0 || file >= len(m.srcs) {
		m.status = m.t("Nothing to edit", "No hay nada que editar")
		return m, nil
	}
	args := editorCommand(m.opts.Editor, m.srcs[file].Path, line)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// ---------- layout and views ----------

func (m Model) leftWidth() int { return clamp(m.width/3, 22, 42) }

// contentHeight is the height of a pane's content: the screen minus the
// borders (2), the title (1) and the status bar (1).
func (m Model) contentHeight() int { return max(3, m.height-4) }

func (m *Model) layout() {
	if m.width == 0 {
		return
	}
	h := m.contentHeight()
	m.detail.Width = max(10, m.width-m.leftWidth()-4)
	m.detail.Height = h
	m.mapView.Width = max(10, m.width-4)
	m.mapView.Height = h
	m.layoutAssist()
	m.help.Width = m.width
	m.syncScroll()
	m.refreshDetail()
	if m.view == viewMap {
		m.refreshMap()
	}
}

func (m *Model) refreshDetail() {
	if m.width == 0 {
		return
	}
	si := -1
	if m.cursor < len(m.items) {
		si = m.items[m.cursor].section
	}
	rows, hlRow := renderDetail(m.analysis, m.opts.Lang, si, m.hl, m.st, m.detail.Width)
	m.detail.SetContent(strings.Join(rows, "\n"))
	m.detail.GotoTop()
	if hlRow >= 0 {
		m.detail.SetYOffset(max(0, hlRow-m.detail.Height/3))
	}
}

func (m *Model) refreshMap() {
	m.mapView.SetContent(strings.Join(renderMap(m.analysis, m.st, m.mapView.Width), "\n"))
}

func (m Model) View() string {
	if m.width == 0 {
		return m.t("Loading…", "Cargando…")
	}
	if m.loadErr != nil {
		return m.st.err.Render(m.t("Could not read the configuration: ", "No se pudo leer la configuración: ")) + m.loadErr.Error() + "\n\n" + m.st.dim.Render(m.t("r retry · q quit", "r reintentar · q salir")) + "\n"
	}
	if m.showHelp {
		return lipgloss.JoinVertical(lipgloss.Left, m.st.title.Render(m.t("haro · help", "haro · ayuda")), "", m.help.FullHelpView(m.keys.FullHelp()), "", m.st.dim.Render(m.t("Press any key to go back.", "Cualquier tecla para volver.")))
	}
	var body string
	h := m.contentHeight()
	switch m.view {
	case viewProblems:
		body = m.viewProblems(h)
	case viewAssist:
		body = m.viewAssist(h)
	case viewMap:
		body = m.st.pane.Width(m.width - 2).Height(h + 1).Render(m.titled(m.t("Traffic map", "Mapa del tráfico"), m.mapView.View()))
	default:
		left := m.listPane(h)
		right := m.detail.View()
		rs := m.st.pane
		ls := m.st.pane
		if m.focus == focusDetail {
			rs = m.st.paneFocus
		} else {
			ls = m.st.paneFocus
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			ls.Width(m.leftWidth()).Height(h+1).Render(m.titled(m.t("Sections", "Secciones"), left)),
			rs.Width(m.width-m.leftWidth()-4).Height(h+1).Render(m.titled(m.t("Detail", "Detalle"), right)))
	}
	return body + "\n" + m.statusBar()
}

// titled prepends a title to a pane's content.
func (m Model) titled(title, content string) string {
	return m.st.title.Render(title) + "\n" + content
}

func (m Model) listPane(h int) string {
	rows := make([]string, 0, len(m.items))
	counts := map[int][2]int{} // section → errors, warnings
	for _, i := range m.analysis.Issues {
		c := counts[i.Section]
		switch i.Severity {
		case core.SevError:
			c[0]++
		case core.SevWarn:
			c[1]++
		}
		counts[i.Section] = c
	}
	w := m.leftWidth() - 2
	visible := max(1, h)
	offset := follow(m.cursor, m.offset, visible)
	for n, it := range m.items {
		if n < offset || n >= offset+visible {
			continue
		}
		var row string
		if it.section < 0 {
			row = m.st.dim.Render("── " + it.label)
		} else {
			s := m.analysis.Doc.Sections[it.section]
			mark := ""
			if c := counts[it.section]; c[0] > 0 {
				mark = " " + m.st.err.Render("!")
			} else if c[1] > 0 {
				mark = " " + m.st.warn.Render("!")
			}
			label := lipgloss.NewStyle().MaxWidth(max(4, w-3)).Render(s.Type + " " + s.Name)
			row = sectionColor(s.Type).Render(label) + mark
			if n == m.cursor {
				row = m.st.cursor.Render(strings.TrimSpace(ansiStrip(row)))
			}
			row = "  " + row
		}
		rows = append(rows, row)
	}
	if len(m.items) == 0 {
		return m.st.dim.Render(m.t("(no sections)", "(sin secciones)"))
	}
	return strings.Join(rows, "\n")
}

func (m Model) viewProblems(h int) string {
	issues := m.filteredIssues()
	title := fmt.Sprintf(m.t("Issues · %s (%d)", "Problemas · %s (%d)"), m.sevFilterName(), len(issues))
	if len(issues) == 0 {
		msg := m.st.ok.Render(m.t("✓ No issues detected.", "✓ Sin problemas detectados.")) + " " + m.st.dim.Render(m.t("Always validate with haproxy -c.", "Valida siempre con haproxy -c."))
		return m.st.pane.Width(m.width - 2).Height(h + 1).Render(m.titled(title, msg))
	}
	visible := max(1, h)
	offset := follow(m.issueCursor, m.issueOffset, visible)
	var rows []string
	for n := offset; n < len(issues) && n < offset+visible; n++ {
		i := issues[n]
		where := fmt.Sprintf("%s:%d", fileLabel(m.analysis, i.File), i.Line)
		line := fmt.Sprintf("%-10s %-22s %s %s", m.st.sevLabel(i.Severity), where, i.Message, m.st.dim.Render("["+i.Rule+"]"))
		line = ansi.Truncate(line, m.width-4, "…")
		if n == m.issueCursor {
			line = m.st.cursor.Render(ansiStrip(line))
		} else {
			line = m.st.sev(i.Severity).Render(fmt.Sprintf("%-10s", m.st.sevLabel(i.Severity))) + strings.TrimPrefix(line, fmt.Sprintf("%-10s", m.st.sevLabel(i.Severity)))
		}
		rows = append(rows, line)
	}
	return m.st.pane.Width(m.width - 2).Height(h).Render(m.titled(title, strings.Join(rows, "\n")))
}

func (m Model) statusBar() string {
	var errs, warns int
	for _, i := range m.analysis.Issues {
		switch i.Severity {
		case core.SevError:
			errs++
		case core.SevWarn:
			warns++
		}
	}
	summary := fmt.Sprintf("%d %s · %d %s", errs, plural(errs, m.t("error", "error"), m.t("errors", "errores")), warns, plural(warns, m.t("warning", "aviso"), m.t("warnings", "avisos")))
	switch {
	case errs > 0:
		summary = m.st.err.Render(summary)
	case warns > 0:
		summary = m.st.warn.Render(summary)
	default:
		summary = m.st.ok.Render(summary)
	}
	keys := m.help.ShortHelpView(m.keys.ShortHelp())
	if m.view == viewAssist && m.status == "" {
		return summary + "   " + m.assistStatus()
	}
	if m.status != "" {
		return summary + "   " + m.status
	}
	return summary + "   " + keys
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ansiStrip removes style sequences, so a whole row can be painted with the
// cursor style.
func ansiStrip(s string) string { return ansiRe.ReplaceAllString(s, "") }
