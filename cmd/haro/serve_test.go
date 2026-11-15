package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, s *server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(localOnly(limitBody(s.routes())))
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, strings.NewReader(string(b)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPutConfigGuards(t *testing.T) {
	file := filepath.Join(t.TempDir(), "haproxy.cfg")
	os.WriteFile(file, []byte("global\n    maxconn 10\n"), 0o640)

	ro := newTestServer(t, &server{paths: []string{file}})
	if res := do(t, "PUT", ro.URL+"/api/v1/config", map[string]string{"path": file, "text": "global\n"}); res.StatusCode != 403 {
		t.Errorf("sin --allow-write debería responder 403, respondió %d", res.StatusCode)
	}

	rw := newTestServer(t, &server{paths: []string{file}, allowWrite: true})
	bad := "frontend f\n    bind *:80\n    default_backend nope\n"
	if res := do(t, "PUT", rw.URL+"/api/v1/config", map[string]string{"path": file, "text": bad}); res.StatusCode != 422 {
		t.Errorf("con errores debería responder 422, respondió %d", res.StatusCode)
	}
	if res := do(t, "PUT", rw.URL+"/api/v1/config", map[string]string{"path": "/etc/passwd", "text": "x"}); res.StatusCode != 400 {
		t.Errorf("un archivo ajeno a la configuración debería responder 400, respondió %d", res.StatusCode)
	}
	if res := do(t, "PUT", rw.URL+"/api/v1/config", map[string]string{"path": file, "text": "global\n    maxconn 20\n"}); res.StatusCode != 200 {
		t.Fatalf("una configuración válida debería guardarse, respondió %d", res.StatusCode)
	}
	if b, _ := os.ReadFile(file); string(b) != "global\n    maxconn 20\n" {
		t.Errorf("contenido guardado inesperado: %q", b)
	}
	if b, _ := os.ReadFile(file + ".bak"); string(b) != "global\n    maxconn 10\n" {
		t.Errorf("la copia .bak no contiene la versión anterior: %q", b)
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

	ts := newTestServer(t, &server{paths: []string{dir}, allowWrite: true})

	res := do(t, "GET", ts.URL+"/api/v1/config", nil)
	var got struct {
		Files []struct{ Path string } `json:"files"`
	}
	json.NewDecoder(res.Body).Decode(&got)
	if len(got.Files) != 3 || filepath.Base(got.Files[0].Path) != "10-global.cfg" {
		t.Fatalf("se esperaban 3 archivos en orden lexicográfico, sin ocultos: %+v", got.Files)
	}

	// Cambiar 20-web.cfg es válido porque el backend vive en 30-api.cfg.
	if res := do(t, "PUT", ts.URL+"/api/v1/config", map[string]string{"path": web, "text": "frontend web\n    bind *:8080\n    default_backend api\n"}); res.StatusCode != 200 {
		t.Errorf("el cambio en 20-web.cfg debería guardarse, respondió %d", res.StatusCode)
	}
	// Vaciar 30-api.cfg rompería la referencia desde 20-web.cfg.
	if res := do(t, "PUT", ts.URL+"/api/v1/config", map[string]string{"path": api, "text": "# vacío\n"}); res.StatusCode != 422 {
		t.Errorf("romper una referencia entre archivos debería responder 422, respondió %d", res.StatusCode)
	}
}

func TestLocalOnly(t *testing.T) {
	ts := newTestServer(t, &server{})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/analyze", strings.NewReader(`{"text":""}`))
	req.Host = "evil.example"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 403 {
		t.Errorf("un Host externo debería recibir 403, recibió %d", res.StatusCode)
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

func TestApplyEndpoint(t *testing.T) {
	ts := newTestServer(t, &server{})
	files := []map[string]string{
		{"path": "a.cfg", "text": "backend a  \r\n    server s 1.1.1.1:80\n"},
		{"path": "b.cfg", "text": "backend b\n    server s 2.2.2.2:80  \n"},
	}
	res := do(t, "POST", ts.URL+"/api/v1/apply", map[string]any{
		"files": files,
		"ops":   []map[string]any{{"op": "add_directive", "section": 1, "text": "balance leastconn"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("apply debería responder 200, respondió %d", res.StatusCode)
	}
	var out struct {
		Files   []struct{ Path, Text string } `json:"files"`
		Changes []struct{ File int }          `json:"changes"`
	}
	json.NewDecoder(res.Body).Decode(&out)
	if len(out.Changes) != 1 || out.Changes[0].File != 1 {
		t.Errorf("solo debía cambiar el segundo archivo: %+v", out.Changes)
	}
	if out.Files[0].Text != files[0]["text"] {
		t.Errorf("el archivo sin cambios debe volver byte a byte: %q", out.Files[0].Text)
	}
	if !strings.Contains(out.Files[1].Text, "    balance leastconn\n") {
		t.Errorf("falta la directiva: %q", out.Files[1].Text)
	}

	res = do(t, "POST", ts.URL+"/api/v1/apply", map[string]any{
		"files": files,
		"ops":   []map[string]any{{"op": "add_directive", "section": 0, "text": "backend x"}},
	})
	if res.StatusCode != 422 {
		t.Errorf("una operación inválida debería responder 422, respondió %d", res.StatusCode)
	}
}
