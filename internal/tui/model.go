package tui

import (
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

// Options configura la interfaz.
type Options struct {
	// Load lee la configuración (en orden de carga). Se vuelve a llamar al
	// recargar. Source.Path debe ser la ruta real del archivo.
	Load func() ([]core.Source, error)
	// Dirs son los directorios pasados como argumento: se vigilan para detectar
	// archivos nuevos o borrados.
	Dirs []string
	// Editor sobrescribe $VISUAL/$EDITOR (vacío = del entorno).
	Editor string
	// Lang es el idioma de las explicaciones y mensajes del detector (vacío = español).
	Lang core.Lang
	// NoColor desactiva los colores.
	NoColor bool
	// NoWatch desactiva la recarga automática.
	NoWatch bool
}

type view int

const (
	viewSections view = iota
	viewProblems
	viewMap
)

// focus indica qué panel recibe las teclas de desplazamiento en la vista de secciones.
type focus int

const (
	focusList focus = iota
	focusDetail
)

type keyMap struct {
	Up, Down, Top, Bottom, Tab, Problems, Map, Edit, Filter, Enter, Back, Help, Quit, Reload key.Binding
}

func newKeys() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "arriba")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "abajo")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "inicio")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "final")),
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "panel")),
		Problems: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "problemas")),
		Map:      key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mapa")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "editar")),
		Filter:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filtrar gravedad")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "ir a la sección")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "volver")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "ayuda")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "salir")),
		Reload:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "recargar")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Problems, k.Map, k.Edit, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.Tab},
		{k.Problems, k.Filter, k.Enter, k.Map, k.Back},
		{k.Edit, k.Reload, k.Help, k.Quit},
	}
}

// item es una fila de la lista de secciones: una cabecera de archivo
// (section = -1) o una sección.
type item struct {
	section int
	file    int
	label   string
}

// Model es el modelo de Bubble Tea.
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
	cursor int // posición en items
	offset int // primera fila visible de la lista

	detail viewport.Model
	hl     *lineRef

	issueCursor int
	issueOffset int
	sevFilter   int // 0 todos, 1 errores, 2 avisos, 3 sugerencias

	mapView viewport.Model

	status string
	w      *watcher
}

// New crea el modelo; la configuración se carga en Init.
func New(opts Options) Model {
	applyColorProfile(opts.NoColor)
	if opts.Editor == "" {
		opts.Editor = defaultEditor()
	}
	h := help.New()
	return Model{opts: opts, st: newStyles(), keys: newKeys(), help: h, detail: viewport.New(0, 0), mapView: viewport.New(0, 0)}
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
			m.status = "El editor terminó con error: " + msg.err.Error()
		}
		return m, m.load

	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

// setSources analiza la configuración y conserva, si puede, la sección
// seleccionada (por tipo y nombre).
func (m *Model) setSources(srcs []core.Source) {
	prevKey := ""
	if s := m.currentSection(); s != nil {
		prevKey = s.Type + "\x00" + s.Name
	}
	m.srcs = srcs
	m.analysis = core.AnalyzeConfigLang(srcs, m.opts.Lang)
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

// buildItems arma las filas de la lista; con varios archivos agrupa por archivo.
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

// skipHeader mueve el cursor fuera de las cabeceras de archivo, en la dirección dada.
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
	if m.items[m.cursor].section < 0 { // no hay más secciones en esa dirección
		m.cursor = prev
	}
	m.hl = nil
	m.syncScroll()
	m.refreshDetail()
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// follow devuelve el primer elemento visible de una lista con scroll para que
// el cursor quede dentro de la ventana, moviéndola lo mínimo.
func follow(cursor, offset, visible int) int {
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+visible {
		return cursor - visible + 1
	}
	return offset
}

// syncScroll recuerda la posición de scroll de las listas tras mover un cursor.
func (m *Model) syncScroll() {
	visible := m.contentHeight()
	m.offset = follow(m.cursor, m.offset, visible)
	m.issueOffset = follow(m.issueCursor, m.issueOffset, visible)
}

// sevFilterName es el nombre del filtro de gravedad actual.
func (m Model) sevFilterName() string {
	return [...]string{"todos", "errores", "avisos", "sugerencias"}[m.sevFilter]
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

// jumpToIssue abre la sección del problema y resalta su línea.
func (m *Model) jumpToIssue(i core.Issue) {
	if i.Section < 0 {
		m.status = fmt.Sprintf("El problema está fuera de cualquier sección (%s:%d)", fileLabel(m.analysis, i.File), i.Line)
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

// edit abre el editor en el archivo y la línea seleccionados.
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
		m.status = "No hay nada que editar"
		return m, nil
	}
	args := editorCommand(m.opts.Editor, m.srcs[file].Path, line)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// ---------- disposición y vistas ----------

func (m Model) leftWidth() int { return clamp(m.width/3, 22, 42) }

// contentHeight es el alto del contenido de un panel: la pantalla menos los
// bordes (2), el título (1) y la barra de estado (1).
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
		return "Cargando…"
	}
	if m.loadErr != nil {
		return m.st.err.Render("No se pudo leer la configuración: ") + m.loadErr.Error() + "\n\n" + m.st.dim.Render("r reintentar · q salir") + "\n"
	}
	if m.showHelp {
		return lipgloss.JoinVertical(lipgloss.Left, m.st.title.Render("haro · ayuda"), "", m.help.FullHelpView(m.keys.FullHelp()), "", m.st.dim.Render("Cualquier tecla para volver."))
	}
	var body string
	h := m.contentHeight()
	switch m.view {
	case viewProblems:
		body = m.viewProblems(h)
	case viewMap:
		body = m.st.pane.Width(m.width - 2).Height(h + 1).Render(m.titled("Mapa del tráfico", m.mapView.View()))
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
			ls.Width(m.leftWidth()).Height(h+1).Render(m.titled("Secciones", left)),
			rs.Width(m.width-m.leftWidth()-4).Height(h+1).Render(m.titled("Detalle", right)))
	}
	return body + "\n" + m.statusBar()
}

// titled antepone un título al contenido de un panel.
func (m Model) titled(title, content string) string {
	return m.st.title.Render(title) + "\n" + content
}

func (m Model) listPane(h int) string {
	rows := make([]string, 0, len(m.items))
	counts := map[int][2]int{} // sección → errores, avisos
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
		return m.st.dim.Render("(sin secciones)")
	}
	return strings.Join(rows, "\n")
}

func (m Model) viewProblems(h int) string {
	issues := m.filteredIssues()
	title := fmt.Sprintf("Problemas · %s (%d)", m.sevFilterName(), len(issues))
	if len(issues) == 0 {
		msg := m.st.ok.Render("✓ Sin problemas detectados.") + " " + m.st.dim.Render("Valida siempre con haproxy -c.")
		return m.st.pane.Width(m.width - 2).Height(h + 1).Render(m.titled(title, msg))
	}
	visible := max(1, h)
	offset := follow(m.issueCursor, m.issueOffset, visible)
	var rows []string
	for n := offset; n < len(issues) && n < offset+visible; n++ {
		i := issues[n]
		where := fmt.Sprintf("%s:%d", fileLabel(m.analysis, i.File), i.Line)
		line := fmt.Sprintf("%-10s %-22s %s %s", sevLabel(i.Severity), where, i.Message, m.st.dim.Render("["+i.Rule+"]"))
		line = ansi.Truncate(line, m.width-4, "…")
		if n == m.issueCursor {
			line = m.st.cursor.Render(ansiStrip(line))
		} else {
			line = m.st.sev(i.Severity).Render(fmt.Sprintf("%-10s", sevLabel(i.Severity))) + strings.TrimPrefix(line, fmt.Sprintf("%-10s", sevLabel(i.Severity)))
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
	summary := fmt.Sprintf("%d %s · %d %s", errs, plural(errs, "error", "errores"), warns, plural(warns, "aviso", "avisos"))
	switch {
	case errs > 0:
		summary = m.st.err.Render(summary)
	case warns > 0:
		summary = m.st.warn.Render(summary)
	default:
		summary = m.st.ok.Render(summary)
	}
	keys := m.help.ShortHelpView(m.keys.ShortHelp())
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

// ansiStrip quita las secuencias de estilo, para poder pintar una fila entera
// con el estilo del cursor.
func ansiStrip(s string) string { return ansiRe.ReplaceAllString(s, "") }
