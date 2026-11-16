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

// haro template: plantillas completas por tecnología (Nextcloud, PostgreSQL…).
// La lógica (parámetros, validación, nombres sin colisiones, operaciones de
// edición) vive en core; aquí solo se lee la configuración, se muestra el diff
// y, con --write, se guarda con las mismas garantías que haro serve.

const templateUsage = `Uso:
  haro template list [--lang es|en] [--format text|json]
  haro template show <id> [--lang es|en] [--format text|json]
  haro template add <id> [--param clave=valor]... [--lang es|en] [--write] [rutas...]

list y show enseñan las plantillas y sus parámetros. add muestra el diff unificado de
añadir la plantilla a la configuración y NO escribe nada; con --write guarda (valida
la configuración completa con haproxy -c, escritura atómica y copia .bak).
Los parámetros de tipo lista aceptan varios --param con la misma clave o valores
separados por comas: --param servers=10.0.0.10:80,10.0.0.11:80
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
		fmt.Fprintf(stderr, "haro template: subcomando desconocido %q\n\n%s", sub, templateUsage)
		return exitError(2)
	}
	if errors.Is(err, errHelpShown) {
		return nil
	}
	return err
}

// parseArgs analiza opciones y posicionales mezclados (el paquete flag se
// detiene en el primer posicional) y los devuelve en orden.
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

// errHelpShown indica que ya se imprimió la ayuda de -h: terminar sin error.
var errHelpShown = errors.New("ayuda mostrada")

// parseID devuelve el primer posicional (el id) y el resto.
func parseID(fs *flag.FlagSet, args []string) (id string, rest []string, err error) {
	pos, err := parseArgs(fs, args)
	if err != nil {
		return "", nil, err
	}
	if len(pos) == 0 {
		return "", nil, errors.New("falta el id de la plantilla (haro template list las enseña)")
	}
	return pos[0], pos[1:], nil
}

// newFlagSet crea un FlagSet que escribe en stderr y no termina el proceso.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func templateList(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("template list", stderr)
	format := fs.String("format", "text", "formato de salida: text | json")
	langArg := langFlag(fs)
	if pos, err := parseArgs(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return fmt.Errorf("sobra el argumento %q", pos[0])
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
	return core.TechTemplate{}, fmt.Errorf("plantilla desconocida %q (disponibles: %s)", id, strings.Join(ids, ", "))
}

func templateShow(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("template show", stderr)
	format := fs.String("format", "text", "formato de salida: text | json")
	noColor := fs.Bool("no-color", false, "sin colores")
	langArg := langFlag(fs)
	id, rest, err := parseID(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("sobra el argumento %q", rest[0])
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
	fmt.Fprintf(stdout, "%s  %s\n%s\n\nParámetros:\n", st.bold(t.ID), st.dim("("+t.Kind+") "+t.Name), t.Description)
	var ex []string
	for _, p := range t.Params {
		req := "opcional"
		switch {
		case p.Required:
			req = "obligatorio"
		case p.Default != nil:
			req = fmt.Sprintf("por defecto %v", p.Default)
		}
		if p.DependsOn != "" {
			req += ", si " + p.DependsOn
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
	fmt.Fprintf(stdout, "\nEjemplo:\n  haro template add %s %s [rutas...]\n", t.ID, strings.Join(ex, " "))
	return nil
}

// paramFlag acumula los --param clave=valor. Repetir una clave junta los
// valores con comas (para las listas).
type paramFlag map[string]string

func (p paramFlag) String() string { return fmt.Sprint(map[string]string(p)) }
func (p paramFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	k = strings.TrimSpace(k)
	if !ok || k == "" {
		return fmt.Errorf("usa --param clave=valor (recibí %q)", v)
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
	fs.Var(params, "param", "parámetro clave=valor; repítelo para varios (haro template show <id> los lista)")
	write := fs.Bool("write", false, "guarda el resultado (valida con haproxy -c, escritura atómica y copia .bak); por defecto solo muestra el diff")
	bin := fs.String("haproxy", "haproxy", "binario de haproxy para validar con -c al escribir")
	noColor := fs.Bool("no-color", false, "sin colores")
	langArg := langFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Uso: haro template add <id> [opciones] [archivo|directorio]...")
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
			return errors.New("«-» (stdin) no es válido aquí: indica archivos o directorios")
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
	out, changes, err := core.Apply(cfg, ops...)
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

	// Problemas que aparecen con el cambio y no estaban antes (p. ej. un servidor TLS sin verificar).
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
		fmt.Fprintf(stderr, "%s:%d: %s %s %s\n", out.Files[i.File].Path, i.Line, st.sev(i.Severity), i.Message, st.dim("["+i.Rule+"]"))
	}

	if !*write {
		fmt.Fprintln(stderr, st.dim("No se ha modificado ningún archivo. Añade --write para guardar (valida con haproxy -c y deja copia .bak)."))
		return nil
	}
	if len(changes) == 0 {
		fmt.Fprintln(stderr, "Nada que guardar: la plantilla no cambia ningún archivo.")
		return nil
	}
	return writeChanges(stderr, paths, *bin, changes)
}

// writeChanges guarda los cambios con las mismas garantías que haro serve
// (server.put): valida la configuración completa con haproxy -c (o con el
// detector si no hay binario), escritura atómica y copia .bak.
func writeChanges(stderr io.Writer, paths []string, bin string, changes []core.FileChange) error {
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
		msg := "no se guardó: " + res.Err
		if res.Check != nil && res.Check.Output != "" {
			msg += "\n" + strings.TrimSpace(res.Check.Output)
		}
		if res.Issue != nil {
			msg += fmt.Sprintf("\n%s:%d: %s", res.Path, res.Issue.Line, res.Issue.Message)
		}
		return errors.New(msg)
	}
	if s.haproxyBin == "" {
		fmt.Fprintln(stderr, "Aviso: no se encontró haproxy; se validó solo con el detector de haro. Valida con haproxy -c.")
	}
	var names []string
	for _, sv := range res.Saved {
		names = append(names, sv.Path)
	}
	sort.Strings(names)
	fmt.Fprintf(stderr, "Guardado en %s (copia .bak junto a cada archivo).\n", strings.Join(names, ", "))
	return nil
}
