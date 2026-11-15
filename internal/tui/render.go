package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/pkarc/haro/core"
)

var (
	reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reCode = regexp.MustCompile("`([^`]+)`")
)

// rich convierte el markdown mínimo de las explicaciones (**negrita**, `código`).
func (s styles) rich(t string) string {
	t = reBold.ReplaceAllStringFunc(t, func(m string) string { return s.bold.Render(m[2 : len(m)-2]) })
	return reCode.ReplaceAllStringFunc(t, func(m string) string { return s.code.Render(m[1 : len(m)-1]) })
}

type lineKey struct{ file, line int }

// lineRef identifica una línea de un archivo para resaltarla.
type lineRef struct{ file, line int }

// fileLabel es el nombre corto de un archivo.
func fileLabel(a core.Analysis, i int) string {
	if i >= 0 && i < len(a.Doc.Files) {
		return filepath.Base(a.Doc.Files[i])
	}
	return ""
}

// wrap ajusta un texto con estilos al ancho dado, respetando las secuencias ANSI.
func wrap(t string, width int) []string {
	if width < 10 {
		width = 10
	}
	return strings.Split(ansi.Wrap(t, width, ""), "\n")
}

// wrapIndent ajusta prefix+text al ancho; las líneas de continuación se sangran
// con el ancho visible de prefix.
func wrapIndent(prefix, text string, width int) []string {
	pad := strings.Repeat(" ", ansi.StringWidth(prefix))
	lines := wrap(text, max(10, width-len(pad)))
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = pad + lines[i]
		}
	}
	return lines
}

// renderDetail devuelve las líneas del panel de detalle de la sección si y la
// fila de la línea resaltada (-1 si no hay): cabecera, modo efectivo, y cada
// directiva con su explicación y los problemas de esa línea.
func renderDetail(a core.Analysis, lang core.Lang, si int, hl *lineRef, st styles, width int) (rows []string, hlRow int) {
	hlRow = -1
	if si < 0 || si >= len(a.Doc.Sections) {
		return nil, hlRow
	}
	s := a.Doc.Sections[si]
	kt := core.KB.Text(lang)
	info := kt.Sections[s.Type]
	head := sectionColor(s.Type).Bold(true).Render(strings.TrimSpace(s.Type + " " + s.Name))
	if info.Title != "" {
		head += "  " + st.dim.Render(info.Title)
	}
	rows = append(rows, wrap(head, width)...)
	if info.Description != "" {
		rows = append(rows, wrap(st.dim.Render(info.Description), width)...)
	}
	if si < len(a.Effective) && (s.Type == "frontend" || s.Type == "backend" || s.Type == "listen" || s.Type == "defaults") {
		e := a.Effective[si]
		from := map[string]string{"own": "propio", "defaults": "heredado de defaults", "implicit": "por defecto (tcp)"}[e.ModeFrom]
		rows = append(rows, st.dim.Render(fmt.Sprintf("modo %s (%s)", e.Mode, from)))
	}
	rows = append(rows, "")

	ex := map[lineKey]core.LineExplanation{}
	for _, e := range a.Explanations {
		ex[lineKey{e.File, e.Line}] = e
	}
	iss := map[lineKey][]core.Issue{}
	for _, i := range a.Issues {
		iss[lineKey{i.File, i.Line}] = append(iss[lineKey{i.File, i.Line}], i)
	}
	// Problemas de la cabecera de la sección.
	for _, i := range iss[lineKey{s.File, s.Line}] {
		rows = append(rows, wrapIndent("     "+st.sev(i.Severity).Render(sevLabel(i.Severity))+" ", i.Message, width)...)
	}
	for _, l := range s.Lines {
		if l.Kind == core.KindBlank {
			continue
		}
		k := lineKey{s.File, l.Line}
		mark := "  "
		isHL := hl != nil && hl.file == s.File && hl.line == l.Line
		if isHL {
			mark = "▶ "
			hlRow = len(rows)
		}
		text := l.Indent + l.Text
		if l.Kind == core.KindComment {
			text = st.dim.Render(strings.TrimSpace(text))
		}
		line := fmt.Sprintf("%s%s %s", mark, st.dim.Render(fmt.Sprintf("%4d", l.Line)), text)
		if isHL {
			line = st.hl.Render(line)
		}
		rows = append(rows, wrap(line, width)...)
		if e, ok := ex[k]; ok {
			label := kt.Categories[e.Explain.Category]
			prefix := "       "
			if label != "" {
				prefix += st.tag.Render("[" + label + "] ")
			}
			rows = append(rows, wrapIndent(prefix, st.rich(e.Explain.Text), width)...)
		}
		for _, i := range iss[k] {
			rows = append(rows, wrapIndent("       "+st.sev(i.Severity).Render(sevLabel(i.Severity))+" ", i.Message, width)...)
		}
	}
	return rows, hlRow
}

// renderMap dibuja el mapa de tráfico como árbol de texto:
// entrada ──si cond──▶ backend ──▶ servidores.
func renderMap(a core.Analysis, st styles, width int) []string {
	t := a.Topology
	var rows []string
	add := func(s string) { rows = append(rows, wrap(s, width)...) }
	if len(t.Entries) == 0 {
		return []string{st.dim.Render("No hay frontends ni listen en la configuración.")}
	}
	backend := map[int]core.BackendNode{}
	for _, b := range t.Backends {
		backend[b.Section] = b
	}
	listen := map[int]core.EntryNode{}
	for _, e := range t.Entries {
		listen[e.Section] = e
	}
	servers := func(prefix string, srv []core.ServerNode) {
		for i, v := range srv {
			branch := "├─ "
			if i == len(srv)-1 {
				branch = "└─ "
			}
			flags := ""
			if v.Backup {
				flags += " backup"
			}
			if v.Disabled {
				flags += " deshabilitado"
			}
			if v.Check {
				flags += " check"
			}
			add(prefix + st.dim.Render(branch) + v.Name + " " + st.dim.Render(v.Address+flags))
		}
	}
	for n, e := range t.Entries {
		if n > 0 {
			rows = append(rows, "")
		}
		binds := make([]string, len(e.Binds))
		for i, b := range e.Binds {
			binds[i] = b.Address
			if b.TLS {
				binds[i] += " 🔒"
			}
		}
		head := sectionColor(e.Type).Bold(true).Render(e.Type + " " + e.Name)
		if len(binds) > 0 {
			head += "  " + st.dim.Render(strings.Join(binds, ", "))
		}
		add(head)
		var edges []core.Edge
		for _, ed := range t.Edges {
			if ed.From == e.Section {
				edges = append(edges, ed)
			}
		}
		if e.Type == "listen" && len(edges) == 0 {
			servers("  ", e.Servers)
		}
		for i, ed := range edges {
			branch, cont := "├─", "│  "
			if i == len(edges)-1 {
				branch, cont = "└─", "   "
			}
			arrow := "──▶"
			label := "por defecto"
			if !ed.Default {
				label = "si " + ed.Condition
				if strings.HasPrefix(ed.Condition, "salvo ") {
					label = ed.Condition
				}
			}
			target := sectionColor("backend").Render("backend " + ed.ToName)
			if ed.To < 0 {
				target = st.err.Render("✗ " + ed.ToName + " (no existe)")
			}
			add("  " + st.dim.Render(branch+" "+label+" "+arrow) + " " + target)
			if b, ok := backend[ed.To]; ok {
				add("  " + st.dim.Render(cont) + "   " + st.dim.Render("balance "+b.Balance))
				servers("  "+st.dim.Render(cont)+"   ", b.Servers)
			} else if l, ok := listen[ed.To]; ok {
				servers("  "+st.dim.Render(cont)+"   ", l.Servers)
			}
		}
	}
	return rows
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)
