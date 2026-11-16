package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/pkarc/haro/core"
	"github.com/pkarc/haro/internal/tui"
)

// isTerminal indica si f es un terminal interactivo (no un pipe ni un archivo).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "sin colores (también con NO_COLOR)")
	noWatch := fs.Bool("no-watch", false, "no recargar al cambiar los archivos")
	langArg := langFlag(fs)
	versionArg := versionFlag(fs)
	editor := fs.String("editor", "", "editor para la tecla e (por defecto $VISUAL o $EDITOR)")
	assistant := fs.Bool("assistant", false, "activa el asistente con IA (tecla a): ENVÍA la configuración a la API de Anthropic (clave en ANTHROPIC_API_KEY o `ant auth login`)")
	assistModel := fs.String("assistant-model", envOr("HARO_ASSISTANT_MODEL", defaultAssistantModel), "modelo de Anthropic del asistente")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Uso: haro tui [opciones] [archivo|directorio]...")
		fmt.Fprintln(fs.Output(), "Interfaz de terminal para navegar la configuración. Sin rutas, abre /etc/haproxy/haproxy.cfg.")
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
	paths := fs.Args()
	for _, p := range paths {
		if p == "-" {
			return errors.New("la interfaz de terminal no puede leer de stdin; usa haro lint o haro explain")
		}
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return errors.New("la interfaz de terminal necesita un terminal interactivo; usa haro lint o haro explain en pipes")
	}
	load := func() ([]core.Source, error) { return readSources(paths) }
	if _, err := load(); err != nil { // falla antes de abrir la pantalla si no hay nada que leer
		return fmt.Errorf("no se pudo leer la configuración: %w\nIndica la ruta: haro tui archivo.cfg", err)
	}
	opts := tui.Options{Load: load, Dirs: paths, Editor: *editor, Lang: lang, HAProxyVersion: *versionArg, NoColor: *noColor, NoWatch: *noWatch}
	if *assistant {
		fmt.Fprintln(os.Stderr, "haro: asistente activado: la configuración se envía a la API de Anthropic (modelo "+*assistModel+")")
		opts.Assist, opts.Save = tuiAssist(newAnthropicStreamer(*assistModel), paths, load), tuiSave(paths)
		opts.AssistModel = *assistModel
	}
	return tui.Run(opts)
}

// tuiAssist conecta el asistente de la TUI con runAssist. Lee la configuración
// al enviar cada petición, así el modelo siempre ve la versión vigente.
func tuiAssist(st streamer, paths []string, load func() ([]core.Source, error)) func(context.Context, []core.AssistMessage, core.Lang, func(string)) (tui.AssistOutcome, error) {
	return func(ctx context.Context, msgs []core.AssistMessage, lang core.Lang, onText func(string)) (tui.AssistOutcome, error) {
		srcs, err := load()
		if err != nil {
			return tui.AssistOutcome{}, err
		}
		out, err := runAssist(ctx, st, srcs, msgs, lang, func(t string) error { onText(t); return ctx.Err() })
		return tui.AssistOutcome{
			Text: out.Text, Proposal: out.Proposal, IssuesAfter: out.IssuesAfter,
			StopReason: out.Result.StopReason, RefusalCategory: out.Result.RefusalCategory,
		}, err
	}
}

// tuiSave guarda una propuesta como `haro serve --allow-write`: valida, escritura atómica y .bak.
func tuiSave(paths []string) func([]core.FileChange) error {
	return func(changes []core.FileChange) error {
		_, err := saveProposal(context.Background(), paths, changes)
		return err
	}
}
