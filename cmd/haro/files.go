package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkarc/haro/core"
)

// expandPaths reproduce cómo carga HAProxy los argumentos de -f: cada
// archivo en el orden dado y, para cada directorio, sus archivos `.cfg` no
// ocultos en orden lexicográfico (LC_COLLATE=C), sin entrar en subdirectorios.
func expandPaths(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		if a == "-" {
			out = append(out, a)
			continue
		}
		fi, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			out = append(out, a)
			continue
		}
		entries, err := os.ReadDir(a)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".cfg") {
				continue
			}
			p := filepath.Join(a, n)
			if st, err := os.Stat(p); err != nil || st.IsDir() { // sigue enlaces simbólicos
				continue
			}
			names = append(names, p)
		}
		sort.Strings(names) // orden de bytes, equivalente a LC_COLLATE=C
		if len(names) == 0 {
			return nil, fmt.Errorf("el directorio %s no contiene archivos .cfg", a)
		}
		out = append(out, names...)
	}
	return out, nil
}

// readSources lee los archivos indicados (o el predeterminado si no hay
// ninguno) siguiendo las reglas de carga de HAProxy.
func readSources(args []string) ([]core.Source, error) {
	if len(args) == 0 {
		args = []string{defaultConfigPath}
	}
	paths, err := expandPaths(args)
	if err != nil {
		return nil, err
	}
	stdinUsed := false
	srcs := make([]core.Source, 0, len(paths))
	for _, p := range paths {
		var b []byte
		name := p
		if p == "-" {
			if stdinUsed {
				return nil, fmt.Errorf("stdin («-») solo puede usarse una vez")
			}
			stdinUsed = true
			b, err = io.ReadAll(os.Stdin)
			name = "stdin"
		} else {
			b, err = os.ReadFile(p)
		}
		if err != nil {
			return nil, err
		}
		srcs = append(srcs, core.Source{Path: name, Text: string(b)})
	}
	return srcs, nil
}
