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

	"github.com/pkarc/haro/core"
	"github.com/pkarc/haro/web"
)

// El API local usa JSON sobre HTTP con las mismas rutas y mensajes que el
// servicio definido en proto/haro/v1/haro.proto. Cuando se añadan las
// dependencias de Connect, el cliente TypeScript generado hablará con este
// mismo servidor sin cambios en la web.

type server struct {
	// paths son los argumentos de --file tal cual (archivos o directorios).
	// Se expanden en cada petición para reflejar archivos nuevos o borrados.
	paths      []string
	allowWrite bool
	haproxyBin string
}

type capabilities struct {
	Version    string   `json:"version"`
	Paths      []string `json:"paths,omitempty"`
	CanRead    bool     `json:"canRead"`
	CanWrite   bool     `json:"canWrite"`
	CanCheck   bool     `json:"canCheck"`
	HAProxyBin string   `json:"haproxyBin,omitempty"`
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runServe(args []string) error {
	fls := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fls.String("addr", "127.0.0.1:7468", "dirección de escucha (por seguridad, solo localhost por defecto)")
	var files multiFlag
	fls.Var(&files, "file", "archivo o directorio de configuración; repítelo para varios, en el orden de haproxy -f")
	allowWrite := fls.Bool("allow-write", false, "permite guardar desde la web (siempre valida antes y crea copia .bak)")
	bin := fls.String("haproxy", "haproxy", "binario de haproxy para validar con -c")
	fls.Usage = func() {
		fmt.Fprintln(fls.Output(), "Uso: haro serve [opciones]")
		fls.PrintDefaults()
	}
	fls.Parse(args)

	s := &server{paths: files, allowWrite: *allowWrite}
	if p, err := exec.LookPath(*bin); err == nil {
		s.haproxyBin = p
	}
	if s.allowWrite && len(s.paths) == 0 {
		return errors.New("--allow-write requiere al menos un --file")
	}
	if len(s.paths) > 0 {
		if _, err := expandPaths(s.paths); err != nil {
			return err
		}
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return err
	}
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if !loopback {
		log.Printf("⚠ escuchando en %s: cualquiera en la red podrá ver y analizar configuraciones. No hay autenticación.", *addr)
	}

	var handler http.Handler = limitBody(s.routes())
	if loopback {
		handler = localOnly(handler)
	}
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("haro %s escuchando en http://%s", version, *addr)
	if len(s.paths) > 0 {
		log.Printf("configuración: %s (escritura %s)", strings.Join(s.paths, " "), map[bool]string{true: "permitida", false: "desactivada"}[s.allowWrite])
	}
	if s.haproxyBin == "" {
		log.Printf("no se encontró el binario de haproxy: la validación real (haproxy -c) no estará disponible")
	}
	return srv.ListenAndServe()
}

func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/capabilities", s.handleCapabilities)
	mux.HandleFunc("GET /api/v1/knowledge", s.handleKnowledge)
	mux.HandleFunc("POST /api/v1/analyze", s.handleAnalyze)
	mux.HandleFunc("POST /api/v1/apply", s.handleApply)
	mux.HandleFunc("POST /api/v1/diff", s.handleDiff)
	mux.HandleFunc("POST /api/v1/check", s.handleCheck)
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	mux.Handle("/", http.FileServer(http.FS(web.FS())))
	return mux
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
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
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

func decode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

// sourcesFromRequest acepta {"text": "..."} para un único archivo o
// {"files": [{"path","text"}, ...]} para varios, en orden de carga.
type analyzeRequest struct {
	Text  string        `json:"text"`
	Files []core.Source `json:"files"`
}

func (r analyzeRequest) sources() []core.Source {
	if len(r.Files) > 0 {
		return r.Files
	}
	return []core.Source{{Path: "haproxy.cfg", Text: r.Text}}
}

func (s *server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, capabilities{Version: version, Paths: s.paths, CanRead: len(s.paths) > 0, CanWrite: s.allowWrite, CanCheck: s.haproxyBin != "", HAProxyBin: s.haproxyBin})
}

// handleKnowledge expone los datos que la interfaz necesita para el
// autocompletado y el modo visual (tipos de sección, categorías, directivas
// conocidas por sección y plantillas), para que la web no duplique nada.
func (s *server) handleKnowledge(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, core.KB.Public())
}

func (s *server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	writeJSON(w, 200, core.AnalyzeConfig(req.sources()))
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

// handleApply aplica operaciones de edición sobre los archivos recibidos y
// devuelve los archivos resultantes, sin tocar el disco: guardar sigue siendo
// PUT /api/v1/config, con todas sus validaciones. files lleva todos los
// archivos de la configuración; changes, solo los que cambiaron.
func (s *server) handleApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		analyzeRequest
		Ops []core.Op `json:"ops"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	_, changes, err := core.Apply(core.NewConfig(req.sources()), req.Ops...)
	if err != nil {
		var ae *core.ApplyError
		if errors.As(err, &ae) {
			writeJSON(w, 422, map[string]any{"error": ae.Error(), "index": ae.Index, "op": ae.Op, "message": ae.Msg})
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	// Los archivos sin cambios se devuelven con su texto original, byte a byte.
	files := append([]core.Source{}, req.sources()...)
	for _, c := range changes {
		files[c.File].Text = c.Text
	}
	if changes == nil {
		changes = []core.FileChange{}
	}
	writeJSON(w, 200, map[string]any{"files": files, "changes": changes})
}

type checkResult struct {
	Available bool   `json:"available"`
	OK        bool   `json:"ok"`
	Output    string `json:"output"`
}

// check valida con `haproxy -c`, escribiendo cada archivo en un directorio
// temporal y pasándolos con -f en el mismo orden.
func (s *server) check(ctx context.Context, srcs []core.Source) (checkResult, error) {
	if s.haproxyBin == "" {
		return checkResult{Available: false}, nil
	}
	dir, err := os.MkdirTemp("", "haro-check-*")
	if err != nil {
		return checkResult{}, err
	}
	defer os.RemoveAll(dir)
	args := []string{"-c"}
	replacer := []string{}
	for i, src := range srcs {
		p := filepath.Join(dir, fmt.Sprintf("%03d.cfg", i))
		if err := os.WriteFile(p, []byte(src.Text), 0o600); err != nil {
			return checkResult{}, err
		}
		args = append(args, "-f", p)
		replacer = append(replacer, p, src.Path)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.haproxyBin, args...).CombinedOutput()
	return checkResult{Available: true, OK: err == nil, Output: strings.NewReplacer(replacer...).Replace(string(out))}, nil
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	res, err := s.check(r.Context(), req.sources())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	if len(s.paths) == 0 {
		writeErr(w, 404, "haro serve se inició sin --file")
		return
	}
	srcs, err := readSources(s.paths)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"files": srcs})
}

// handlePutConfig guarda uno o varios archivos de la configuración. Acepta
// {path, text} para uno o {files: [{path, text}]} para varios, que se validan
// y se guardan juntos: así un cambio que toca dos archivos a la vez (por
// ejemplo renombrar un backend y sus referencias) no queda a medias. Solo
// funciona con --allow-write; valida la configuración completa (todos los
// archivos, con los cambios aplicados) con `haproxy -c` o, si no está
// instalado, con el detector de haro. Escribe de forma atómica, deja una copia
// .bak de cada archivo y, si falla una escritura, restaura los ya escritos.
func (s *server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite {
		writeErr(w, 403, "la escritura está desactivada; inicia haro serve con --allow-write")
		return
	}
	var req struct {
		core.Source
		Files []core.Source `json:"files"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "petición inválida")
		return
	}
	changes := req.Files
	if len(changes) == 0 {
		changes = []core.Source{req.Source}
	}
	srcs, err := readSources(s.paths)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	old := make([]string, len(srcs))
	for i, src := range srcs {
		old[i] = src.Text
	}
	touched := map[int]bool{}
	for _, c := range changes {
		idx := -1
		for i, src := range srcs {
			if src.Path == c.Path || (c.Path == "" && len(srcs) == 1) {
				idx = i
				break
			}
		}
		if idx < 0 {
			writeErr(w, 400, "ese archivo no forma parte de la configuración servida")
			return
		}
		srcs[idx].Text = c.Text
		touched[idx] = true
	}

	res, err := s.check(r.Context(), srcs)
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
		for _, i := range core.Lint(core.NewConfig(srcs).Merge()) {
			if i.Severity == core.SevError {
				writeJSON(w, 422, map[string]any{"error": "la configuración tiene errores y no hay haproxy para validarla; no se guardó", "issue": i, "path": srcs[i.File].Path})
				return
			}
		}
	}

	var written []int
	var saved []map[string]string
	for i := range srcs {
		if !touched[i] {
			continue
		}
		if err := writeFile(srcs[i].Path, srcs[i].Text, true); err != nil {
			for _, j := range written { // deshacer lo ya escrito
				_ = writeFile(srcs[j].Path, old[j], false)
			}
			writeErr(w, 500, err.Error())
			return
		}
		written = append(written, i)
		saved = append(saved, map[string]string{"path": srcs[i].Path, "backup": srcs[i].Path + ".bak"})
	}
	out := map[string]any{"saved": true, "files": saved, "check": res}
	if len(saved) == 1 {
		out["path"], out["backup"] = saved[0]["path"], saved[0]["backup"]
	}
	writeJSON(w, 200, out)
}

// writeFile escribe el archivo de forma atómica (temporal + rename), con copia
// .bak de la versión anterior si backup es true.
func writeFile(path, text string, backup bool) error {
	mode := os.FileMode(0o640)
	if old, err := os.ReadFile(path); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		if backup {
			if err := os.WriteFile(path+".bak", old, mode); err != nil {
				return fmt.Errorf("no se pudo crear la copia de seguridad: %w", err)
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".haro-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.WriteString(tmp, text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
