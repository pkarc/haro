//go:build js && wasm

// WebAssembly module of the haro core, for the public serverless web UI: the
// configuration never leaves the browser. It exposes a single global function,
// haroCore.call(method, jsonRequest) → jsonResponse, and the web UI wraps it
// in WasmCoreClient (web/src/core/wasm.ts).
//
// Requests and responses are JSON with the same field names (camelCase) as the
// proto/haro/v1/haro.proto contract, so the web UI validates and completes
// them with the generated code. A response is {"result": …} or
// {"error": {"code", "message"}}.
//
// Build: make wasm
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

// codedError is an error with the API code (invalid_argument…).
type codedError struct {
	code string
	err  error
}

func (e codedError) Error() string { return e.err.Error() }

func invalid(err error) error { return codedError{"invalid_argument", err} }

func parseLang(s string) (core.Lang, error) {
	if s == "" {
		return core.LangDefault, nil
	}
	l, ok := core.ParseLang(s)
	if !ok {
		return "", invalid(fmt.Errorf("unknown language %q (use es or en)", s))
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
				return nil, invalid(fmt.Errorf("invalid HAProxy version: %q", r.HAProxyVersion))
			}
		}
		return core.AnalyzeConfigWith(r.sources(), core.LintOptions{Lang: lang, HAProxyVersion: r.HAProxyVersion}), nil
	},
	"apply": func(raw []byte) (any, error) {
		var r struct {
			sourcesReq
			Ops  []core.Op `json:"ops"`
			Lang string    `json:"lang"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, invalid(err)
		}
		lang, err := parseLang(r.Lang)
		if err != nil {
			return nil, err
		}
		out, err := coreapi.Apply(r.sources(), r.Ops, lang)
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

// call handles a call and always returns JSON.
func call(method string, raw []byte) string {
	fn, ok := methods[method]
	if !ok {
		return errJSON("unimplemented", fmt.Errorf("unknown method: %s", method))
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
			return errJSON("invalid_argument", errors.New("missing method"))
		}
		var raw []byte
		if len(args) > 1 {
			raw = []byte(args[1].String())
		}
		// A panic must not leave the module dead: it is returned as an error.
		defer func() { recover() }()
		return call(args[0].String(), raw)
	}))
	js.Global().Set("haroCore", api)
	select {} // the module stays alive serving calls
}
