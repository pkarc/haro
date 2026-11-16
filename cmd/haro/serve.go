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

// The local API is the Connect service defined in proto/haro/v1/haro.proto
// (see rpc.go). The web UI (generated TypeScript client) and any Connect, gRPC
// or HTTP+JSON client talk to it.

type server struct {
	// paths are the --file arguments as given (files or directories).
	// They are expanded on every request to reflect new or deleted files.
	paths      []string
	allowWrite bool
	haproxyBin string
	// assist is the AI assistant; nil if not started with --assistant.
	assist streamer
	// assistModel is the model name, reported in the capabilities.
	assistModel string
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runServe(args []string) error {
	fls := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fls.String("addr", "127.0.0.1:7468", "listen address (localhost only by default, for safety)")
	var files multiFlag
	fls.Var(&files, "file", "configuration file or directory; repeat for several, in haproxy -f order")
	allowWrite := fls.Bool("allow-write", false, "allow saving from the web UI (always validates first and creates a .bak copy)")
	bin := fls.String("haproxy", "haproxy", "haproxy binary used to validate with -c")
	assistant := fls.Bool("assistant", false, "enable the AI assistant: SENDS the configuration to the Anthropic API from this server (key in ANTHROPIC_API_KEY or `ant auth login`; it never reaches the browser)")
	assistModel := fls.String("assistant-model", envOr("HARO_ASSISTANT_MODEL", defaultAssistantModel), "Anthropic model for the assistant")
	fls.Usage = func() {
		fmt.Fprintln(fls.Output(), "Usage: haro serve [options]")
		fls.PrintDefaults()
	}
	fls.Parse(args)

	s := &server{paths: files, allowWrite: *allowWrite}
	if p, err := exec.LookPath(*bin); err == nil {
		s.haproxyBin = p
	}
	if *assistant {
		s.assist, s.assistModel = newAnthropicStreamer(*assistModel), *assistModel
		log.Printf("⚠ assistant enabled: the configuration you send to it will go to the Anthropic API (model %s)", *assistModel)
	}
	if s.allowWrite && len(s.paths) == 0 {
		return errors.New("--allow-write requires at least one --file")
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
		log.Printf("⚠ listening on %s: anyone on the network can view and analyze configurations. There is no authentication.", *addr)
	}

	var handler http.Handler = limitBody(s.routes())
	if loopback {
		handler = localOnly(handler)
	}
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("haro %s listening on http://%s", version, *addr)
	if len(s.paths) > 0 {
		log.Printf("configuration: %s (writing %s)", strings.Join(s.paths, " "), map[bool]string{true: "allowed", false: "disabled"}[s.allowWrite])
	}
	if s.haproxyBin == "" {
		log.Printf("haproxy binary not found: real validation (haproxy -c) will not be available")
	}
	return srv.ListenAndServe()
}

func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	s.mountRPC(mux)
	mux.Handle("/", http.FileServer(http.FS(web.FS())))
	return mux
}

// localOnly rejects requests whose Host is not local, to prevent DNS
// rebinding attacks from web pages open on the same machine.
func localOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "host not allowed", http.StatusForbidden)
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

// check validates with `haproxy -c`, writing each file to a temporary
// directory and passing them with -f in the same order.
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

// saved describes a saved file.
type saved struct{ Path, Backup string }

// putOutcome is the result of saving. A save rejected by validation is not a
// server failure: it carries the reason in Err and, depending on who rejected
// it, the output of haproxy -c (Check) or the detector error (Issue).
type putOutcome struct {
	Saved []saved
	Err   string
	Check *checkResult
	Issue *core.Issue
	Path  string // file of the detector error
}

// errClient is a request error (not a server one).
type errClient string

func (e errClient) Error() string { return string(e) }

// put saves one or more files of the served configuration. They are validated
// together (the full configuration with all changes applied) and saved all or
// nothing: a change touching two files at once, such as renaming a backend and
// its references, cannot be left half done. It validates with `haproxy -c` or,
// if not installed, with haro's detector. It writes atomically, leaves a .bak
// copy of each file and, if a write fails, restores those already written.
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
			return out, errClient("that file is not part of the served configuration")
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
		out.Err = "haproxy -c rejected the configuration; not saved"
		return out, nil
	}
	if !res.Available {
		// Without haproxy installed, haro's detector is the only barrier.
		for _, i := range core.Lint(core.NewConfig(srcs).Merge()) {
			if i.Severity == core.SevError {
				i := i
				out.Err = "the configuration has errors and there is no haproxy to validate it; not saved"
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
			for _, j := range written { // undo what was already written
				_ = writeFile(srcs[j].Path, old[j], false)
			}
			return out, err
		}
		written = append(written, i)
		out.Saved = append(out.Saved, saved{srcs[i].Path, srcs[i].Path + ".bak"})
	}
	return out, nil
}

var errDenied = errors.New("writing is disabled; start haro serve with --allow-write")

// writeFile writes the file atomically (temp + rename), with a .bak copy of
// the previous version if backup is true.
func writeFile(path, text string, backup bool) error {
	mode := os.FileMode(0o640)
	if old, err := os.ReadFile(path); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		if backup {
			if err := os.WriteFile(path+".bak", old, mode); err != nil {
				return fmt.Errorf("could not create the backup copy: %w", err)
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
