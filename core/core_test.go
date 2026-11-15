package core

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Ejecuta `go test ./core -update` para regenerar los .golden.json tras un
// cambio intencionado en las explicaciones o en el detector.
var update = flag.Bool("update", false, "regenera los archivos golden")

type golden struct {
	Issues       []Issue      `json:"issues"`
	Explanations []goldenLine `json:"explanations"`
	Topology     Topology     `json:"topology"`
}

type goldenLine struct {
	Line    int    `json:"line"`
	Keyword string `json:"keyword"`
	Args    string `json:"args"`
	Text    string `json:"text"`
	Known   bool   `json:"known"`
}

// goldenName devuelve la ruta del golden de un caso: español sin sufijo de
// idioma (`x.golden.json`) e inglés con él (`x.golden.en.json`).
func goldenName(base string, lang Lang) string {
	if lang == LangES {
		return base + ".golden.json"
	}
	return base + ".golden." + string(lang) + ".json"
}

func TestGolden(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.cfg")
	if len(files) == 0 {
		t.Fatal("no hay casos en testdata/")
	}
	for _, f := range files {
		for _, lang := range Langs {
			t.Run(filepath.Base(f)+"/"+string(lang), func(t *testing.T) {
				src, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				a := AnalyzeLang(string(src), lang)
				g := golden{Issues: a.Issues, Topology: a.Topology}
				for _, e := range a.Explanations {
					g.Explanations = append(g.Explanations, goldenLine{e.Line, e.Keyword, e.Args, e.Explain.Text, e.Explain.Known})
				}
				got, _ := json.MarshalIndent(g, "", "  ")
				got = append(got, '\n')
				gf := goldenName(strings.TrimSuffix(f, ".cfg"), lang)
				if *update {
					if err := os.WriteFile(gf, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(gf)
				if err != nil {
					t.Fatalf("falta %s; ejecuta go test ./core -update", gf)
				}
				if string(want) != string(got) {
					t.Errorf("%s no coincide con el resultado actual; revisa el diff y ejecuta -update si el cambio es intencionado", gf)
				}
			})
		}
	}
}

// TestGoldenMulti trata cada subdirectorio de testdata como una configuración
// de varios archivos, cargados como lo haría `haproxy -f dir/`.
func TestGoldenMulti(t *testing.T) {
	entries, _ := filepath.Glob("testdata/*")
	found := 0
	for _, dir := range entries {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() || filepath.Base(dir) == "corpus" {
			continue
		}
		found++
		for _, lang := range Langs {
			t.Run(filepath.Base(dir)+"/"+string(lang), func(t *testing.T) {
				names, _ := filepath.Glob(filepath.Join(dir, "*.cfg"))
				sort.Strings(names)
				var srcs []Source
				for _, n := range names {
					if strings.HasPrefix(filepath.Base(n), ".") {
						continue
					}
					b, _ := os.ReadFile(n)
					srcs = append(srcs, Source{Path: filepath.Base(n), Text: string(b)})
				}
				a := AnalyzeConfigLang(srcs, lang)
				type gl struct {
					File string `json:"file"`
					goldenLine
				}
				g := struct {
					Issues       []Issue  `json:"issues"`
					Explanations []gl     `json:"explanations"`
					Topology     Topology `json:"topology"`
				}{Issues: a.Issues, Topology: a.Topology}
				for _, e := range a.Explanations {
					g.Explanations = append(g.Explanations, gl{srcs[e.File].Path, goldenLine{e.Line, e.Keyword, e.Args, e.Explain.Text, e.Explain.Known}})
				}
				got, _ := json.MarshalIndent(g, "", "  ")
				got = append(got, '\n')
				gf := goldenName(filepath.Clean(dir), lang)
				if *update {
					os.WriteFile(gf, got, 0o644)
					return
				}
				want, err := os.ReadFile(gf)
				if err != nil {
					t.Fatalf("falta %s; ejecuta go test ./core -update", gf)
				}
				if string(want) != string(got) {
					t.Errorf("%s no coincide con el resultado actual", gf)
				}
				// Editar a través de la vista unificada modifica el archivo de origen.
				cfg := NewConfig(srcs)
				m := cfg.Merge()
				m.Sections[len(m.Sections)-1].Lines = append(m.Sections[len(m.Sections)-1].Lines, &Line{Kind: KindComment, Text: "# editado"})
				if !strings.Contains(cfg.Serialize(m.Sections[len(m.Sections)-1].File), "# editado") {
					t.Error("la edición en la vista unificada no llegó al archivo de origen")
				}
			})
		}
	}
	if found == 0 {
		t.Fatal("no hay casos de varios archivos en testdata/")
	}
}

func TestRoundTrip(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.cfg")
	more, _ := filepath.Glob("testdata/*/*.cfg")
	for _, f := range append(files, more...) {
		src, _ := os.ReadFile(f)
		if out := Serialize(Parse(string(src))); out != string(src) {
			t.Errorf("%s: Serialize(Parse(x)) != x", f)
		}
	}
}

func TestTokenize(t *testing.T) {
	cases := map[string][]string{
		`http-request set-header X "a b" # c`: {"http-request", "set-header", "X", "a b"},
		`acl x path_beg /a\ b`:                {"acl", "x", "path_beg", "/a b"},
		`log 'x y' local0`:                    {"log", "x y", "local0"},
		`url#frag stays`:                      {"url#frag", "stays"},
	}
	for in, want := range cases {
		got, _ := Tokenize(in)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("Tokenize(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestACLPhraseEN(t *testing.T) {
	cases := map[string]string{
		"path_beg -i /api /v2":         "the path starts with `/api` or `/v2` (case-insensitive)",
		"hdr(host) -i x.com":           "the Host header is `x.com` (case-insensitive)",
		"src 10.0.0.0/8":               "the source IP is in `10.0.0.0/8`",
		"ssl_fc":                       "the incoming connection is TLS",
		"sc_http_req_rate(0) gt 20":    "the client's request rate (counter 0) is greater than **20**",
		"hdr_dom(host) -i example.com": "the Host header contains the domain `example.com` (case-insensitive)",
	}
	for in, want := range cases {
		if got := ACLPhraseLang(strings.Fields(in), LangEN); got != want {
			t.Errorf("ACLPhraseLang(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestACLPhrase(t *testing.T) {
	cases := map[string]string{
		"path_beg -i /api /v2":         "la ruta empieza por `/api` o `/v2` (sin distinguir mayúsculas)",
		"hdr(host) -i x.com":           "el Host es `x.com` (sin distinguir mayúsculas)",
		"src 10.0.0.0/8":               "la IP de origen está en `10.0.0.0/8`",
		"ssl_fc":                       "la conexión entrante es TLS",
		"sc_http_req_rate(0) gt 20":    "el ritmo de peticiones del cliente (contador 0) es mayor que **20**",
		"hdr_dom(host) -i example.com": "el Host contiene el dominio `example.com` (sin distinguir mayúsculas)",
	}
	for in, want := range cases {
		if got := ACLPhrase(strings.Fields(in)); got != want {
			t.Errorf("ACLPhrase(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestEditRoundTrip(t *testing.T) {
	d := Parse("backend a\n    server s1 1.1.1.1:80\nbackend b\n    server s2 2.2.2.2:80")
	d.Sections[0], d.Sections[1] = d.Sections[1], d.Sections[0]
	l, _ := ParseLine("server s3 3.3.3.3:80 check", 0)
	l.IndentSet = false
	d.Sections[0].Lines = append(d.Sections[0].Lines, l)
	EnsureSeparation(d)
	want := "backend b\n    server s2 2.2.2.2:80\n    server s3 3.3.3.3:80 check\n\nbackend a\n    server s1 1.1.1.1:80\n"
	if got := Serialize(d); got != want {
		t.Errorf("edición:\n%q\nquería\n%q", got, want)
	}
}

func TestDiffLines(t *testing.T) {
	ops := DiffLines("a\nb\nc", "a\nx\nc")
	var s []string
	for _, o := range ops {
		s = append(s, o.Op+o.Text)
	}
	if got := strings.Join(s, ","); got != "=a,-b,+x,=c" {
		t.Errorf("DiffLines = %s", got)
	}
}

// readMulti lee un caso de varios archivos de testdata en orden lexicográfico.
func readMulti(t *testing.T, dir string) []Source {
	t.Helper()
	names, _ := filepath.Glob(filepath.Join("testdata", dir, "*.cfg"))
	sort.Strings(names)
	var srcs []Source
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, Source{Path: filepath.Base(n), Text: string(b)})
	}
	return srcs
}

func TestUnifiedDiff(t *testing.T) {
	if d := UnifiedDiff("a.cfg", "a\nb\n", "a\nb\n", 3); d != "" {
		t.Errorf("textos iguales deben dar diff vacío: %q", d)
	}
	before := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"
	after := "1\n2\nTRES\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n"
	want := "--- a.cfg\n+++ a.cfg\n@@ -1,6 +1,6 @@\n 1\n 2\n-3\n+TRES\n 4\n 5\n 6\n@@ -10,3 +10,4 @@\n 10\n 11\n 12\n+13\n"
	if got := UnifiedDiff("a.cfg", before, after, 3); got != want {
		t.Errorf("diff unificado:\n got  %q\n want %q", got, want)
	}
	if got := UnifiedDiff("x", "", "a\n", 3); !strings.Contains(got, "@@ -0,0 +1 @@\n+a\n") {
		t.Errorf("archivo nuevo: %q", got)
	}
}
