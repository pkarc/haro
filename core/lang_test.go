package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// flatKeys aplana un JSON a rutas de claves («directives/daemon») y devuelve
// además las rutas cuyo valor es una cadena vacía.
func flatKeys(prefix string, v any, keys *[]string, empty *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			flatKeys(prefix+"/"+k, vv, keys, empty)
		}
	case string:
		*keys = append(*keys, prefix)
		if strings.TrimSpace(x) == "" {
			*empty = append(*empty, prefix)
		}
	default:
		*keys = append(*keys, prefix)
	}
}

func TestKnowledgeTextsSameShape(t *testing.T) {
	load := func(raw []byte) (keys, empty []string) {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		flatKeys("", v, &keys, &empty)
		sort.Strings(keys)
		return
	}
	esKeys, esEmpty := load(textESJSON)
	enKeys, enEmpty := load(textENJSON)
	if len(esKeys) == 0 {
		t.Fatal("text.es.json vacío")
	}
	if len(esEmpty)+len(enEmpty) > 0 {
		t.Errorf("valores vacíos: es=%v en=%v", esEmpty, enEmpty)
	}
	es, en := map[string]bool{}, map[string]bool{}
	for _, k := range esKeys {
		es[k] = true
	}
	for _, k := range enKeys {
		en[k] = true
	}
	for k := range es {
		if !en[k] {
			t.Errorf("falta en text.en.json: %s", k)
		}
	}
	for k := range en {
		if !es[k] {
			t.Errorf("falta en text.es.json: %s", k)
		}
	}
	// Todo lo que el JSON estructural declara debe tener texto.
	for name := range KB.Sections {
		if KB.Text(LangEN).Sections[name].Title == "" {
			t.Errorf("sección sin título en inglés: %s", name)
		}
	}
	if len(KB.Text(LangEN).Categories) != len(KB.Text(LangES).Categories) {
		t.Error("categorías distintas entre idiomas")
	}
}

func TestRulesSameKeys(t *testing.T) {
	es, en := RulesFor(LangES), RulesFor(LangEN)
	if len(es) != len(en) || len(es) == 0 {
		t.Fatalf("reglas distintas: es=%d en=%d", len(es), len(en))
	}
	for id, d := range es {
		if en[id] == "" || d == "" {
			t.Errorf("regla %s sin descripción en algún idioma", id)
		}
	}
	if len(Rules) != len(es) {
		t.Error("Rules debe ser el español")
	}
}

func TestParseLang(t *testing.T) {
	cases := map[string]struct {
		l  Lang
		ok bool
	}{
		"es": {LangES, true}, "en": {LangEN, true}, "en_US.UTF-8": {LangEN, true}, "es-MX": {LangES, true},
		"EN": {LangEN, true}, "es_ES@euro": {LangES, true}, "": {"", false}, "fr": {"", false}, "C": {"", false}, "english": {"", false}, "e": {"", false},
	}
	for in, want := range cases {
		l, ok := ParseLang(in)
		if l != want.l || ok != want.ok {
			t.Errorf("ParseLang(%q) = %q, %v", in, l, ok)
		}
	}
}

func TestLangFromEnv(t *testing.T) {
	set := func(all, msgs, lang string) {
		t.Setenv("LC_ALL", all)
		t.Setenv("LC_MESSAGES", msgs)
		t.Setenv("LANG", lang)
	}
	set("", "", "")
	if LangFromEnv() != LangES {
		t.Error("sin variables debe ser español")
	}
	set("", "", "en_US.UTF-8")
	if LangFromEnv() != LangEN {
		t.Error("LANG=en_US debe ser inglés")
	}
	set("", "es_ES.UTF-8", "en_US.UTF-8")
	if LangFromEnv() != LangES {
		t.Error("LC_MESSAGES tiene prioridad sobre LANG")
	}
	set("en_GB", "es_ES", "es_ES")
	if LangFromEnv() != LangEN {
		t.Error("LC_ALL tiene prioridad")
	}
	set("", "", "C")
	if LangFromEnv() != LangES {
		t.Error("un locale desconocido es español")
	}
	set("", "", "fr_FR.UTF-8")
	if LangFromEnv() != LangES {
		t.Error("otro idioma cae en español")
	}
}

// textCases devuelve los textos de todos los casos de testdata (y del corpus,
// si existe) analizados en el idioma pedido.
func textCases(t *testing.T, lang Lang) map[string]string {
	t.Helper()
	out := map[string]string{}
	add := func(name string, a Analysis) {
		for _, e := range a.Explanations {
			if e.Keyword == "description" {
				continue // texto libre del usuario
			}
			out[name+":"+e.Keyword+":"+e.Args] = e.Explain.Text
		}
		for _, i := range a.Issues {
			out[name+":"+i.Rule+":"+i.Message] = i.Message
		}
	}
	files, _ := filepath.Glob("testdata/*.cfg")
	for _, f := range files {
		src, _ := os.ReadFile(f)
		add(f, AnalyzeLang(string(src), lang))
	}
	for _, dir := range []string{"multi", "multiok"} {
		add(dir, AnalyzeConfigLang(readMulti(t, dir), lang))
	}
	filepath.WalkDir(filepath.Join("testdata", "corpus"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".cfg") {
			b, _ := os.ReadFile(p)
			add(p, AnalyzeLang(string(b), lang))
		}
		return nil
	})
	// Los textos de la base de conocimiento y de las reglas también cuentan.
	for k, v := range flattenText(KB.Text(lang)) {
		out["kb:"+k] = v
	}
	for id, d := range RulesFor(lang) {
		out["rule:"+id] = d
	}
	return out
}

func flattenText(kt *KnowledgeText) map[string]string {
	raw, _ := json.Marshal(kt)
	var v any
	json.Unmarshal(raw, &v)
	out := map[string]string{}
	var walk func(p string, v any)
	walk = func(p string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, vv := range x {
				walk(p+"/"+k, vv)
			}
		case string:
			out[p] = x
		}
	}
	walk("", v)
	return out
}

var reCodeSpan = regexp.MustCompile("`[^`]*`")
var reQuoted = regexp.MustCompile(`[“«][^”»]*[”»]`)
var reWordSep = regexp.MustCompile(`[^\p{L}\p{N}_\-]+`)

// words devuelve el texto en minúsculas, sin código ni nombres citados del
// usuario («x», “x”), con palabras separadas
// por un espacio y rodeado de espacios, para buscar « palabra ».
func words(s string) string {
	s = reCodeSpan.ReplaceAllString(s, " ")
	s = reQuoted.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "power of two choices", " ") // término técnico de HAProxy
	s = strings.ReplaceAll(s, "**", " ")
	return " " + strings.ToLower(reWordSep.ReplaceAllString(s, " ")) + " "
}

// TestNoLanguageLeftovers busca palabras de un idioma en los textos del otro.
// Es heurístico: ajusta las listas si aparece un falso positivo.
func TestNoLanguageLeftovers(t *testing.T) {
	spanish := []string{" para ", " los ", " las ", " del ", " una ", " cuando ", " que ", " sin ", " con ", " por ", " como ", " si ", " si ", " en ", " de ", " la ", " el ", " se ", " es ", " y ", " o ", " al ", " desde ", " tras ", " hasta ", " ó ", "ñ", "¿", "¡", "ó", "á", "í", "ú", "é", "«", "»"}
	english := []string{" the ", " and ", " with ", " from ", " is ", " are ", " of ", " to ", " if ", " unless ", " for ", " it ", " on ", " at ", " in "}
	check := func(lang Lang, bad []string) {
		for name, text := range textCases(t, lang) {
			w := words(text)
			raw := strings.ToLower(text)
			for _, b := range bad {
				hay := w
				if !strings.HasPrefix(b, " ") {
					hay = raw
				}
				if strings.Contains(hay, b) {
					t.Errorf("[%s] «%s» contiene %q:\n  %s", lang, name, strings.TrimSpace(b), text)
					break
				}
			}
		}
	}
	check(LangEN, spanish)
	check(LangES, english)
}

func TestAnalyzeLangDiffers(t *testing.T) {
	src, _ := os.ReadFile("testdata/example.cfg")
	es, en := AnalyzeLang(string(src), LangES), AnalyzeLang(string(src), LangEN)
	if len(es.Explanations) != len(en.Explanations) || len(es.Issues) != len(en.Issues) {
		t.Fatal("el idioma no debe cambiar la estructura del análisis")
	}
	for i := range es.Explanations {
		a, b := es.Explanations[i], en.Explanations[i]
		if a.Line != b.Line || a.Keyword != b.Keyword || a.Explain.Category != b.Explain.Category || a.Explain.Known != b.Explain.Known {
			t.Errorf("línea %d: estructura distinta entre idiomas", a.Line)
		}
	}
	if es.Explanations[0].Explain.Text == en.Explanations[0].Explain.Text {
		t.Error("los textos deberían diferir")
	}
	if got := Analyze(string(src)); got.Explanations[0].Explain.Text != es.Explanations[0].Explain.Text {
		t.Error("Analyze debe seguir en español")
	}
}

// TestConcurrentLangs ejecuta el análisis en los dos idiomas desde varias
// goroutines; con -race detecta cualquier estado compartido por idioma.
func TestConcurrentLangs(t *testing.T) {
	srcs := readMulti(t, "multi")
	prob, _ := os.ReadFile("testdata/problems.cfg")
	srcs = append(srcs, Source{Path: "problems.cfg", Text: string(prob)})
	render := func(lang Lang) string {
		a := AnalyzeConfigLang(srcs, lang)
		raw, _ := json.Marshal(struct {
			I []Issue
			E []LineExplanation
		}{a.Issues, a.Explanations})
		return string(raw)
	}
	want := map[Lang]string{LangES: render(LangES), LangEN: render(LangEN)}
	if want[LangES] == want[LangEN] {
		t.Fatal("los idiomas deberían diferir")
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		for _, lang := range Langs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					if render(lang) != want[lang] {
						t.Errorf("resultado distinto en %s bajo concurrencia", lang)
						return
					}
					_ = KB.PublicFor(lang)
				}
			}()
		}
	}
	wg.Wait()
}

// TestCoreStaysPortable comprueba que el núcleo compila a WASM y que no
// depende de módulos externos (CLAUDE.md: debe poder compilarse a WASM).
func TestCoreStaysPortable(t *testing.T) {
	if testing.Short() {
		t.Skip("lento")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("sin go en el PATH")
	}
	run := func(env []string, args ...string) string {
		cmd := exec.Command(goBin, args...)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run([]string{"GOOS=js", "GOARCH=wasm"}, "build", "-o", os.DevNull, ".")
	mod := strings.TrimSpace(run(nil, "list", "-m"))
	deps := run(nil, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".")
	for _, d := range strings.Fields(deps) {
		if d != mod+"/core" {
			t.Errorf("el núcleo depende de un paquete externo: %s", d)
		}
	}
}
