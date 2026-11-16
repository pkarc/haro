// Package coreapi gathers the logic of the core operations that does not
// depend on the transport, so the Connect server
// (cmd/haro/rpc.go) and the WASM module (wasm/) share it. It uses only core and the
// standard library, so it compiles to WebAssembly.
package coreapi

import "github.com/pkarc/haro/core"

// Applied is the result of applying operations without writing to disk: all
// the files (those that do not change, with their original text byte for byte) and only
// those that changed.
type Applied struct {
	Files   []core.Source     `json:"files"`
	Changes []core.FileChange `json:"changes"`
}

// Apply applies ops to srcs, with error messages in lang. The error is a
// *core.ApplyError if an operation is not valid.
func Apply(srcs []core.Source, ops []core.Op, lang core.Lang) (Applied, error) {
	_, changes, err := core.ApplyLang(core.NewConfig(srcs), lang, ops...)
	if err != nil {
		return Applied{}, err
	}
	return finish(srcs, changes), nil
}

// Rendered is the result of rendering a technology template.
type Rendered struct {
	Ops []core.Op `json:"ops"`
	Applied
}

// RenderTemplate renders template id with params and returns the
// operations and the result of applying them. The error is a *core.TemplateError
// if the parameters are not valid.
func RenderTemplate(srcs []core.Source, id string, params map[string]any, lang core.Lang) (Rendered, error) {
	cfg := core.NewConfig(srcs)
	ops, err := core.RenderTechTemplate(id, params, cfg, lang)
	if err != nil {
		return Rendered{}, err
	}
	_, changes, err := core.ApplyLang(cfg, lang, ops...)
	if err != nil {
		return Rendered{}, err // a template's operations must always apply
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
