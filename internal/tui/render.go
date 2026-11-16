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

// rich converts the minimal markdown of the explanations (**bold**, `code`).
func (s styles) rich(t string) string {
	t = reBold.ReplaceAllStringFunc(t, func(m string) string { return s.bold.Render(m[2 : len(m)-2]) })
	return reCode.ReplaceAllStringFunc(t, func(m string) string { return s.code.Render(m[1 : len(m)-1]) })
}

type lineKey struct{ file, line int }

// lineRef identifies a file line so it can be highlighted.
type lineRef struct{ file, line int }

// fileLabel is the short name of a file.
func fileLabel(a core.Analysis, i int) string {
	if i >= 0 && i < len(a.Doc.Files) {
		return filepath.Base(a.Doc.Files[i])
	}
	return ""
}

// wrap fits styled text to the given width, respecting ANSI sequences.
func wrap(t string, width int) []string {
	if width < 10 {
		width = 10
	}
	return strings.Split(ansi.Wrap(t, width, ""), "\n")
}

// wrapIndent fits prefix+text to the width; continuation lines are indented
// by the visible width of prefix.
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

// renderDetail returns the lines of the detail pane for section si and the
// row of the highlighted line (-1 if none): header, effective mode, and each
// directive with its explanation and the issues on that line.
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
		from := map[string]string{"own": st.t("own", "propio"), "defaults": st.t("inherited from defaults", "heredado de defaults"), "implicit": st.t("default (tcp)", "por defecto (tcp)")}[e.ModeFrom]
		rows = append(rows, st.dim.Render(fmt.Sprintf(st.t("mode %s (%s)", "modo %s (%s)"), e.Mode, from)))
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
	// Issues on the section header.
	for _, i := range iss[lineKey{s.File, s.Line}] {
		rows = append(rows, wrapIndent("     "+st.sev(i.Severity).Render(st.sevLabel(i.Severity))+" ", i.Message, width)...)
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
			rows = append(rows, wrapIndent("       "+st.sev(i.Severity).Render(st.sevLabel(i.Severity))+" ", i.Message, width)...)
		}
	}
	return rows, hlRow
}

// renderMap draws the traffic map as a text tree:
// entry ──if cond──▶ backend ──▶ servers.
func renderMap(a core.Analysis, st styles, width int) []string {
	t := a.Topology
	var rows []string
	add := func(s string) { rows = append(rows, wrap(s, width)...) }
	if len(t.Entries) == 0 {
		return []string{st.dim.Render(st.t("There are no frontends or listens in the configuration.", "No hay frontends ni listen en la configuración."))}
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
				flags += st.t(" disabled", " deshabilitado")
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
			label := st.t("default", "por defecto")
			if !ed.Default {
				label = st.t("if ", "si ") + ed.Condition
				if strings.HasPrefix(ed.Condition, "salvo ") || strings.HasPrefix(ed.Condition, "unless ") {
					label = ed.Condition
				}
			}
			target := sectionColor("backend").Render("backend " + ed.ToName)
			if ed.To < 0 {
				target = st.err.Render("✗ " + ed.ToName + st.t(" (does not exist)", " (no existe)"))
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
