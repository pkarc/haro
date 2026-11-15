package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/pkarc/haro/core"
)

const defaultConfigPath = "/etc/haproxy/haproxy.cfg"

// readConfig lee un único archivo (o stdin con "-"). Lo usa diff.
func readConfig(path string) (string, string, error) {
	if path == "" {
		path = defaultConfigPath
	}
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), "stdin", err
	}
	b, err := os.ReadFile(path)
	return string(b), path, err
}

// ---------- estilos de terminal ----------

type style struct{ on bool }

func newStyle(noColor bool) style {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return style{}
	}
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return style{}
	}
	return style{on: true}
}

func (s style) wrap(code, t string) string {
	if !s.on {
		return t
	}
	return "\x1b[" + code + "m" + t + "\x1b[0m"
}
func (s style) bold(t string) string   { return s.wrap("1", t) }
func (s style) dim(t string) string    { return s.wrap("2", t) }
func (s style) red(t string) string    { return s.wrap("31", t) }
func (s style) yellow(t string) string { return s.wrap("33", t) }
func (s style) blue(t string) string   { return s.wrap("34", t) }
func (s style) cyan(t string) string   { return s.wrap("36", t) }
func (s style) green(t string) string  { return s.wrap("32", t) }

var reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)
var reCode = regexp.MustCompile("`([^`]+)`")

// rich convierte el markdown mínimo de las explicaciones en estilos de terminal.
func (s style) rich(t string) string {
	t = reBold.ReplaceAllStringFunc(t, func(m string) string { return s.bold(m[2 : len(m)-2]) })
	return reCode.ReplaceAllStringFunc(t, func(m string) string { return s.cyan(m[1 : len(m)-1]) })
}

func (s style) sev(sev core.Severity) string {
	switch sev {
	case core.SevError:
		return s.red("error")
	case core.SevWarn:
		return s.yellow("aviso")
	default:
		return s.blue("sugerencia")
	}
}

// ---------- idioma ----------

// langFlag registra --lang en fs. El valor por defecto sale del entorno
// (LC_ALL, LC_MESSAGES, LANG); resolve lo convierte en core.Lang.
func langFlag(fs *flag.FlagSet) *string {
	return fs.String("lang", "", "idioma de las explicaciones y mensajes del detector: es | en (por defecto, según LC_ALL/LANG)")
}

func resolveLang(v string) (core.Lang, error) {
	if v == "" {
		return core.LangFromEnv(), nil
	}
	if l, ok := core.ParseLang(v); ok {
		return l, nil
	}
	return "", fmt.Errorf("idioma desconocido %q (usa es o en)", v)
}

// ---------- lint ----------

func runLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	format := fs.String("format", "text", "formato de salida: text | json | sarif | markdown")
	disable := fs.String("disable", "", "reglas a ignorar, separadas por comas (p. ej. no-global,tls-no-alpn)")
	strict := fs.Bool("strict", false, "termina con código 1 también si hay avisos")
	noColor := fs.Bool("no-color", false, "sin colores")
	listRules := fs.Bool("rules", false, "lista las reglas disponibles y sale")
	langArg := langFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Uso: haro lint [opciones] [archivo|directorio|-]...")
		fmt.Fprintln(fs.Output(), "Varios archivos o directorios se analizan juntos, en el orden de carga de haproxy -f.")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	st := newStyle(*noColor)
	if *listRules {
		rules := core.RulesFor(lang)
		ids := make([]string, 0, len(rules))
		for id := range rules {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("%-18s %s\n", id, rules[id])
		}
		return nil
	}
	if f := *format; f != "text" && f != "json" && f != "sarif" && f != "markdown" {
		return fmt.Errorf("formato desconocido %q (usa text, json, sarif o markdown)", f)
	}
	srcs, err := readSources(fs.Args())
	if err != nil {
		return err
	}
	off := map[string]bool{}
	for _, r := range strings.Split(*disable, ",") {
		if r = strings.TrimSpace(r); r != "" {
			off[r] = true
		}
	}
	var issues []core.Issue
	for _, i := range core.LintLang(core.NewConfig(srcs).Merge(), lang) {
		if !off[i.Rule] {
			issues = append(issues, i)
		}
	}
	if issues == nil {
		issues = []core.Issue{}
	}
	counts := map[core.Severity]int{}
	for _, i := range issues {
		counts[i.Severity]++
	}
	if *format == "sarif" {
		if err := writeSARIF(os.Stdout, srcs, issues, lang); err != nil {
			return err
		}
	} else if *format == "markdown" {
		writeMarkdown(os.Stdout, srcs, issues)
	} else if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		type jsonIssue struct {
			Path string `json:"path"`
			core.Issue
		}
		out := make([]jsonIssue, len(issues))
		for k, i := range issues {
			out[k] = jsonIssue{srcs[i.File].Path, i}
		}
		paths := make([]string, len(srcs))
		for k, s := range srcs {
			paths[k] = s.Path
		}
		enc.Encode(map[string]any{"files": paths, "issues": out})
	} else {
		lines := make([][]string, len(srcs))
		for k, s := range srcs {
			lines[k] = strings.Split(s.Text, "\n")
		}
		for _, i := range issues {
			fmt.Printf("%s:%d: %s %s %s\n", srcs[i.File].Path, i.Line, st.sev(i.Severity), i.Message, st.dim("["+i.Rule+"]"))
			if l := lines[i.File]; i.Line-1 < len(l) {
				fmt.Printf("    %s\n", st.dim(strings.TrimSpace(l[i.Line-1])))
			}
		}
		if len(issues) == 0 {
			var fargs []string
			for _, s := range srcs {
				fargs = append(fargs, "-f "+s.Path)
			}
			fmt.Println(st.green("✓ Sin problemas detectados.") + st.dim(" Valida siempre con: haproxy -c "+strings.Join(fargs, " ")))
		} else {
			fmt.Printf("\n%s, %s, %s\n", plural(counts[core.SevError], "error", "errores"), plural(counts[core.SevWarn], "aviso", "avisos"), plural(counts[core.SevInfo], "sugerencia", "sugerencias"))
		}
	}
	if counts[core.SevError] > 0 || (*strict && counts[core.SevWarn] > 0) {
		return exitError(1)
	}
	return nil
}

// ---------- explain ----------

func runExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "sin colores")
	section := fs.String("section", "", "explica solo la sección con este nombre o tipo")
	format := fs.String("format", "text", "formato de salida: text | json")
	langArg := langFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Uso: haro explain [opciones] [archivo|directorio|-]...")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	srcs, err := readSources(fs.Args())
	if err != nil {
		return err
	}
	a := core.AnalyzeConfigLang(srcs, lang)
	text := core.KB.Text(lang)
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(a)
	}
	st := newStyle(*noColor)
	type key struct{ file, line int }
	byLine := map[key][]core.Issue{}
	for _, i := range a.Issues {
		byLine[key{i.File, i.Line}] = append(byLine[key{i.File, i.Line}], i)
	}
	exByLine := map[key]core.LineExplanation{}
	for _, e := range a.Explanations {
		exByLine[key{e.File, e.Line}] = e
	}
	curFile := -1
	for si, s := range a.Doc.Sections {
		if len(srcs) > 1 && s.File != curFile && (*section == "" || s.Name == *section || s.Type == *section) {
			curFile = s.File
			fmt.Println(st.dim("── " + srcs[s.File].Path + " ──"))
		}
		if *section != "" && s.Name != *section && s.Type != *section {
			continue
		}
		info := core.KB.Sections[s.Type]
		info.Title, info.Description = text.Sections[s.Type].Title, text.Sections[s.Type].Description
		head := st.bold(s.Type)
		if s.Name != "" {
			head += " " + st.bold(s.Name)
		}
		fmt.Printf("%s  %s\n", head, st.dim(info.Title))
		fmt.Printf("  %s\n", st.dim(info.Description))
		if e := a.Effective[si]; s.Type == "frontend" || s.Type == "backend" || s.Type == "listen" {
			from := map[string]string{"own": "", "defaults": " (heredado de defaults)", "implicit": " (implícito)"}[e.ModeFrom]
			fmt.Printf("  %s\n", st.dim("modo "+e.Mode+from))
		}
		for _, i := range byLine[key{s.File, s.Line}] {
			fmt.Printf("  %s %s\n", st.sev(i.Severity), i.Message)
		}
		for _, l := range s.Lines {
			if l.Kind != core.KindDirective {
				continue
			}
			e := exByLine[key{s.File, l.Line}]
			cat := text.Categories[e.Explain.Category]
			fmt.Printf("\n  %s %s %s\n", st.dim(fmt.Sprintf("%4d", l.Line)), st.cyan(l.Keyword), e.Args)
			fmt.Printf("       %s %s\n", st.dim("["+cat+"]"), st.rich(e.Explain.Text))
			for _, i := range byLine[key{s.File, l.Line}] {
				fmt.Printf("       %s %s\n", st.sev(i.Severity), i.Message)
			}
		}
		fmt.Println()
	}
	return nil
}

// ---------- diff ----------

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "sin colores")
	fs.Usage = func() { fmt.Fprintln(fs.Output(), "Uso: haro diff <antes> <después>") }
	fs.Parse(args)
	if fs.NArg() != 2 {
		fs.Usage()
		return exitError(2)
	}
	a, _, err := readConfig(fs.Arg(0))
	if err != nil {
		return err
	}
	b, _, err := readConfig(fs.Arg(1))
	if err != nil {
		return err
	}
	st := newStyle(*noColor)
	changed := false
	for _, o := range core.DiffLines(core.Normalize(a), core.Normalize(b)) {
		switch o.Op {
		case "+":
			changed = true
			fmt.Println(st.green("+ " + o.Text))
		case "-":
			changed = true
			fmt.Println(st.red("- " + o.Text))
		}
	}
	if changed {
		return exitError(1)
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
