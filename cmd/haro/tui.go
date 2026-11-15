package main

import (
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
	editor := fs.String("editor", "", "editor para la tecla e (por defecto $VISUAL o $EDITOR)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Uso: haro tui [opciones] [archivo|directorio]...")
		fmt.Fprintln(fs.Output(), "Interfaz de terminal para navegar la configuración. Sin rutas, abre /etc/haproxy/haproxy.cfg.")
		fs.PrintDefaults()
	}
	fs.Parse(args)
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
	return tui.Run(tui.Options{Load: load, Dirs: paths, Editor: *editor, NoColor: *noColor, NoWatch: *noWatch})
}
