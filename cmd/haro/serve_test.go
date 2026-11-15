package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/pkarc/haro/core"
	harov1 "github.com/pkarc/haro/gen/haro/v1"
	"github.com/pkarc/haro/gen/haro/v1/harov1connect"
)

type clients struct {
	url    string
	core   harov1connect.CoreServiceClient
	config harov1connect.ConfigServiceClient
}

func newTestServer(t *testing.T, s *server) clients {
	t.Helper()
	ts := httptest.NewServer(localOnly(limitBody(s.routes())))
	t.Cleanup(ts.Close)
	return clients{
		url:    ts.URL,
		core:   harov1connect.NewCoreServiceClient(ts.Client(), ts.URL),
		config: harov1connect.NewConfigServiceClient(ts.Client(), ts.URL),
	}
}

func file(path, text string) *harov1.SourceFile { return &harov1.SourceFile{Path: path, Text: text} }

func put(t *testing.T, c clients, files ...*harov1.SourceFile) (*harov1.PutConfigResponse, error) {
	t.Helper()
	res, err := c.config.PutConfig(context.Background(), connect.NewRequest(&harov1.PutConfigRequest{Files: files}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func TestPutConfigGuards(t *testing.T) {
	f := filepath.Join(t.TempDir(), "haproxy.cfg")
	os.WriteFile(f, []byte("global\n    maxconn 10\n"), 0o640)

	ro := newTestServer(t, &server{paths: []string{f}})
	if _, err := put(t, ro, file(f, "global\n")); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("sin --allow-write debería ser PermissionDenied, fue %v", err)
	}

	rw := newTestServer(t, &server{paths: []string{f}, allowWrite: true})
	res, err := put(t, rw, file(f, "frontend f\n    bind *:80\n    default_backend nope\n"))
	if err != nil || res.Saved || res.Error == "" || res.Issue == nil {
		t.Errorf("con errores debería rechazarse con saved=false, motivo e issue: %+v, %v", res, err)
	}
	if _, err := put(t, rw, file("/etc/passwd", "x")); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("un archivo ajeno a la configuración debería ser InvalidArgument, fue %v", err)
	}
	res, err = put(t, rw, file(f, "global\n    maxconn 20\n"))
	if err != nil || !res.Saved || res.Path != f {
		t.Fatalf("una configuración válida debería guardarse: %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(f); string(b) != "global\n    maxconn 20\n" {
		t.Errorf("contenido guardado inesperado: %q", b)
	}
	if b, _ := os.ReadFile(f + ".bak"); string(b) != "global\n    maxconn 10\n" {
		t.Errorf("la copia .bak no contiene la versión anterior: %q", b)
	}
	// Compatibilidad: path y text sueltos, sin files.
	if res, err := rw.config.PutConfig(context.Background(), connect.NewRequest(&harov1.PutConfigRequest{Path: f, Text: "global\n    maxconn 30\n"})); err != nil || !res.Msg.Saved {
		t.Errorf("path+text debería guardarse: %v", err)
	}
}

func TestMultiFileServe(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "20-web.cfg")
	api := filepath.Join(dir, "30-api.cfg")
	os.WriteFile(filepath.Join(dir, "10-global.cfg"), []byte("defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n"), 0o640)
	os.WriteFile(web, []byte("frontend web\n    bind *:80\n    default_backend api\n"), 0o640)
	os.WriteFile(api, []byte("backend api\n    server a 10.0.0.1:80\n"), 0o640)
	os.WriteFile(filepath.Join(dir, ".oculto.cfg"), []byte("basura"), 0o640)

	c := newTestServer(t, &server{paths: []string{dir}, allowWrite: true})
	got, err := c.config.GetConfig(context.Background(), connect.NewRequest(&harov1.GetConfigRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Msg.Files); n != 3 || filepath.Base(got.Msg.Files[0].Path) != "10-global.cfg" {
		t.Fatalf("se esperaban 3 archivos en orden lexicográfico, sin ocultos: %+v", got.Msg.Files)
	}

	// Cambiar 20-web.cfg es válido porque el backend vive en 30-api.cfg.
	if res, err := put(t, c, file(web, "frontend web\n    bind *:8080\n    default_backend api\n")); err != nil || !res.Saved {
		t.Errorf("el cambio en 20-web.cfg debería guardarse: %+v, %v", res, err)
	}
	// Vaciar 30-api.cfg rompería la referencia desde 20-web.cfg.
	if res, err := put(t, c, file(api, "# vacío\n")); err != nil || res.Saved {
		t.Errorf("romper una referencia entre archivos debería rechazarse: %+v, %v", res, err)
	}
}

// Un cambio que toca dos archivos a la vez (renombrar un backend y su
// referencia) solo es válido como conjunto: se guarda de una vez o no se guarda.
func TestPutConfigMultiFileAtomic(t *testing.T) {
	dir := t.TempDir()
	web := filepath.Join(dir, "20-web.cfg")
	api := filepath.Join(dir, "30-api.cfg")
	os.WriteFile(web, []byte("frontend f\n    bind *:80\n    default_backend api\n"), 0o640)
	os.WriteFile(api, []byte("backend api\n    server s 1.1.1.1:80\n"), 0o640)
	c := newTestServer(t, &server{paths: []string{dir}, allowWrite: true})

	// Por separado, cada archivo dejaría una referencia rota.
	if res, err := put(t, c, file(api, "backend api2\n    server s 1.1.1.1:80\n")); err != nil || res.Saved {
		t.Errorf("guardar solo el backend renombrado debería rechazarse: %+v, %v", res, err)
	}
	files := []*harov1.SourceFile{
		file(web, "frontend f\n    bind *:80\n    default_backend api2\n"),
		file(api, "backend api2\n    server s 1.1.1.1:80\n"),
	}
	if res, err := put(t, c, files...); err != nil || !res.Saved || len(res.Files) != 2 {
		t.Fatalf("el conjunto válido debería guardarse: %+v, %v", res, err)
	}
	for _, f := range files {
		if b, _ := os.ReadFile(f.Path); string(b) != f.Text {
			t.Errorf("%s no se guardó: %q", f.Path, b)
		}
		if _, err := os.Stat(f.Path + ".bak"); err != nil {
			t.Errorf("falta la copia .bak de %s", f.Path)
		}
	}
	// Un conjunto inválido no toca ningún archivo.
	bad := []*harov1.SourceFile{
		file(web, "frontend f\n    bind *:80\n    default_backend nope\n"),
		file(api, "backend api3\n    server s 1.1.1.1:80\n"),
	}
	if res, err := put(t, c, bad...); err != nil || res.Saved {
		t.Errorf("conjunto inválido debería rechazarse: %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(api); string(b) != files[1].Text {
		t.Errorf("un guardado rechazado modificó %s: %q", api, b)
	}
}

func TestLocalOnly(t *testing.T) {
	c := newTestServer(t, &server{})
	req, _ := http.NewRequest("POST", c.url+"/haro.v1.CoreService/Analyze", strings.NewReader(`{"text":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "evil.example"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 403 {
		t.Errorf("un Host externo debería recibir 403, recibió %d", res.StatusCode)
	}
}

// Una página externa no puede escribir ni analizar con una petición «simple»
// del navegador (text/plain, sin preflight): Connect solo acepta sus tipos.
func TestRejectsSimpleCrossOriginPosts(t *testing.T) {
	c := newTestServer(t, &server{allowWrite: true, paths: []string{"x.cfg"}})
	for _, path := range []string{"/haro.v1.ConfigService/PutConfig", "/haro.v1.CoreService/Analyze"} {
		req, _ := http.NewRequest("POST", c.url+path, strings.NewReader(`{"text":"x"}`))
		req.Header.Set("Content-Type", "text/plain")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("%s con text/plain debería recibir 415, recibió %d", path, res.StatusCode)
		}
	}
}

func TestExpandPaths(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.cfg", "a.cfg", "10.cfg", "9.cfg", ".h.cfg", "x.txt"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o640)
	}
	os.Mkdir(filepath.Join(dir, "sub.cfg"), 0o750)
	got, err := expandPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	// Orden de bytes, como LC_COLLATE=C en HAProxy: "10" va antes que "9".
	if strings.Join(names, ",") != "10.cfg,9.cfg,a.cfg,b.cfg" {
		t.Errorf("expandPaths = %v", names)
	}
}

func TestApply(t *testing.T) {
	c := newTestServer(t, &server{})
	files := []*harov1.SourceFile{
		file("a.cfg", "backend a  \r\n    server s 1.1.1.1:80\n"),
		file("b.cfg", "backend b\n    server s 2.2.2.2:80  \n"),
	}
	res, err := c.core.Apply(context.Background(), connect.NewRequest(&harov1.ApplyRequest{
		Files: files,
		Ops:   []*harov1.Op{{Op: "add_directive", Section: 1, Text: "balance leastconn"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.Changes) != 1 || res.Msg.Changes[0].File != 1 {
		t.Errorf("solo debía cambiar el segundo archivo: %+v", res.Msg.Changes)
	}
	if res.Msg.Files[0].Text != files[0].Text {
		t.Errorf("el archivo sin cambios debe volver byte a byte: %q", res.Msg.Files[0].Text)
	}
	if !strings.Contains(res.Msg.Files[1].Text, "    balance leastconn\n") {
		t.Errorf("falta la directiva: %q", res.Msg.Files[1].Text)
	}

	// Las operaciones opcionales (file, at, to) llegan como punteros.
	to := int32(0)
	res, err = c.core.Apply(context.Background(), connect.NewRequest(&harov1.ApplyRequest{
		Files: files,
		Ops:   []*harov1.Op{{Op: "move_section", Section: 1, To: &to, File: new(int32)}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Msg.Files[0].Text; !strings.HasPrefix(got, "backend b\n") {
		t.Errorf("move_section con to=0 no movió la sección: %q", got)
	}

	_, err = c.core.Apply(context.Background(), connect.NewRequest(&harov1.ApplyRequest{
		Files: files,
		Ops:   []*harov1.Op{{Op: "add_directive", Section: 0, Text: "backend x"}},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("una operación inválida debería ser InvalidArgument, fue %v", err)
	}
}

func TestKnowledge(t *testing.T) {
	c := newTestServer(t, &server{})
	res, err := c.core.GetKnowledge(context.Background(), connect.NewRequest(&harov1.GetKnowledgeRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	k := res.Msg
	if len(k.Sections) == 0 || len(k.Directives["server"].GetSections()) == 0 || len(k.Rules) == 0 || k.Templates["backend"] == nil {
		t.Errorf("respuesta incompleta: %+v", k)
	}
}

func TestCapabilitiesAndDiffAndCheck(t *testing.T) {
	c := newTestServer(t, &server{})
	caps, err := c.config.GetCapabilities(context.Background(), connect.NewRequest(&harov1.GetCapabilitiesRequest{}))
	if err != nil || caps.Msg.Version == "" || caps.Msg.CanWrite {
		t.Errorf("capabilities inesperadas: %+v, %v", caps, err)
	}
	if _, err := c.config.GetConfig(context.Background(), connect.NewRequest(&harov1.GetConfigRequest{})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("sin --file GetConfig debería ser NotFound, fue %v", err)
	}
	d, err := c.core.Diff(context.Background(), connect.NewRequest(&harov1.DiffRequest{Before: "a\nb", After: "a\nc"}))
	if err != nil || len(d.Msg.Ops) != 4 {
		t.Errorf("diff inesperado: %+v, %v", d, err)
	}
	ck, err := c.core.Check(context.Background(), connect.NewRequest(&harov1.CheckRequest{Text: "global\n"}))
	if err != nil || ck.Msg.Available {
		t.Errorf("sin haproxy Check debería informar available=false: %+v, %v", ck, err)
	}
}

// El análisis de todos los casos de testdata (y del corpus, si existe) debe
// poder expresarse en el contrato proto: si el núcleo añade un campo que el
// .proto no tiene, la conversión estricta falla aquí.
func TestRPCContract(t *testing.T) {
	var cases [][]core.Source
	single, _ := filepath.Glob("../../core/testdata/*.cfg")
	corpus, _ := filepath.Glob("../../core/testdata/corpus/*/*.cfg")
	for _, p := range append(single, corpus...) {
		b, _ := os.ReadFile(p)
		cases = append(cases, []core.Source{{Path: filepath.Base(p), Text: string(b)}})
	}
	for _, dir := range []string{"multi", "multiok"} {
		names, _ := filepath.Glob("../../core/testdata/" + dir + "/*.cfg")
		var srcs []core.Source
		for _, n := range names {
			b, _ := os.ReadFile(n)
			srcs = append(srcs, core.Source{Path: filepath.Base(n), Text: string(b)})
		}
		cases = append(cases, srcs)
	}
	if len(cases) < 5 {
		t.Fatalf("faltan casos: %d", len(cases))
	}
	for _, srcs := range cases {
		a := core.AnalyzeConfig(srcs)
		out, err := toProto(a, &harov1.Analysis{})
		if err != nil {
			t.Fatalf("%s: %v", srcs[0].Path, err)
		}
		if len(out.Issues) != len(a.Issues) || len(out.Doc.Sections) != len(a.Doc.Sections) || len(out.Explanations) != len(a.Explanations) {
			t.Errorf("%s: la conversión perdió datos", srcs[0].Path)
		}
	}
}
