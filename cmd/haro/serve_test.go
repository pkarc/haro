package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, s *server) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/analyze", s.handleAnalyze)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	ts := httptest.NewServer(localOnly(limitBody(mux)))
	t.Cleanup(ts.Close)
	return ts
}

func put(t *testing.T, url, body string) *http.Response {
	req, _ := http.NewRequest("PUT", url, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPutConfigGuards(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "haproxy.cfg")
	os.WriteFile(file, []byte("global\n    maxconn 10\n"), 0o640)

	ro := newTestServer(t, &server{file: file})
	if res := put(t, ro.URL+"/api/v1/config", `{"text":"global\n"}`); res.StatusCode != 403 {
		t.Errorf("sin --allow-write debería responder 403, respondió %d", res.StatusCode)
	}

	rw := newTestServer(t, &server{file: file, allowWrite: true})
	bad := `{"text":"frontend f\n    bind *:80\n    default_backend nope\n"}`
	if res := put(t, rw.URL+"/api/v1/config", bad); res.StatusCode != 422 {
		t.Errorf("con errores debería responder 422, respondió %d", res.StatusCode)
	}
	good := `{"text":"global\n    maxconn 20\n"}`
	if res := put(t, rw.URL+"/api/v1/config", good); res.StatusCode != 200 {
		t.Fatalf("una configuración válida debería guardarse, respondió %d", res.StatusCode)
	}
	if b, _ := os.ReadFile(file); string(b) != "global\n    maxconn 20\n" {
		t.Errorf("contenido guardado inesperado: %q", b)
	}
	if b, _ := os.ReadFile(file + ".bak"); string(b) != "global\n    maxconn 10\n" {
		t.Errorf("la copia .bak no contiene la versión anterior: %q", b)
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
