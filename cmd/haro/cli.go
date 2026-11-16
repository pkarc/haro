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

// readConfig reads a single file (or stdin with "-"). Used by diff.
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

// ---------- terminal styles ----------

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

// rich turns the minimal markdown of the explanations into terminal styles.
func (s style) rich(t string) string {
	t = reBold.ReplaceAllStringFunc(t, func(m string) string { return s.bold(m[2 : len(m)-2]) })
	return reCode.ReplaceAllStringFunc(t, func(m string) string { return s.cyan(m[1 : len(m)-1]) })
}

func (s style) sev(sev core.Severity, lang core.Lang) string {
	switch sev {
	case core.SevError:
		return s.red("error")
	case core.SevWarn:
		return s.yellow(tx(lang, "aviso", "warning"))
	default:
		return s.blue(tx(lang, "sugerencia", "suggestion"))
	}
}

// tx picks the user-facing text for the language (English by default).
func tx(lang core.Lang, es, en string) string {
	if lang == core.LangES {
		return es
	}
	return en
}

// summary is the "N errors, N warnings, N suggestions" line.
func summary(lang core.Lang, counts map[core.Severity]int) string {
	return plural(counts[core.SevError], "error", tx(lang, "errores", "errors")) + ", " +
		plural(counts[core.SevWarn], tx(lang, "aviso", "warning"), tx(lang, "avisos", "warnings")) + ", " +
		plural(counts[core.SevInfo], tx(lang, "sugerencia", "suggestion"), tx(lang, "sugerencias", "suggestions"))
}

// ---------- language ----------

// langFlag registers --lang on fs. The default comes from the environment
// (LC_ALL, LC_MESSAGES, LANG); resolveLang turns it into a core.Lang.
func langFlag(fs *flag.FlagSet) *string {
	return fs.String("lang", "", "language of the explanations and detector messages: en | es (default: from LC_ALL/LANG, otherwise en)")
}

func resolveLang(v string) (core.Lang, error) {
	if v == "" {
		return core.LangFromEnv(), nil
	}
	if l, ok := core.ParseLang(v); ok {
		return l, nil
	}
	return "", fmt.Errorf("unknown language %q (use es or en)", v)
}

// ---------- lint ----------

func runLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text | json | sarif | markdown")
	disable := fs.String("disable", "", "rules to ignore, comma-separated (e.g. no-global,tls-no-alpn)")
	strict := fs.Bool("strict", false, "exit with code 1 also on warnings")
	noColor := fs.Bool("no-color", false, "no colors")
	listRules := fs.Bool("rules", false, "list the available rules and exit")
	langArg := langFlag(fs)
	versionArg := versionFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: haro lint [options] [file|directory|-]...")
		fmt.Fprintln(fs.Output(), "Several files or directories are analyzed together, in haproxy -f load order.")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	if err := checkVersion(*versionArg); err != nil {
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
		return fmt.Errorf("unknown format %q (use text, json, sarif or markdown)", f)
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
	for _, i := range core.LintWith(core.NewConfig(srcs).Merge(), core.LintOptions{Lang: lang, HAProxyVersion: *versionArg}) {
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
		writeMarkdown(os.Stdout, srcs, issues, lang)
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
			fmt.Printf("%s:%d: %s %s %s\n", srcs[i.File].Path, i.Line, st.sev(i.Severity, lang), i.Message, st.dim("["+i.Rule+"]"))
			if l := lines[i.File]; i.Line-1 < len(l) {
				fmt.Printf("    %s\n", st.dim(strings.TrimSpace(l[i.Line-1])))
			}
		}
		if len(issues) == 0 {
			var fargs []string
			for _, s := range srcs {
				fargs = append(fargs, "-f "+s.Path)
			}
			fmt.Println(st.green(tx(lang, "✓ Sin problemas detectados.", "✓ No problems found.")) + st.dim(tx(lang, " Valida siempre con: haproxy -c ", " Always validate with: haproxy -c ")+strings.Join(fargs, " ")))
		} else {
			fmt.Printf("\n%s\n", summary(lang, counts))
		}
	}
	if counts[core.SevError] > 0 || (*strict && counts[core.SevWarn] > 0) {
		return exitError(1)
	}
	return nil
}

// versionFlag registra --haproxy-version en fs.
func versionFlag(fs *flag.FlagSet) *string {
	return fs.String("haproxy-version", "", "target HAProxy version (e.g. 2.8 or 3.0): warns about anything that needs a later version")
}

// checkVersion validates the --haproxy-version value (empty = no target version).
func checkVersion(v string) error {
	if v == "" {
		return nil
	}
	if _, ok := core.ParseVersion(v); !ok {
		return fmt.Errorf("unknown HAProxy version %q (use, for example, 2.8 or 3.0)", v)
	}
	return nil
}

// ---------- explain ----------

func runExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "no colors")
	section := fs.String("section", "", "explain only the section with this name or type")
	format := fs.String("format", "text", "output format: text | json")
	langArg := langFlag(fs)
	versionArg := versionFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: haro explain [options] [file|directory|-]...")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	if err := checkVersion(*versionArg); err != nil {
		return err
	}
	srcs, err := readSources(fs.Args())
	if err != nil {
		return err
	}
	a := core.AnalyzeConfigWith(srcs, core.LintOptions{Lang: lang, HAProxyVersion: *versionArg})
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
			from := map[string]string{"own": "", "defaults": tx(lang, " (heredado de defaults)", " (inherited from defaults)"), "implicit": tx(lang, " (implícito)", " (implicit)")}[e.ModeFrom]
			fmt.Printf("  %s\n", st.dim(tx(lang, "modo ", "mode ")+e.Mode+from))
		}
		for _, i := range byLine[key{s.File, s.Line}] {
			fmt.Printf("  %s %s\n", st.sev(i.Severity, lang), i.Message)
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
				fmt.Printf("       %s %s\n", st.sev(i.Severity, lang), i.Message)
			}
		}
		fmt.Println()
	}
	return nil
}

// ---------- diff ----------

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "no colors")
	fs.Usage = func() { fmt.Fprintln(fs.Output(), "Usage: haro diff <before> <after>") }
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
