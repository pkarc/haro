package tui

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"

	"github.com/pkarc/haro/core"
)

func init() { lipgloss.SetColorProfile(termenv.Ascii) }

const problems = `defaults
    mode http
    timeout connect 5s
    timeout client 30s
    timeout server 30s

frontend web
    bind *:80
    acl is_api path_beg /api
    use_backend api if is_api
    default_backend nope

backend app
    server a1 10.0.0.1:80 check
`

func loader(text string) func() ([]core.Source, error) {
	return func() ([]core.Source, error) { return []core.Source{{Path: "/tmp/x.cfg", Text: text}}, nil }
}

// model carga la configuración y devuelve el modelo listo, con tamaño de pantalla.
func model(t *testing.T, text string) Model {
	t.Helper()
	m := New(Options{Load: loader(text), NoWatch: true, Editor: "vi"})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(m.load())
	return tm.(Model)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	var tm tea.Model = m
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		tm, _ = tm.Update(msg)
	}
	return tm.(Model)
}

func TestNavigationSkipsFileHeaders(t *testing.T) {
	srcs := func() ([]core.Source, error) {
		return []core.Source{
			{Path: "/a/10-a.cfg", Text: "backend a\n    server s 1.1.1.1:80\n"},
			{Path: "/a/20-b.cfg", Text: "backend b\n    server s 2.2.2.2:80\n"},
		}, nil
	}
	m := New(Options{Load: srcs, NoWatch: true})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(m.load())
	m = tm.(Model)
	if len(m.items) != 4 || m.items[0].section != -1 {
		t.Fatalf("la lista debería agrupar por archivo: %+v", m.items)
	}
	if s := m.currentSection(); s == nil || s.Name != "a" {
		t.Fatalf("el cursor debe empezar en la primera sección, no en la cabecera: %+v", s)
	}
	m = press(t, m, "j")
	if s := m.currentSection(); s == nil || s.Name != "b" {
		t.Errorf("j debe saltar la cabecera del segundo archivo: %+v", s)
	}
	m = press(t, m, "j", "j")
	if s := m.currentSection(); s == nil || s.Name != "b" {
		t.Errorf("al final no debe moverse: %+v", s)
	}
	m = press(t, m, "k", "k", "k")
	if s := m.currentSection(); s == nil || s.Name != "a" {
		t.Errorf("al inicio debe quedarse en la primera sección: %+v", s)
	}
}

func TestDetailShowsExplanationAndIssues(t *testing.T) {
	m := model(t, problems)
	// frontend web
	m = press(t, m, "j")
	out := m.View()
	for _, want := range []string{"frontend web", "bind *:80", "default_backend nope", "El backend «nope» no existe"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en el detalle:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "error") {
		t.Errorf("el resumen debe mostrar errores:\n%s", out)
	}
}

func TestProblemsJumpHighlightsLine(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "p")
	out := m.View()
	if !strings.Contains(out, "Problemas · todos") || !strings.Contains(out, "[backend-missing]") {
		t.Fatalf("la vista de problemas no se muestra:\n%s", out)
	}
	// Filtrar por errores deja solo errores.
	m = press(t, m, "f")
	if n := len(m.filteredIssues()); n == 0 {
		t.Fatal("el filtro de errores no debería estar vacío")
	}
	for _, i := range m.filteredIssues() {
		if i.Severity != core.SevError {
			t.Errorf("el filtro de errores dejó %v", i)
		}
	}
	// Ir al problema backend-missing.
	for n, i := range m.filteredIssues() {
		if i.Rule == "backend-missing" {
			m.issueCursor = n
		}
	}
	m = press(t, m, "enter")
	if m.view != viewSections || m.hl == nil {
		t.Fatalf("Enter debe abrir la sección y resaltar: view=%v hl=%v", m.view, m.hl)
	}
	if s := m.currentSection(); s == nil || s.Name != "web" {
		t.Errorf("debe seleccionar frontend web: %+v", s)
	}
	if !strings.Contains(m.View(), "▶") {
		t.Errorf("la línea resaltada debe marcarse con ▶:\n%s", m.View())
	}
	// Esc o p vuelven; p dos veces cierra la vista.
	m = press(t, m, "p", "p")
	if m.view != viewSections {
		t.Error("p debe alternar la vista de problemas")
	}
}

func TestMapView(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "m")
	out := m.View()
	for _, want := range []string{"Mapa del tráfico", "frontend web", "si is_api", "✗ nope (no existe)", "por defecto"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en el mapa:\n%s", want, out)
		}
	}
}

func TestRenderMapTree(t *testing.T) {
	a := core.Analyze("frontend f\n    bind *:80\n    use_backend b if x\n    default_backend b\n\nbackend b\n    server s1 1.1.1.1:80 check\n    server s2 1.1.1.2:80 backup\n")
	got := strings.Join(renderMap(a, newStyles(), 100), "\n")
	for _, want := range []string{"frontend f  *:80", "├─ si x ──▶ backend b", "└─ por defecto ──▶ backend b", "balance roundrobin", "├─ s1 1.1.1.1:80 check", "└─ s2 1.1.1.2:80 backup"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q:\n%s", want, got)
		}
	}
}

func TestHelpAndQuit(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "?")
	if !strings.Contains(m.View(), "haro · ayuda") {
		t.Error("? debe mostrar la ayuda")
	}
	m = press(t, m, "x") // cualquier tecla vuelve
	if strings.Contains(m.View(), "haro · ayuda") {
		t.Error("la ayuda debe cerrarse con cualquier tecla")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil || cmd() != tea.Quit() {
		t.Error("q debe salir")
	}
}

func TestEditorCommand(t *testing.T) {
	cases := []struct {
		editor string
		want   string
	}{
		{"vim", "vim +7 /tmp/a.cfg"},
		{"nvim -u NONE", "nvim -u NONE +7 /tmp/a.cfg"},
		{"/usr/bin/nano", "/usr/bin/nano +7 /tmp/a.cfg"},
		{"emacs", "emacs +7 /tmp/a.cfg"},
		{"code", "code --wait -g /tmp/a.cfg:7"},
		{"hx", "hx /tmp/a.cfg:7"},
		{"subl", "subl --wait /tmp/a.cfg:7"},
		{"", "vi +7 /tmp/a.cfg"},
	}
	for _, c := range cases {
		if got := strings.Join(editorCommand(c.editor, "/tmp/a.cfg", 7), " "); got != c.want {
			t.Errorf("editorCommand(%q) = %q, quería %q", c.editor, got, c.want)
		}
	}
}

func TestEditUsesSelectionAndHighlight(t *testing.T) {
	m := model(t, problems)
	m = press(t, m, "j") // frontend web, cabecera en la línea 7
	_, cmd := m.edit()
	if cmd == nil {
		t.Fatal("e debe lanzar el editor")
	}
	// Con una línea resaltada, el editor abre esa línea.
	m.hl = &lineRef{file: 0, line: 11}
	if args := editorCommand(m.opts.Editor, m.srcs[m.hl.file].Path, m.hl.line); args[1] != "+11" {
		t.Errorf("args = %v", args)
	}
}

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if !noColor(false) {
		t.Error("NO_COLOR debe desactivar los colores")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if !noColor(false) {
		t.Error("TERM=dumb debe desactivar los colores")
	}
	t.Setenv("TERM", "xterm")
	if noColor(false) || !noColor(true) {
		t.Error("--no-color debe forzarlo y su ausencia no")
	}
}

// Recarga automática: al cambiar el archivo, la interfaz muestra el contenido
// nuevo sin que el usuario haga nada. Se ejecuta con el programa real de
// Bubble Tea (teatest).
func TestWatchReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "haproxy.cfg")
	os.WriteFile(path, []byte("backend uno\n    server s 1.1.1.1:80\n"), 0o644)
	load := func() ([]core.Source, error) {
		b, err := os.ReadFile(path)
		return []core.Source{{Path: path, Text: string(b)}}, err
	}
	tm := teatest.NewTestModel(t, New(Options{Load: load}), teatest.WithInitialTermSize(100, 30))
	waitFor(t, tm, "backend uno")
	os.WriteFile(path, []byte("backend dos\n    server s 1.1.1.1:80\n"), 0o644)
	waitFor(t, tm, "backend dos")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func waitFor(t *testing.T, tm *teatest.TestModel, s string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte(s)) },
		teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(50*time.Millisecond))
}

func TestLoadErrorIsShown(t *testing.T) {
	m := New(Options{Load: func() ([]core.Source, error) { return nil, io.ErrUnexpectedEOF }, NoWatch: true})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	tm, _ = tm.Update(m.load())
	if out := tm.View(); !strings.Contains(out, "No se pudo leer la configuración") {
		t.Errorf("el error de carga debe mostrarse:\n%s", out)
	}
}

func TestListScrollIsRemembered(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("backend b")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(string(rune('a' + i/26)))
		b.WriteString("\n    server s 1.1.1.1:80\n\n")
	}
	m := New(Options{Load: loader(b.String()), NoWatch: true})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	tm, _ = tm.Update(m.load())
	m = tm.(Model)
	m = press(t, m, "G")
	if m.offset == 0 {
		t.Fatal("al ir al final la ventana debe desplazarse")
	}
	bottom := m.offset
	m = press(t, m, "k")
	if m.offset != bottom {
		t.Errorf("subir una fila no debe mover la ventana: %d → %d", bottom, m.offset)
	}
}

// Con configuraciones reales (si se descargó el corpus) ninguna vista debe
// fallar, con cualquier ancho de pantalla.
func TestCorpusRenders(t *testing.T) {
	files, _ := filepath.Glob("../../core/testdata/corpus/*/*.cfg")
	deep, _ := filepath.Glob("../../core/testdata/corpus/*/*/*.cfg")
	files = append(files, deep...)
	if len(files) == 0 {
		t.Skip("sin corpus; ejecuta scripts/fetch-corpus.sh")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, w := range []int{20, 60, 140} {
			m := New(Options{Load: loader(string(b)), NoWatch: true})
			var tm tea.Model = m
			tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: 12})
			tm, _ = tm.Update(m.load())
			m = tm.(Model)
			_ = m.View()
			for _, k := range []string{"G", "p", "enter", "m", "esc", "?", "x"} {
				m = press(t, m, k)
				_ = m.View()
			}
		}
	}
}

func TestDetailInEnglish(t *testing.T) {
	m := New(Options{Load: loader(problems), NoWatch: true, Editor: "vi", Lang: core.LangEN})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(m.load())
	tm = press(t, tm.(Model), "j")
	out := tm.View()
	for _, want := range []string{"The backend “nope” does not exist.", "Traffic entry point", "Default backend"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en el detalle en inglés:\n%s", want, out)
		}
	}
	if strings.Contains(out, "no existe") {
		t.Errorf("restos de español:\n%s", out)
	}
}
