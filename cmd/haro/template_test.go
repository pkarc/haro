package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/pkarc/haro/core"
	harov1 "github.com/pkarc/haro/gen/haro/v1"
)

const tplBase = "global\n    log /dev/log local0\n\ndefaults\n    mode http\n    log global\n    option httplog\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend base_in\n    bind 127.0.0.1:8080\n    default_backend base\n\nbackend base\n    server s1 127.0.0.1:9000 check\n"

var ncArgs = []string{"--param", "domain=cloud.example.com", "--param", "servers=10.0.0.10:80", "--param", "cert=/etc/haproxy/certs/cloud.pem"}

func runTpl(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := templateCmd(args, &out, &errb)
	return out.String(), errb.String(), err
}

func TestCLITemplateListAndShow(t *testing.T) {
	out, _, err := runTpl(t, "list", "--lang", "es")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"proxmox", "homeassistant", "nextcloud", "jellyfin", "gitea", "postgresql", "redis", "elasticsearch", "kubernetes"} {
		if !strings.Contains(out, id) {
			t.Errorf("list no menciona %s:\n%s", id, out)
		}
	}
	out, _, err = runTpl(t, "show", "nextcloud", "--lang", "es", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"domain", "obligatorio", "servers", "hsts", "por defecto true", "haro template add nextcloud --param domain="} {
		if !strings.Contains(out, want) {
			t.Errorf("show nextcloud no contiene %q:\n%s", want, out)
		}
	}
	out, _, _ = runTpl(t, "show", "nextcloud", "--lang", "en", "--format", "json")
	if !strings.Contains(out, `"label": "Public domain"`) {
		t.Errorf("--format json --lang en:\n%s", out)
	}
	if _, _, err := runTpl(t, "show", "nope"); err == nil || !strings.Contains(err.Error(), "plantilla desconocida") {
		t.Errorf("plantilla desconocida: %v", err)
	}
	if _, _, err := runTpl(t); err == nil {
		t.Error("sin subcomando debe fallar")
	}
}

func TestCLITemplateAddPrintsDiffWithoutWriting(t *testing.T) {
	p := writeCfg(t, t.TempDir(), "haproxy.cfg", tplBase)
	out, errb, err := runTpl(t, append([]string{"add", "nextcloud", "--lang", "es", "--no-color"}, append(ncArgs, p)...)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--- " + p, "+frontend nextcloud_https", "+backend nextcloud", "+    server nc1 10.0.0.10:80 check", "+    bind *:443 ssl crt /etc/haproxy/certs/cloud.pem alpn h2,http/1.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en el diff:\n%s", want, out)
		}
	}
	if strings.Contains(out, "+global") || strings.Contains(out, "+defaults") {
		t.Errorf("no debe duplicar global ni defaults:\n%s", out)
	}
	if !strings.Contains(errb, "No se ha modificado ningún archivo") {
		t.Errorf("debe avisar de que no escribió: %q", errb)
	}
	if b, _ := os.ReadFile(p); string(b) != tplBase {
		t.Error("sin --write no debe modificar el archivo")
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("sin --write no debe crear .bak")
	}
	// Las opciones también valen detrás de las rutas de los parámetros y en cualquier idioma.
	out, _, err = runTpl(t, "add", "--lang", "en", "nextcloud", p, "--param", "domain=c.example.com", "--param", "servers=10.0.0.1:80", "--param", "servers=10.0.0.2:80", "--param", "cert=/c.pem")
	if err != nil || !strings.Contains(out, "server nc2 10.0.0.2:80 check") || !strings.Contains(out, "generated with the haro template") {
		t.Errorf("servers repetido y --lang en: %v\n%s", err, out)
	}
}

func TestCLITemplateAddErrors(t *testing.T) {
	p := writeCfg(t, t.TempDir(), "haproxy.cfg", tplBase)
	_, _, err := runTpl(t, "add", "nextcloud", "--lang", "en", "--param", "servers=10.0.0.10:80", p)
	if err == nil || !strings.Contains(err.Error(), "missing parameter “domain”") {
		t.Errorf("error en inglés: %v", err)
	}
	_, _, err = runTpl(t, "add", "nextcloud", "--lang", "es", "--param", "servers=10.0.0.10", "--param", "domain=a.com", "--param", "cert=/c.pem", p)
	if err == nil || !strings.Contains(err.Error(), "no es un servidor válido") {
		t.Errorf("error en español: %v", err)
	}
	if _, _, err := runTpl(t, "add", "nextcloud", "--param", "sinigual", p); err == nil {
		t.Error("--param sin = debe fallar")
	}
	if _, _, err := runTpl(t, "add", "nextcloud", "--param", "x=1", "-"); err == nil {
		t.Error("stdin no es válido para add")
	}
	if b, _ := os.ReadFile(p); string(b) != tplBase {
		t.Error("un error no debe modificar nada")
	}
}

func TestCLITemplateAddWrite(t *testing.T) {
	dir := t.TempDir()
	p := writeCfg(t, dir, "haproxy.cfg", tplBase)
	// --haproxy inexistente: valida con el detector de haro (sin depender de haproxy ni de la ruta del certificado).
	out, errb, err := runTpl(t, append([]string{"add", "nextcloud", "--write", "--haproxy", "/no/existe/haproxy", "--lang", "es"}, append(ncArgs, p)...)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, errb)
	}
	if !strings.Contains(out, "+backend nextcloud") || !strings.Contains(errb, "Guardado en "+p) {
		t.Errorf("salida inesperada:\n%s\n%s", out, errb)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "backend nextcloud") || !strings.HasPrefix(string(got), tplBase[:len(tplBase)-1]) {
		t.Errorf("el archivo no tiene el resultado esperado:\n%s", got)
	}
	if bak, _ := os.ReadFile(p + ".bak"); string(bak) != tplBase {
		t.Errorf(".bak debe tener el original:\n%s", bak)
	}
	// El resultado es exactamente lo que calcula el núcleo (los mismos pasos que --dry-run).
	cfg := core.NewConfig([]core.Source{{Path: p, Text: tplBase}})
	ops, _ := core.RenderTechTemplate("nextcloud", map[string]any{"domain": "cloud.example.com", "servers": "10.0.0.10:80", "cert": "/etc/haproxy/certs/cloud.pem"}, cfg, core.LangES)
	want, _, _ := core.Apply(cfg, ops...)
	if string(got) != want.Serialize(0) {
		t.Error("el archivo guardado difiere del resultado de Apply")
	}
}

func TestCLITemplateAddWriteRejectedKeepsFile(t *testing.T) {
	// Una configuración con un error (backend inexistente): la validación completa rechaza el guardado.
	broken := "frontend f\n    bind 127.0.0.1:80\n    default_backend nope\n"
	p := writeCfg(t, t.TempDir(), "haproxy.cfg", broken)
	_, _, err := runTpl(t, append([]string{"add", "nextcloud", "--write", "--haproxy", "/no/existe/haproxy"}, append(ncArgs, p)...)...)
	if err == nil || !strings.Contains(err.Error(), "no se guardó") {
		t.Errorf("debe rechazar el guardado: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != broken {
		t.Error("un guardado rechazado no debe modificar el archivo")
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("un guardado rechazado no debe crear .bak")
	}
}

func TestCLITemplateAddWriteMultiFile(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, "00-global.cfg", "global\n    daemon\n")
	writeCfg(t, dir, "10-defaults.cfg", "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n")
	apps := writeCfg(t, dir, "20-apps.cfg", "")
	_, errb, err := runTpl(t, append([]string{"add", "redis", "--write", "--haproxy", "/no/existe/haproxy", "--param", "bind=10.0.0.5:6379", "--param", "servers=10.0.0.20:6379"}, dir)...)
	if err != nil {
		t.Fatalf("%v\n%s", err, errb)
	}
	if b, _ := os.ReadFile(apps); !strings.Contains(string(b), "backend redis") {
		t.Errorf("el último archivo debe recibir la plantilla:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "00-global.cfg")); string(b) != "global\n    daemon\n" {
		t.Error("los demás archivos no deben tocarse")
	}
	if _, err := os.Stat(filepath.Join(dir, "00-global.cfg.bak")); err == nil {
		t.Error("solo se hace .bak de lo que cambia")
	}
}

// ---------- RPC ----------

func TestRPCListTemplates(t *testing.T) {
	c := newTestServer(t, &server{})
	for _, lang := range []string{"", "es", "en"} {
		res, err := c.core.ListTemplates(context.Background(), connect.NewRequest(&harov1.ListTemplatesRequest{Lang: lang}))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Msg.Templates) != len(core.TechTemplates(core.LangES)) {
			t.Fatalf("lang=%q: %d plantillas", lang, len(res.Msg.Templates))
		}
		var nc *harov1.TechTemplate
		for _, tp := range res.Msg.Templates {
			if tp.Id == "nextcloud" {
				nc = tp
			}
		}
		if nc == nil || nc.Kind != "web" || len(nc.Params) != 5 {
			t.Fatalf("nextcloud: %+v", nc)
		}
		wantName := map[string]string{"": "Dominio público", "es": "Dominio público", "en": "Public domain"}[lang]
		if nc.Params[0].Key != "domain" || nc.Params[0].Label != wantName || !nc.Params[0].Required {
			t.Errorf("lang=%q: domain: %+v", lang, nc.Params[0])
		}
		var hsts *harov1.TemplateParam
		for _, p := range nc.Params {
			if p.Key == "hsts" {
				hsts = p
			}
		}
		if hsts == nil || hsts.Type != "bool" || hsts.DefaultValue == nil || hsts.DefaultValue.GetBoolValue() != true {
			t.Errorf("hsts debe llevar default true tipado: %+v", hsts)
		}
	}
	if _, err := c.core.ListTemplates(context.Background(), connect.NewRequest(&harov1.ListTemplatesRequest{Lang: "fr"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("idioma desconocido: %v", err)
	}
}

func mustValue(t *testing.T, v any) *structpb.Value {
	t.Helper()
	x, err := structpb.NewValue(v)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestRPCRenderTemplate(t *testing.T) {
	c := newTestServer(t, &server{})
	req := &harov1.RenderTemplateRequest{
		Id: "nextcloud", Text: tplBase, Lang: "en",
		Params: map[string]*structpb.Value{
			"domain":  mustValue(t, "cloud.example.com"),
			"cert":    mustValue(t, "/etc/haproxy/certs/cloud.pem"),
			"servers": mustValue(t, []any{"10.0.0.10:80", "10.0.0.11:80"}),
			"hsts":    mustValue(t, false),
		},
	}
	res, err := c.core.RenderTemplate(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Msg
	if len(m.Ops) == 0 || len(m.Files) != 1 || len(m.Changes) != 1 {
		t.Fatalf("respuesta: %d ops, %d files, %d changes", len(m.Ops), len(m.Files), len(m.Changes))
	}
	text := m.Changes[0].Text
	for _, want := range []string{"backend nextcloud\n", "server nc2 10.0.0.11:80 check", "generated with the haro template"} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Strict-Transport-Security") {
		t.Error("hsts=false (bool tipado) debe quitar la cabecera HSTS")
	}
	if m.Files[0].Text != text || !strings.HasPrefix(text, tplBase[:len(tplBase)-1]) {
		t.Error("files debe traer el texto resultante")
	}
	// Las ops devueltas aplicadas con Apply dan el mismo resultado.
	ap, err := c.core.Apply(context.Background(), connect.NewRequest(&harov1.ApplyRequest{Text: tplBase, Ops: m.Ops}))
	if err != nil || ap.Msg.Files[0].Text != text {
		t.Errorf("Apply(ops) difiere: %v", err)
	}
	// Varios archivos: solo cambia el último.
	res, err = c.core.RenderTemplate(context.Background(), connect.NewRequest(&harov1.RenderTemplateRequest{
		Id: "redis", Params: map[string]*structpb.Value{"bind": mustValue(t, "10.0.0.5:6379"), "servers": mustValue(t, "10.0.0.20:6379")},
		Files: []*harov1.SourceFile{file("a.cfg", "global\n    daemon\n"), file("b.cfg", "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n")},
	}))
	if err != nil || len(res.Msg.Changes) != 1 || res.Msg.Changes[0].Path != "b.cfg" || len(res.Msg.Files) != 2 {
		t.Fatalf("varios archivos: %v %+v", err, res)
	}
	// Sin archivos ni texto: parte de una configuración vacía.
	res, err = c.core.RenderTemplate(context.Background(), connect.NewRequest(&harov1.RenderTemplateRequest{
		Id: "kubernetes", Params: map[string]*structpb.Value{"servers": mustValue(t, []any{"10.0.0.81:6443"})}}))
	if err != nil || !strings.Contains(res.Msg.Files[0].Text, "global") || !strings.Contains(res.Msg.Files[0].Text, "backend kubernetes") {
		t.Fatalf("configuración vacía: %v", err)
	}
}

func TestRPCRenderTemplateErrors(t *testing.T) {
	c := newTestServer(t, &server{})
	for _, tc := range []struct {
		id, lang string
		params   map[string]*structpb.Value
		want     string
	}{
		{"nextcloud", "en", map[string]*structpb.Value{}, "missing parameter “domain”"},
		{"nextcloud", "es", map[string]*structpb.Value{"domain": mustValue(t, "a b")}, "no es un dominio válido"},
		{"nextcloud", "es", map[string]*structpb.Value{"domain": mustValue(t, 5)}, "debe ser texto"},
		{"nope", "en", nil, "unknown template"},
	} {
		_, err := c.core.RenderTemplate(context.Background(), connect.NewRequest(&harov1.RenderTemplateRequest{Id: tc.id, Lang: tc.lang, Params: tc.params, Text: tplBase}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s/%s: quería InvalidArgument con %q, fue %v", tc.id, tc.lang, tc.want, err)
		}
	}
	if _, err := c.core.RenderTemplate(context.Background(), connect.NewRequest(&harov1.RenderTemplateRequest{Id: "redis", Lang: "xx"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("idioma desconocido: %v", err)
	}
}

func TestRPCTemplateContract(t *testing.T) {
	for _, lang := range core.Langs {
		for _, tp := range core.TechTemplates(lang) {
			if _, err := toProto(tp, &harov1.TechTemplate{}); err != nil {
				t.Errorf("%s (%s): %v", tp.ID, lang, err)
			}
		}
	}
}

// ---------- MCP ----------

func TestMCPTemplateListAndRender(t *testing.T) {
	text, st, isErr := mcpCall(t, &mcpTools{}, "haro_template_list", map[string]any{"lang": "en"})
	if isErr || !strings.Contains(text, "nextcloud (web)") || !strings.Contains(text, "- domain (string, obligatorio): Public domain") {
		t.Fatalf("list:\n%s", text)
	}
	if list, _ := st["templates"].([]any); len(list) != len(core.TechTemplates(core.LangEN)) {
		t.Errorf("structured: %v", st["templates"])
	}

	dir := t.TempDir()
	p := writeCfg(t, dir, "haproxy.cfg", tplBase)
	args := map[string]any{"paths": []string{p}, "id": "nextcloud", "params": map[string]any{
		"domain": "cloud.example.com", "cert": "/etc/haproxy/certs/cloud.pem", "servers": []string{"10.0.0.10:80"}}}
	text, st, isErr = mcpCall(t, &mcpTools{allowWrite: true}, "haro_template_render", args)
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "+backend nextcloud") || !strings.Contains(text, "@@") || !strings.Contains(text, "no se ha escrito nada") {
		t.Errorf("debe devolver el diff y avisar de que es una simulación:\n%s", text)
	}
	if st["written"] != false {
		t.Error("nunca escribe")
	}
	if b, _ := os.ReadFile(p); string(b) != tplBase {
		t.Error("el archivo cambió")
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("render no debe crear .bak")
	}
	ops, _ := st["ops"].([]any)
	if len(ops) < 5 {
		t.Fatalf("ops: %v", st["ops"])
	}

	// Las ops devueltas se aplican tal cual con haro_apply (y, con write, guarda).
	args2 := map[string]any{"paths": []string{p}, "ops": ops, "write": true}
	text, st2, isErr := mcpCall(t, &mcpTools{allowWrite: true, haproxyBin: ""}, "haro_apply", args2)
	if isErr {
		t.Fatal(text)
	}
	if st2["written"] != true {
		t.Errorf("haro_apply con las ops debe guardar: %v", st2)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "backend nextcloud") {
		t.Errorf("no se aplicó:\n%s", b)
	}
}

func TestMCPTemplateRenderErrorsAndLang(t *testing.T) {
	good := map[string]any{"domain": "c.example.com", "cert": "/c.pem", "servers": "10.0.0.1:80"}
	text, _, isErr := mcpCall(t, &mcpTools{}, "haro_template_render", map[string]any{"text": tplBase, "id": "nextcloud", "params": map[string]any{"servers": "10.0.0.1:80"}, "lang": "en"})
	if !isErr || !strings.Contains(text, "missing parameter “domain”") {
		t.Errorf("error en inglés: %v %s", isErr, text)
	}
	text, _, isErr = mcpCall(t, &mcpTools{}, "haro_template_render", map[string]any{"text": tplBase, "id": "nextcloud", "params": map[string]any{"servers": "10.0.0.1"}})
	if !isErr || !strings.Contains(text, "falta el parámetro «domain»") {
		t.Errorf("error en español: %v %s", isErr, text)
	}
	if _, _, isErr = mcpCall(t, &mcpTools{}, "haro_template_render", map[string]any{"text": tplBase, "id": "", "params": good}); !isErr {
		t.Error("id vacío debe fallar")
	}
	// Con text, los cambios incluyen el texto resultante (para quien no tiene archivos).
	_, st, isErr := mcpCall(t, &mcpTools{lang: core.LangEN}, "haro_template_render", map[string]any{"text": tplBase, "id": "nextcloud", "params": good})
	if isErr {
		t.Fatal("render con text")
	}
	ch, _ := st["changes"].([]any)
	if len(ch) != 1 || !strings.Contains(ch[0].(map[string]any)["text"].(string), "generated with the haro template") {
		t.Errorf("changes con text: %v", st["changes"])
	}
	// Una plantilla que introduce un problema lo comunica (Proxmox sin CA).
	text, st, _ = mcpCall(t, &mcpTools{}, "haro_template_render", map[string]any{"text": tplBase, "id": "proxmox", "params": map[string]any{"domain": "pve.example.com", "cert": "/c.pem", "servers": "10.0.0.1:8006"}})
	if is, _ := st["issues"].([]any); len(is) != 1 || !strings.Contains(text, "tls-no-verify") {
		t.Errorf("debe avisar del problema que introduce: %v\n%s", st["issues"], text)
	}
}
