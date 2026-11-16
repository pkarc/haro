//go:build js && wasm

// Módulo WebAssembly del núcleo de haro, para la web pública sin servidor: la
// configuración nunca sale del navegador. Expone una única función global,
// haroCore.call(método, jsonPeticion) → jsonRespuesta, y la web la envuelve
// en WasmCoreClient (web/src/core/wasm.ts).
//
// Las peticiones y respuestas son JSON con los mismos nombres de campo
// (camelCase) que el contrato proto/haro/v1/haro.proto, así que la web los
// valida y completa con el código generado. Una respuesta es
// {"result": …} o {"error": {"code", "message"}}.
//
// Compilar: make wasm
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"

	"github.com/pkarc/haro/core"
	"github.com/pkarc/haro/internal/coreapi"
)

type sourcesReq struct {
	Text  string        `json:"text"`
	Files []core.Source `json:"files"`
}

func (r sourcesReq) sources() []core.Source {
	if len(r.Files) > 0 {
		return r.Files
	}
	return []core.Source{{Path: "haproxy.cfg", Text: r.Text}}
}

// codedError es un error con el código de la API (invalid_argument…).
type codedError struct {
	code string
	err  error
}

func (e codedError) Error() string { return e.err.Error() }

func invalid(err error) error { return codedError{"invalid_argument", err} }

func parseLang(s string) (core.Lang, error) {
	if s == "" {
		return core.LangES, nil
	}
	l, ok := core.ParseLang(s)
	if !ok {
		return "", invalid(fmt.Errorf("idioma desconocido %q (usa es o en)", s))
	}
	return l, nil
}

var methods = map[string]func(raw []byte) (any, error){
	"analyze": func(raw []byte) (any, error) {
		var r struct {
			sourcesReq
			Lang           string `json:"lang"`
			HAProxyVersion string `json:"haproxyVersion"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		lang, err := parseLang(r.Lang)
		if err != nil {
			return nil, err
		}
		if r.HAProxyVersion != "" {
			if _, ok := core.ParseVersion(r.HAProxyVersion); !ok {
				return nil, invalid(fmt.Errorf("versión de HAProxy no válida: %q", r.HAProxyVersion))
			}
		}
		return core.AnalyzeConfigWith(r.sources(), core.LintOptions{Lang: lang, HAProxyVersion: r.HAProxyVersion}), nil
	},
	"apply": func(raw []byte) (any, error) {
		var r struct {
			sourcesReq
			Ops []core.Op `json:"ops"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		out, err := coreapi.Apply(r.sources(), r.Ops)
		var ae *core.ApplyError
		if errors.As(err, &ae) {
			return nil, invalid(ae)
		}
		return out, err
	},
	"diff": func(raw []byte) (any, error) {
		var r struct{ Before, After string }
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		return map[string]any{"ops": core.DiffLines(core.Normalize(r.Before), core.Normalize(r.After))}, nil
	},
	"knowledge": func(raw []byte) (any, error) {
		var r struct{ Lang string }
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		lang, err := parseLang(r.Lang)
		if err != nil {
			return nil, err
		}
		return core.KB.PublicFor(lang), nil
	},
	"listTemplates": func(raw []byte) (any, error) {
		var r struct{ Lang string }
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		lang, err := parseLang(r.Lang)
		if err != nil {
			return nil, err
		}
		return map[string]any{"templates": core.TechTemplates(lang)}, nil
	},
	"renderTemplate": func(raw []byte) (any, error) {
		var r struct {
			sourcesReq
			ID     string         `json:"id"`
			Params map[string]any `json:"params"`
			Lang   string         `json:"lang"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		lang, err := parseLang(r.Lang)
		if err != nil {
			return nil, err
		}
		out, err := coreapi.RenderTemplate(r.sources(), r.ID, r.Params, lang)
		var te *core.TemplateError
		if errors.As(err, &te) {
			return nil, invalid(te)
		}
		return out, err
	},
}

// call atiende una llamada y siempre devuelve JSON.
func call(method string, raw []byte) string {
	fn, ok := methods[method]
	if !ok {
		return errJSON("unimplemented", fmt.Errorf("método desconocido: %s", method))
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	res, err := fn(raw)
	if err != nil {
		var ce codedError
		if errors.As(err, &ce) {
			return errJSON(ce.code, ce.err)
		}
		return errJSON("internal", err)
	}
	b, err := json.Marshal(map[string]any{"result": res})
	if err != nil {
		return errJSON("internal", err)
	}
	return string(b)
}

func errJSON(code string, err error) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
	return string(b)
}

func main() {
	api := js.Global().Get("Object").New()
	api.Set("call", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) < 1 {
			return errJSON("invalid_argument", errors.New("falta el método"))
		}
		var raw []byte
		if len(args) > 1 {
			raw = []byte(args[1].String())
		}
		// Un pánico no debe dejar muerto el módulo: se devuelve como error.
		defer func() { recover() }()
		return call(args[0].String(), raw)
	}))
	js.Global().Set("haroCore", api)
	select {} // el módulo se queda vivo atendiendo llamadas
}
