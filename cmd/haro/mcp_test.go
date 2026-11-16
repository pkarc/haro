package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkarc/haro/core"
)

// mcpCall ejecuta una llamada a herramienta y devuelve (texto, estructurado, isError).
func mcpCall(t *testing.T, tools *mcpTools, name string, args any) (string, map[string]any, bool) {
	t.Helper()
	a, _ := json.Marshal(args)
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + string(a) + `}}`
	var out bytes.Buffer
	if err := tools.server().Serve(context.Background(), strings.NewReader(req+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			Content []struct{ Text string }
			Struct  map[string]any `json:"structuredContent"`
			IsError bool           `json:"isError"`
		}
		Error *struct{ Message string }
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta: %v\n%s", err, out.String())
	}
	if resp.Error != nil {
		t.Fatalf("error de protocolo: %s", resp.Error.Message)
	}
	return resp.Result.Content[0].Text, resp.Result.Struct, resp.Result.IsError
}

func writeCfg(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}
	return p
}

const mcpCfg = "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend web\n    bind *:80\n    default_backend api\n\nbackend api\n    server a 10.0.0.1:80 check\n"

func TestMCPLintCitesFileAndLine(t *testing.T) {
	dir := t.TempDir()
	p := writeCfg(t, dir, "haproxy.cfg", strings.Replace(mcpCfg, "default_backend api", "default_backend nope", 1))
	text, st, isErr := mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"paths": []string{p}})
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, p+":9: error El backend «nope» no existe") {
		t.Errorf("el texto debe citar archivo y línea:\n%s", text)
	}
	issues := st["issues"].([]any)
	found := false
	for _, i := range issues {
		m := i.(map[string]any)
		if m["rule"] == "backend-missing" && m["path"] == p && m["line"].(float64) == 9 {
			found = true
		}
	}
	if !found {
		t.Errorf("falta backend-missing en structuredContent: %v", issues)
	}
	// disable ignora reglas.
	_, st, _ = mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"paths": []string{p}, "disable": []string{"backend-missing"}})
	for _, i := range st["issues"].([]any) {
		if i.(map[string]any)["rule"] == "backend-missing" {
			t.Error("disable no ignoró la regla")
		}
	}
}

func TestMCPExplainSectionAndText(t *testing.T) {
	text, st, isErr := mcpCall(t, &mcpTools{}, "haro_explain", map[string]any{"text": mcpCfg, "section": "api"})
	if isErr || !strings.Contains(text, "backend api") || !strings.Contains(text, "server a 10.0.0.1:80") {
		t.Errorf("explain: %v\n%s", isErr, text)
	}
	if n := len(st["sections"].([]any)); n != 1 {
		t.Errorf("solo debía explicar la sección api: %d", n)
	}
	if text, _, isErr := mcpCall(t, &mcpTools{}, "haro_explain", map[string]any{"text": mcpCfg, "section": "zzz"}); !isErr {
		t.Errorf("una sección inexistente debe ser error de herramienta: %s", text)
	}
}

func TestMCPArgumentValidation(t *testing.T) {
	for name, args := range map[string]any{
		"ambos":       map[string]any{"paths": []string{"x"}, "text": "global"},
		"desconocido": map[string]any{"otra": 1},
		"stdin":       map[string]any{"paths": []string{"-"}},
	} {
		if text, _, isErr := mcpCall(t, &mcpTools{}, "haro_lint", args); !isErr {
			t.Errorf("%s: debería ser error de herramienta: %s", name, text)
		}
	}
}

func TestMCPRootsConfineReads(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	in := writeCfg(t, root, "ok.cfg", mcpCfg)
	out := writeCfg(t, outside, "secret.cfg", mcpCfg)
	os.Symlink(out, filepath.Join(root, "link.cfg"))
	os.Symlink(outside, filepath.Join(root, "linkdir"))
	rootAbs, _ := filepath.EvalSymlinks(root)
	tools := &mcpTools{roots: []string{rootAbs}}

	if _, _, isErr := mcpCall(t, tools, "haro_lint", map[string]any{"paths": []string{in}}); isErr {
		t.Error("una ruta dentro de la raíz debe leerse")
	}
	for name, p := range map[string]string{
		"fuera":         out,
		"punto-punto":   filepath.Join(root, "..", filepath.Base(outside), "secret.cfg"),
		"enlace":        filepath.Join(root, "link.cfg"),
		"dir-enlazado":  filepath.Join(root, "linkdir"),
		"prefijo-falso": root + "-otro",
	} {
		if text, _, isErr := mcpCall(t, tools, "haro_lint", map[string]any{"paths": []string{p}}); !isErr {
			t.Errorf("%s: debía rechazarse: %s", name, text)
		}
	}
}

func TestMCPApplyDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	p := writeCfg(t, dir, "haproxy.cfg", mcpCfg)
	text, st, isErr := mcpCall(t, &mcpTools{allowWrite: true}, "haro_apply", map[string]any{
		"paths": []string{p},
		"ops":   []map[string]any{{"op": "add_directive", "section": 2, "text": "balance leastconn", "at": 0}},
	})
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "+    balance leastconn") || !strings.Contains(text, "@@") {
		t.Errorf("debe devolver el diff:\n%s", text)
	}
	if st["written"] != false {
		t.Error("sin write no debe escribir")
	}
	if b, _ := os.ReadFile(p); string(b) != mcpCfg {
		t.Error("el archivo cambió en una simulación")
	}
}

func TestMCPApplyWriteGuards(t *testing.T) {
	dir := t.TempDir()
	p := writeCfg(t, dir, "haproxy.cfg", mcpCfg)
	ops := []map[string]any{{"op": "add_directive", "section": 2, "text": "balance leastconn"}}

	if text, _, isErr := mcpCall(t, &mcpTools{}, "haro_apply", map[string]any{"paths": []string{p}, "ops": ops, "write": true}); !isErr || !strings.Contains(text, "--allow-write") {
		t.Errorf("sin --allow-write debe rechazar: %s", text)
	}
	if text, _, isErr := mcpCall(t, &mcpTools{allowWrite: true}, "haro_apply", map[string]any{"text": mcpCfg, "ops": ops, "write": true}); !isErr {
		t.Errorf("write con text debe rechazarse: %s", text)
	}
	// Con --allow-write y raíz: escribe, con copia .bak.
	rootAbs, _ := filepath.EvalSymlinks(dir)
	tools := &mcpTools{allowWrite: true, roots: []string{rootAbs}}
	text, st, isErr := mcpCall(t, tools, "haro_apply", map[string]any{"paths": []string{p}, "ops": ops, "write": true})
	if isErr || st["written"] != true {
		t.Fatalf("debía escribir: %v\n%s", isErr, text)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "    balance leastconn\n") {
		t.Errorf("no se escribió el cambio:\n%s", b)
	}
	if b, _ := os.ReadFile(p + ".bak"); string(b) != mcpCfg {
		t.Error("falta la copia .bak con el original")
	}
	// Una edición que rompe la configuración no se guarda (el detector es la barrera sin haproxy).
	before, _ := os.ReadFile(p)
	text, _, isErr = mcpCall(t, tools, "haro_apply", map[string]any{"paths": []string{p}, "write": true,
		"ops": []map[string]any{{"op": "update_directive", "section": 1, "lineIndex": 1, "text": "default_backend nope"}}})
	if !isErr || !strings.Contains(text, "no se guardó") {
		t.Errorf("una edición inválida no debe guardarse: %s", text)
	}
	if after, _ := os.ReadFile(p); string(after) != string(before) {
		t.Error("el archivo cambió pese al rechazo")
	}
}

func TestMCPApplyRenameAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, "10-web.cfg", "frontend f\n    bind *:80\n    default_backend api\n")
	writeCfg(t, dir, "20-api.cfg", "backend api\n    server a 10.0.0.1:80\n")
	rootAbs, _ := filepath.EvalSymlinks(dir)
	tools := &mcpTools{allowWrite: true, roots: []string{rootAbs}}
	text, _, isErr := mcpCall(t, tools, "haro_apply", map[string]any{"paths": []string{dir}, "write": true,
		"ops": []map[string]any{{"op": "rename_section", "section": 1, "name": "api2", "updateRefs": true}}})
	if isErr {
		t.Fatal(text)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "10-web.cfg"))
	b, _ := os.ReadFile(filepath.Join(dir, "20-api.cfg"))
	if !strings.Contains(string(a), "default_backend api2") || !strings.Contains(string(b), "backend api2") {
		t.Errorf("el renombrado debe guardarse en los dos archivos a la vez:\n%s\n%s", a, b)
	}
}

func TestMCPDiffAndCheckAndResources(t *testing.T) {
	text, st, _ := mcpCall(t, &mcpTools{}, "haro_diff", map[string]any{"before": "a\nb\n", "after": "a\nc\n"})
	if st["changed"] != true || !strings.Contains(text, "-b") || !strings.Contains(text, "+c") {
		t.Errorf("diff: %s", text)
	}
	text, _, _ = mcpCall(t, &mcpTools{}, "haro_diff", map[string]any{"before": "a", "after": "a\n"})
	if text != "Sin diferencias." {
		t.Errorf("sin diferencias: %q", text)
	}
	text, st, isErr := mcpCall(t, &mcpTools{}, "haro_check", map[string]any{"text": mcpCfg})
	if isErr || st["available"] != false || !strings.Contains(text, "no está instalado") {
		t.Errorf("check sin haproxy: %s", text)
	}

	var out bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"haro://rules"}}`
	(&mcpTools{}).server().Serve(context.Background(), strings.NewReader(req+"\n"), &out)
	if !strings.Contains(out.String(), "backend-missing") {
		t.Errorf("haro://rules debe listar las reglas: %s", out.String())
	}
}

func TestMCPToolListHasAllTools(t *testing.T) {
	got := strings.Join((&mcpTools{}).server().SortedNames(), ",")
	if got != "haro_analyze,haro_apply,haro_check,haro_diff,haro_explain,haro_lint,haro_template_list,haro_template_render" {
		t.Errorf("herramientas: %s", got)
	}
}

func TestMCPLang(t *testing.T) {
	cfg := strings.Replace(mcpCfg, "default_backend api", "default_backend nope", 1)
	// Sin lang: español. Con lang=en: inglés, también en explain y analyze.
	text, _, _ := mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"text": cfg})
	if !strings.Contains(text, "no existe") {
		t.Errorf("por defecto debe ser español: %s", text)
	}
	text, st, isErr := mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"text": cfg, "lang": "en"})
	if isErr || !strings.Contains(text, "The backend “nope” does not exist.") {
		t.Errorf("lint en inglés: %v %s", isErr, text)
	}
	if st["issues"].([]any)[0].(map[string]any)["message"] == "" {
		t.Error("falta el mensaje estructurado")
	}
	text, _, _ = mcpCall(t, &mcpTools{}, "haro_explain", map[string]any{"text": cfg, "lang": "en", "section": "web"})
	if !strings.Contains(text, "Default backend") || !strings.Contains(text, "Traffic entry point") {
		t.Errorf("explain en inglés: %s", text)
	}
	// --lang fija el valor por defecto y el argumento lo sobrescribe.
	text, _, _ = mcpCall(t, &mcpTools{lang: core.LangEN}, "haro_lint", map[string]any{"text": cfg})
	if !strings.Contains(text, "does not exist") {
		t.Errorf("--lang en: %s", text)
	}
	text, _, _ = mcpCall(t, &mcpTools{lang: core.LangEN}, "haro_lint", map[string]any{"text": cfg, "lang": "es"})
	if !strings.Contains(text, "no existe") {
		t.Errorf("lang=es debe ganar a --lang: %s", text)
	}
	if text, _, isErr := mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"text": cfg, "lang": "fr"}); !isErr {
		t.Errorf("un idioma desconocido debe ser error: %s", text)
	}
	// El esquema de las herramientas de lectura declara el enum.
	for _, tool := range (&mcpTools{}).tools() {
		switch tool.Name {
		case "haro_lint", "haro_explain", "haro_analyze", "haro_apply":
			props := tool.InputSchema["properties"].(map[string]any)
			lp, ok := props["lang"].(map[string]any)
			if !ok || len(lp["enum"].([]string)) != 2 {
				t.Errorf("%s: falta lang (enum es/en) en el esquema", tool.Name)
			}
		}
	}
	// El recurso de reglas sigue el idioma por defecto.
	var out bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"haro://rules"}}`
	(&mcpTools{lang: core.LangEN}).server().Serve(context.Background(), strings.NewReader(req+"\n"), &out)
	if !strings.Contains(out.String(), "Reference to a backend that does not exist") {
		t.Errorf("haro://rules en inglés: %s", out.String())
	}
}

func TestMCPHAProxyVersion(t *testing.T) {
	cfg := "global\n    thread-groups 2\n"
	text, _, isErr := mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"text": cfg, "haproxyVersion": "2.4", "lang": "en"})
	if isErr || !strings.Contains(text, "newer-than-target") || !strings.Contains(text, "thread-groups") {
		t.Errorf("con versión 2.4 debe avisar: %s", text)
	}
	text, _, _ = mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"text": cfg, "haproxyVersion": "3.0"})
	if strings.Contains(text, "newer-than-target") {
		t.Errorf("con 3.0 no debe avisar: %s", text)
	}
	if _, _, isErr := mcpCall(t, &mcpTools{}, "haro_lint", map[string]any{"text": cfg, "haproxyVersion": "nope"}); !isErr {
		t.Error("una versión no válida debe ser error")
	}
	for _, tool := range (&mcpTools{}).tools() {
		switch tool.Name {
		case "haro_lint", "haro_explain", "haro_analyze", "haro_apply":
			if _, ok := tool.InputSchema["properties"].(map[string]any)["haproxyVersion"]; !ok {
				t.Errorf("%s: falta haproxyVersion en el esquema", tool.Name)
			}
		}
	}
}
