package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/pkarc/haro/core"
)

// haro template: full templates by technology (Nextcloud, PostgreSQL…).
// The logic (parameters, validation, collision-free names, edit operations)
// lives in core; here we only read the configuration, show the diff and, with
// --write, save it with the same guarantees as haro serve.

const templateUsage = `Usage:
  haro template list [--lang en|es] [--format text|json]
  haro template show <id> [--lang en|es] [--format text|json]
  haro template add <id> [--param key=value]... [--lang en|es] [--write] [paths...]

list and show display the templates and their parameters. add shows the unified diff of
adding the template to the configuration and writes NOTHING; with --write it saves (validates
the full configuration with haproxy -c, atomic write and .bak copy).
List-type parameters accept several --param with the same key or comma-separated
values: --param servers=10.0.0.10:80,10.0.0.11:80
`

func runTemplate(args []string) error { return templateCmd(args, os.Stdout, os.Stderr) }

func templateCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, templateUsage)
		return exitError(2)
	}
	var err error
	switch sub, rest := args[0], args[1:]; sub {
	case "list":
		err = templateList(rest, stdout, stderr)
	case "show":
		err = templateShow(rest, stdout, stderr)
	case "add":
		err = templateAdd(rest, stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, templateUsage)
		return nil
	default:
		fmt.Fprintf(stderr, "haro template: unknown subcommand %q\n\n%s", sub, templateUsage)
		return exitError(2)
	}
	if errors.Is(err, errHelpShown) {
		return nil
	}
	return err
}

// parseArgs parses mixed options and positionals (package flag stops at the
// first positional) and returns the positionals in order.
func parseArgs(fs *flag.FlagSet, args []string) (pos []string, err error) {
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, errHelpShown
			}
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// errHelpShown means the -h help was already printed: finish without an error.
var errHelpShown = errors.New("help shown")

// parseID returns the first positional (the id) and the rest.
func parseID(fs *flag.FlagSet, args []string) (id string, rest []string, err error) {
	pos, err := parseArgs(fs, args)
	if err != nil {
		return "", nil, err
	}
	if len(pos) == 0 {
		return "", nil, errors.New("missing template id (haro template list shows them)")
	}
	return pos[0], pos[1:], nil
}

// newFlagSet creates a FlagSet that writes to stderr and does not end the process.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func templateList(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("template list", stderr)
	format := fs.String("format", "text", "output format: text | json")
	langArg := langFlag(fs)
	if pos, err := parseArgs(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return fmt.Errorf("unexpected argument %q", pos[0])
	}
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	list := core.TechTemplates(lang)
	if *format == "json" {
		return writeJSON(stdout, list)
	}
	for _, t := range list {
		desc := t.Description
		if i := strings.Index(desc, ". "); i > 0 {
			desc = desc[:i+1]
		}
		fmt.Fprintf(stdout, "%-14s %-4s %s\n", t.ID, t.Kind, t.Name)
		fmt.Fprintf(stdout, "%-14s      %s\n", "", desc)
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func findTemplate(id string, lang core.Lang) (core.TechTemplate, error) {
	var ids []string
	for _, t := range core.TechTemplates(lang) {
		if t.ID == id {
			return t, nil
		}
		ids = append(ids, t.ID)
	}
	return core.TechTemplate{}, fmt.Errorf("unknown template %q (available: %s)", id, strings.Join(ids, ", "))
}

func templateShow(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("template show", stderr)
	format := fs.String("format", "text", "output format: text | json")
	noColor := fs.Bool("no-color", false, "no colors")
	langArg := langFlag(fs)
	id, rest, err := parseID(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	t, err := findTemplate(id, lang)
	if err != nil {
		return err
	}
	if *format == "json" {
		return writeJSON(stdout, t)
	}
	st := newStyle(*noColor)
	fmt.Fprintf(stdout, "%s  %s\n%s\n\n"+tx(lang, "Parámetros", "Parameters")+":\n", st.bold(t.ID), st.dim("("+t.Kind+") "+t.Name), t.Description)
	var ex []string
	for _, p := range t.Params {
		req := tx(lang, "opcional", "optional")
		switch {
		case p.Required:
			req = tx(lang, "obligatorio", "required")
		case p.Default != nil:
			req = fmt.Sprintf(tx(lang, "por defecto %v", "default %v"), p.Default)
		}
		if p.DependsOn != "" {
			req += tx(lang, ", si ", ", if ") + p.DependsOn
		}
		fmt.Fprintf(stdout, "  %s  %s\n", st.cyan(fmt.Sprintf("%-15s", p.Key)), st.dim(fmt.Sprintf("%-6s %s", p.Type, req)))
		fmt.Fprintf(stdout, "      %s\n", p.Label)
		if p.Help != "" {
			fmt.Fprintf(stdout, "      %s\n", st.dim(p.Help))
		}
		if p.Required && p.DependsOn == "" && p.Example != "" {
			ex = append(ex, fmt.Sprintf("--param %s=%s", p.Key, p.Example))
		}
	}
	fmt.Fprintf(stdout, "\n"+tx(lang, "Ejemplo", "Example")+":\n  haro template add %s %s ["+tx(lang, "rutas", "paths")+"...]\n", t.ID, strings.Join(ex, " "))
	return nil
}

// paramFlag accumulates the --param key=value pairs. Repeating a key joins
// the values with commas (for lists).
type paramFlag map[string]string

func (p paramFlag) String() string { return fmt.Sprint(map[string]string(p)) }
func (p paramFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	k = strings.TrimSpace(k)
	if !ok || k == "" {
		return fmt.Errorf("use --param key=value (got %q)", v)
	}
	if old, dup := p[k]; dup {
		val = old + "," + val
	}
	p[k] = val
	return nil
}

func templateAdd(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("template add", stderr)
	params := paramFlag{}
	fs.Var(params, "param", "parameter key=value; repeat it for several (haro template show <id> lists them)")
	write := fs.Bool("write", false, "save the result (validates with haproxy -c, atomic write and .bak copy); by default it only shows the diff")
	bin := fs.String("haproxy", "haproxy", "haproxy binary used to validate with -c when writing")
	noColor := fs.Bool("no-color", false, "no colors")
	langArg := langFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: haro template add <id> [options] [file|directory]...")
		fs.PrintDefaults()
	}
	id, paths, err := parseID(fs, args)
	if err != nil {
		return err
	}
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	if _, err := findTemplate(id, lang); err != nil {
		return err
	}
	for _, p := range paths {
		if p == "-" {
			return errors.New("“-” (stdin) is not valid here: give files or directories")
		}
	}
	srcs, err := readSources(paths)
	if err != nil {
		return err
	}
	raw := make(map[string]any, len(params))
	for k, v := range params {
		raw[k] = v
	}
	cfg := core.NewConfig(srcs)
	ops, err := core.RenderTechTemplate(id, raw, cfg, lang)
	if err != nil {
		return err
	}
	out, changes, err := core.ApplyLang(cfg, lang, ops...)
	if err != nil {
		return err
	}

	st := newStyle(*noColor)
	for _, c := range changes {
		for _, line := range strings.Split(strings.TrimRight(core.UnifiedDiff(c.Path, srcs[c.File].Text, c.Text, 3), "\n"), "\n") {
			switch {
			case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
				fmt.Fprintln(stdout, st.bold(line))
			case strings.HasPrefix(line, "+"):
				fmt.Fprintln(stdout, st.green(line))
			case strings.HasPrefix(line, "-"):
				fmt.Fprintln(stdout, st.red(line))
			case strings.HasPrefix(line, "@@"):
				fmt.Fprintln(stdout, st.cyan(line))
			default:
				fmt.Fprintln(stdout, line)
			}
		}
	}

	// Problems that appear with the change and were not there before (e.g. a TLS server without verification).
	seen := map[string]int{}
	for _, i := range core.LintWith(cfg.Merge(), core.LintOptions{Lang: lang}) {
		seen[i.Rule+"\x00"+i.Message]++
	}
	for _, i := range core.LintWith(out.Merge(), core.LintOptions{Lang: lang}) {
		k := i.Rule + "\x00" + i.Message
		if seen[k] > 0 {
			seen[k]--
			continue
		}
		fmt.Fprintf(stderr, "%s:%d: %s %s %s\n", out.Files[i.File].Path, i.Line, st.sev(i.Severity, lang), i.Message, st.dim("["+i.Rule+"]"))
	}

	if !*write {
		fmt.Fprintln(stderr, st.dim(tx(lang, "No se ha modificado ningún archivo. Añade --write para guardar (valida con haproxy -c y deja copia .bak).", "No file was modified. Add --write to save (validates with haproxy -c and leaves a .bak copy).")))
		return nil
	}
	if len(changes) == 0 {
		fmt.Fprintln(stderr, tx(lang, "Nada que guardar: la plantilla no cambia ningún archivo.", "Nothing to save: the template does not change any file."))
		return nil
	}
	return writeChanges(stderr, paths, *bin, changes, lang)
}

// writeChanges saves the changes with the same guarantees as haro serve
// (server.put): validates the full configuration with haproxy -c (or with the
// detector if there is no binary), atomic write and .bak copy.
func writeChanges(stderr io.Writer, paths []string, bin string, changes []core.FileChange, lang core.Lang) error {
	s := &server{paths: paths, allowWrite: true}
	if p, err := exec.LookPath(bin); err == nil {
		s.haproxyBin = p
	}
	files := make([]core.Source, len(changes))
	for i, c := range changes {
		files[i] = core.Source{Path: c.Path, Text: c.Text}
	}
	res, err := s.put(context.Background(), files)
	if err != nil {
		return err
	}
	if res.Err != "" {
		msg := tx(lang, "no se guardó: ", "not saved: ") + res.Err
		if res.Check != nil && res.Check.Output != "" {
			msg += "\n" + strings.TrimSpace(res.Check.Output)
		}
		if res.Issue != nil {
			msg += fmt.Sprintf("\n%s:%d: %s", res.Path, res.Issue.Line, res.Issue.Message)
		}
		return errors.New(msg)
	}
	if s.haproxyBin == "" {
		fmt.Fprintln(stderr, tx(lang, "Aviso: no se encontró haproxy; se validó solo con el detector de haro. Valida con haproxy -c.", "Warning: haproxy not found; validated only with haro's detector. Validate with haproxy -c."))
	}
	var names []string
	for _, sv := range res.Saved {
		names = append(names, sv.Path)
	}
	sort.Strings(names)
	fmt.Fprintf(stderr, tx(lang, "Guardado en %s (copia .bak junto a cada archivo).\n", "Saved to %s (.bak copy next to each file).\n"), strings.Join(names, ", "))
	return nil
}
