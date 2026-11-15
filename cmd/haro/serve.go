package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/haro/haro/core"
	"github.com/haro/haro/web"
)

// El API local usa JSON sobre HTTP con las mismas rutas y mensajes que el
// servicio Connect definido en proto/haro/v1/haro.proto. Cuando se añadan
// las dependencias de Connect, el cliente TypeScript generado hablará con
// este mismo servidor sin cambios en la web.

type server struct {
	file       string
	allowWrite bool
	haproxyBin string
}

type capabilities struct {
	Version    string `json:"version"`
	File       string `json:"file,omitempty"`
	CanRead    bool   `json:"canRead"`
	CanWrite   bool   `json:"canWrite"`
	CanCheck   bool   `json:"canCheck"`
	HAProxyBin string `json:"haproxyBin,omitempty"`
}

func runServe(args []string) error {
	fls := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fls.String("addr", "127.0.0.1:7468", "dirección de escucha (por seguridad, solo localhost por defecto)")
	file := fls.String("file", "", "archivo haproxy.cfg que la web puede leer (p. ej. /etc/haproxy/haproxy.cfg)")
	allowWrite := fls.Bool("allow-write", false, "permite guardar en --file desde la web (siempre valida antes y crea copia .bak)")
	bin := fls.String("haproxy", "haproxy", "binario de haproxy para validar con -c")
	fls.Usage = func() {
		fmt.Fprintln(fls.Output(), "Uso: haro serve [opciones]")
		fls.PrintDefaults()
	}
	fls.Parse(args)

	s := &server{file: *file, allowWrite: *allowWrite}
	if p, err := exec.LookPath(*bin); err == nil {
		s.haproxyBin = p
	}
	if s.allowWrite && s.file == "" {
		return errors.New("--allow-write requiere --file")
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Printf("⚠ escuchando en %s: cualquiera en la red podrá ver y analizar configuraciones. No hay autenticación.", *addr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/capabilities", s.handleCapabilities)
	mux.HandleFunc("POST /api/v1/analyze", s.handleAnalyze)
	mux.HandleFunc("POST /api/v1/diff", s.handleDiff)
	mux.HandleFunc("POST /api/v1/check", s.handleCheck)
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	mux.Handle("/", http.FileServer(http.FS(web.FS())))

	var handler http.Handler = limitBody(mux)
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		handler = localOnly(handler)
	}
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("haro %s escuchando en http://%s", version, *addr)
	if s.file != "" {
		log.Printf("archivo: %s (escritura %s)", s.file, map[bool]string{true: "permitida", false: "desactivada"}[s.allowWrite])
	}
	if s.haproxyBin == "" {
		log.Printf("no se encontró el binario de haproxy: la validación real (haproxy -c) no estará disponible")
	}
	return srv.ListenAndServe()
}

// localOnly rechaza peticiones cuyo Host no sea local, para evitar ataques de
// DNS rebinding desde páginas web abiertas en el mismo equipo.
func localOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			writeErr(w, 403, "host no permitido")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func limitBody(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, capabilities{Version: version, File: s.file, CanRead: s.file != "", CanWrite: s.allowWrite, CanCheck: s.haproxyBin != "", HAProxyBin: s.haproxyBin})
}

func (s *server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	writeJSON(w, 200, core.Analyze(req.Text))
}

func (s *server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Before string `json:"before"`
		After  string `json:"after"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	writeJSON(w, 200, map[string]any{"ops": core.DiffLines(core.Normalize(req.Before), core.Normalize(req.After))})
}

type checkResult struct {
	Available bool   `json:"available"`
	OK        bool   `json:"ok"`
	Output    string `json:"output"`
}

// check valida el texto con `haproxy -c` usando un archivo temporal.
func (s *server) check(ctx context.Context, text string) (checkResult, error) {
	if s.haproxyBin == "" {
		return checkResult{Available: false}, nil
	}
	f, err := os.CreateTemp("", "haro-*.cfg")
	if err != nil {
		return checkResult{}, err
	}
	defer os.Remove(f.Name())
	if _, err := io.WriteString(f, text); err != nil {
		f.Close()
		return checkResult{}, err
	}
	f.Close()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.haproxyBin, "-c", "-f", f.Name()).CombinedOutput()
	res := checkResult{Available: true, OK: err == nil, Output: strings.ReplaceAll(string(out), f.Name(), "haproxy.cfg")}
	return res, nil
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	res, err := s.check(r.Context(), req.Text)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	if s.file == "" {
		writeErr(w, 404, "haro serve se inició sin --file")
		return
	}
	b, err := os.ReadFile(s.file)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": s.file, "text": string(b)})
}

// handlePutConfig guarda la configuración solo si --allow-write está activo
// y, cuando hay binario de haproxy, solo si `haproxy -c` la acepta. Escribe
// de forma atómica y deja una copia .bak de la versión anterior.
func (s *server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite {
		writeErr(w, 403, "la escritura está desactivada; inicia haro serve con --allow-write")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	res, err := s.check(r.Context(), req.Text)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if res.Available && !res.OK {
		writeJSON(w, 422, map[string]any{"error": "haproxy -c rechazó la configuración; no se guardó", "check": res})
		return
	}
	if !res.Available {
		// Sin haproxy instalado, el detector de haro es la única barrera.
		for _, i := range core.Lint(core.Parse(req.Text)) {
			if i.Severity == core.SevError {
				writeJSON(w, 422, map[string]any{"error": "la configuración tiene errores y no hay haproxy para validarla; no se guardó", "issue": i})
				return
			}
		}
	}
	if old, err := os.ReadFile(s.file); err == nil {
		if err := os.WriteFile(s.file+".bak", old, 0o640); err != nil {
			writeErr(w, 500, "no se pudo crear la copia de seguridad: "+err.Error())
			return
		}
	}
	mode := os.FileMode(0o640)
	if fi, err := os.Stat(s.file); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.file), ".haro-*.cfg")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, req.Text); err != nil {
		tmp.Close()
		writeErr(w, 500, err.Error())
		return
	}
	tmp.Close()
	os.Chmod(tmp.Name(), mode)
	if err := os.Rename(tmp.Name(), s.file); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"saved": true, "path": s.file, "backup": s.file + ".bak", "check": res})
}
