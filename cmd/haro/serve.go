package main

import (
	"context"
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

// El API local es el servicio Connect definido en proto/haro/v1/haro.proto
// (ver rpc.go). Hablan con él la web (cliente TypeScript generado) y cualquier
// cliente Connect, gRPC o HTTP+JSON.

type server struct {
	// paths son los argumentos de --file tal cual (archivos o directorios).
	// Se expanden en cada petición para reflejar archivos nuevos o borrados.
	paths      []string
	allowWrite bool
	haproxyBin string
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
	s.mountRPC(mux)
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
			http.Error(w, "host no permitido", http.StatusForbidden)
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

// saved describe un archivo guardado.
type saved struct{ Path, Backup string }

// putOutcome es el resultado de guardar. Un guardado rechazado por la
// validación no es un fallo del servidor: lleva el motivo en Err y, según quién
// lo rechazó, la salida de haproxy -c (Check) o el error del detector (Issue).
type putOutcome struct {
	Saved []saved
	Err   string
	Check *checkResult
	Issue *core.Issue
	Path  string // archivo del error del detector
}

// errClient es un error de la petición (no del servidor).
type errClient string

func (e errClient) Error() string { return string(e) }

// put guarda uno o varios archivos de la configuración servida. Se validan
// juntos (la configuración completa, con todos los cambios aplicados) y se
// guardan todo o nada: un cambio que toca dos archivos a la vez, como renombrar
// un backend y sus referencias, no puede quedar a medias. Valida con `haproxy
// -c` o, si no está instalado, con el detector de haro. Escribe de forma
// atómica, deja una copia .bak de cada archivo y, si una escritura falla,
// restaura los ya escritos.
func (s *server) put(ctx context.Context, changes []core.Source) (putOutcome, error) {
	var out putOutcome
	if !s.allowWrite {
		return out, errDenied
	}
	srcs, err := readSources(s.paths)
	if err != nil {
		return out, err
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
			return out, errClient("ese archivo no forma parte de la configuración servida")
		}
		srcs[idx].Text = c.Text
		touched[idx] = true
	}

	res, err := s.check(ctx, srcs)
	if err != nil {
		return out, err
	}
	out.Check = &res
	if res.Available && !res.OK {
		out.Err = "haproxy -c rechazó la configuración; no se guardó"
		return out, nil
	}
	if !res.Available {
		// Sin haproxy instalado, el detector de haro es la única barrera.
		for _, i := range core.Lint(core.NewConfig(srcs).Merge()) {
			if i.Severity == core.SevError {
				i := i
				out.Err = "la configuración tiene errores y no hay haproxy para validarla; no se guardó"
				out.Issue, out.Path = &i, srcs[i.File].Path
				return out, nil
			}
		}
	}

	var written []int
	for i := range srcs {
		if !touched[i] {
			continue
		}
		if err := writeFile(srcs[i].Path, srcs[i].Text, true); err != nil {
			for _, j := range written { // deshacer lo ya escrito
				_ = writeFile(srcs[j].Path, old[j], false)
			}
			return out, err
		}
		written = append(written, i)
		out.Saved = append(out.Saved, saved{srcs[i].Path, srcs[i].Path + ".bak"})
	}
	return out, nil
}

var errDenied = errors.New("la escritura está desactivada; inicia haro serve con --allow-write")

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
