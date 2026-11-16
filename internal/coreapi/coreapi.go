// Package coreapi reúne la lógica de las operaciones del núcleo que no
// depende del transporte, para que la compartan el servidor Connect
// (cmd/haro/rpc.go) y el módulo WASM (wasm/). Solo usa core y la biblioteca
// estándar, así que compila a WebAssembly.
package coreapi

import "github.com/pkarc/haro/core"

// Applied es el resultado de aplicar operaciones sin escribir en disco: todos
// los archivos (los que no cambian, con su texto original byte a byte) y solo
// los que cambiaron.
type Applied struct {
	Files   []core.Source     `json:"files"`
	Changes []core.FileChange `json:"changes"`
}

// Apply aplica ops sobre srcs. El error es un *core.ApplyError si una
// operación no es válida.
func Apply(srcs []core.Source, ops []core.Op) (Applied, error) {
	_, changes, err := core.Apply(core.NewConfig(srcs), ops...)
	if err != nil {
		return Applied{}, err
	}
	return finish(srcs, changes), nil
}

// Rendered es el resultado de renderizar una plantilla de tecnología.
type Rendered struct {
	Ops []core.Op `json:"ops"`
	Applied
}

// RenderTemplate renderiza la plantilla id con params y devuelve las
// operaciones y el resultado de aplicarlas. El error es un *core.TemplateError
// si los parámetros no son válidos.
func RenderTemplate(srcs []core.Source, id string, params map[string]any, lang core.Lang) (Rendered, error) {
	cfg := core.NewConfig(srcs)
	ops, err := core.RenderTechTemplate(id, params, cfg, lang)
	if err != nil {
		return Rendered{}, err
	}
	_, changes, err := core.Apply(cfg, ops...)
	if err != nil {
		return Rendered{}, err // las operaciones de una plantilla siempre deben aplicarse
	}
	return Rendered{Ops: ops, Applied: finish(srcs, changes)}, nil
}

func finish(srcs []core.Source, changes []core.FileChange) Applied {
	files := append([]core.Source{}, srcs...)
	out := Applied{Changes: []core.FileChange{}}
	for _, ch := range changes {
		files[ch.File].Text = ch.Text
		out.Changes = append(out.Changes, ch)
	}
	out.Files = files
	return out
}
