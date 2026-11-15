package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ip(i int) *int { return &i }

// applyText aplica operaciones a un único archivo y devuelve su texto final.
func applyText(t *testing.T, text string, ops ...Op) string {
	t.Helper()
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: text}})
	out, _, err := Apply(cfg, ops...)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return out.Serialize(0)
}

const base = "global\n    maxconn 100\n\ndefaults\n    mode http\n\nfrontend web\n    bind *:80\n    default_backend app\n\nbackend app\n    server a1 10.0.0.1:80 check\n"

func TestApplyDirectives(t *testing.T) {
	cases := []struct {
		name string
		ops  []Op
		want string
	}{
		{"add al final de la sección, antes de los blancos",
			[]Op{{Op: OpAddDirective, Section: 3, Text: "server a2 10.0.0.2:80 check"}},
			strings.Replace(base, "check\n", "check\n    server a2 10.0.0.2:80 check\n", 1)},
		{"add en una sección con otra detrás",
			[]Op{{Op: OpAddDirective, Section: 0, Text: "log /dev/log local0"}},
			strings.Replace(base, "maxconn 100\n", "maxconn 100\n    log /dev/log local0\n", 1)},
		{"add en posición concreta",
			[]Op{{Op: OpAddDirective, Section: 2, Text: "mode http", At: ip(0)}},
			"global\n    maxconn 100\n\ndefaults\n    mode http\n\nfrontend web\n    mode http\n    bind *:80\n    default_backend app\n\nbackend app\n    server a1 10.0.0.1:80 check\n"},
		{"update conserva la indentación",
			[]Op{{Op: OpUpdateDirective, Section: 0, LineIndex: 0, Text: "maxconn 2000"}},
			strings.Replace(base, "maxconn 100", "maxconn 2000", 1)},
		{"remove",
			[]Op{{Op: OpRemoveDirective, Section: 2, LineIndex: 1}},
			strings.Replace(base, "    default_backend app\n", "", 1)},
		{"move hacia arriba",
			[]Op{{Op: OpMoveDirective, Section: 2, LineIndex: 1, Delta: -1}},
			strings.Replace(base, "    bind *:80\n    default_backend app\n", "    default_backend app\n    bind *:80\n", 1)},
		{"move en el límite no hace nada",
			[]Op{{Op: OpMoveDirective, Section: 2, LineIndex: 0, Delta: -3}},
			base},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyText(t, base, c.ops...); got != c.want {
				t.Errorf("\n got  %q\n want %q", got, c.want)
			}
		})
	}
}

func TestApplyKeepsIndentStyle(t *testing.T) {
	got := applyText(t, "backend a\n\tserver s1 1.1.1.1:80\n", Op{Op: OpAddDirective, Section: 0, Text: "server s2 2.2.2.2:80"})
	if want := "backend a\n\tserver s1 1.1.1.1:80\n\tserver s2 2.2.2.2:80\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestApplyRejectsSectionHeaders(t *testing.T) {
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: base}})
	for _, text := range []string{"backend nuevo", "frontend x", "global", "a\nb", ""} {
		if _, _, err := Apply(cfg, Op{Op: OpAddDirective, Section: 0, Text: text}); err == nil {
			t.Errorf("add_directive %q debería fallar", text)
		}
		if _, _, err := Apply(cfg, Op{Op: OpUpdateDirective, Section: 0, LineIndex: 0, Text: text}); err == nil {
			t.Errorf("update_directive %q debería fallar", text)
		}
	}
	// Una directiva que se llama como una sección rara sí es válida dentro de una sección.
	if got := applyText(t, base, Op{Op: OpAddDirective, Section: 0, Text: "cache x"}); !strings.Contains(got, "    cache x\n") {
		t.Errorf("cache x no se añadió: %q", got)
	}
}

func TestApplyAddSection(t *testing.T) {
	cases := []struct {
		name string
		src  string
		op   Op
		want string
	}{
		{"backend con plantilla al final",
			base,
			Op{Op: OpAddSection, Type: "backend", Name: "nuevo", Template: true},
			base + "\nbackend nuevo\n    balance roundrobin\n    server srv1 127.0.0.1:8080 check\n"},
		{"sin plantilla solo la cabecera",
			base,
			Op{Op: OpAddSection, Type: "backend", Name: "vacio"},
			base + "\nbackend vacio\n"},
		{"sin salto de línea final",
			"backend a\n    server s 1.1.1.1:80",
			Op{Op: OpAddSection, Type: "backend", Name: "b"},
			"backend a\n    server s 1.1.1.1:80\n\nbackend b"},
		{"frontend usa el primer backend",
			base,
			Op{Op: OpAddSection, Type: "frontend", Name: "f2", Template: true},
			base + "\nfrontend f2\n    bind *:8080\n    mode http\n    default_backend app\n"},
		{"nombre por defecto único",
			"backend nuevo_backend\n    server s 1.1.1.1:80\n",
			Op{Op: OpAddSection, Type: "backend"},
			"backend nuevo_backend\n    server s 1.1.1.1:80\n\nbackend nuevo_backend_2\n"},
		{"global va al principio",
			"defaults\n    mode http\n",
			Op{Op: OpAddSection, Type: "global"},
			"global\n\ndefaults\n    mode http\n"},
		{"defaults va tras global",
			"global\n    maxconn 1\n\nbackend a\n    server s 1.1.1.1:80\n",
			Op{Op: OpAddSection, Type: "defaults"},
			"global\n    maxconn 1\n\ndefaults\n\nbackend a\n    server s 1.1.1.1:80\n"},
		{"en una posición concreta",
			base,
			Op{Op: OpAddSection, Type: "backend", Name: "x", At: ip(3)},
			strings.Replace(base, "\nbackend app\n", "\nbackend x\n\nbackend app\n", 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyText(t, c.src, c.op); got != c.want {
				t.Errorf("\n got  %q\n want %q", got, c.want)
			}
		})
	}
}

func TestApplyAddSectionErrors(t *testing.T) {
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: base}})
	for name, op := range map[string]Op{
		"tipo desconocido":   {Op: OpAddSection, Type: "nada"},
		"nombre repetido":    {Op: OpAddSection, Type: "backend", Name: "app"},
		"listen vs frontend": {Op: OpAddSection, Type: "listen", Name: "web"},
		"segundo global":     {Op: OpAddSection, Type: "global"},
		"global con nombre":  {Op: OpAddSection, Type: "global", Name: "x"},
		"espacios":           {Op: OpAddSection, Type: "backend", Name: "a b"},
		"posición inválida":  {Op: OpAddSection, Type: "backend", Name: "z", At: ip(99)},
	} {
		if _, _, err := Apply(cfg, op); err == nil {
			t.Errorf("%s: debería fallar", name)
		}
	}
	// Un frontend y un backend pueden compartir nombre.
	if _, _, err := Apply(cfg, Op{Op: OpAddSection, Type: "frontend", Name: "app"}); err != nil {
		t.Errorf("frontend app debería ser válido: %v", err)
	}
}

func TestApplyRemoveSectionTakesLeadingComments(t *testing.T) {
	src := "# global\nglobal\n    maxconn 1\n\n# el api\nbackend api\n    server s 1.1.1.1:80\n\n# el web\nbackend web\n    server s 2.2.2.2:80\n"
	if got, want := applyText(t, src, Op{Op: OpRemoveSection, Section: 1}), "# global\nglobal\n    maxconn 1\n\n# el web\nbackend web\n    server s 2.2.2.2:80\n"; got != want {
		t.Errorf("\n got  %q\n want %q", got, want)
	}
	if got, want := applyText(t, src, Op{Op: OpRemoveSection, Section: 2}), "# global\nglobal\n    maxconn 1\n\n# el api\nbackend api\n    server s 1.1.1.1:80\n"; got != want {
		t.Errorf("último:\n got  %q\n want %q", got, want)
	}
	if got, want := applyText(t, src, Op{Op: OpRemoveSection, Section: 0}), "# el api\nbackend api\n    server s 1.1.1.1:80\n\n# el web\nbackend web\n    server s 2.2.2.2:80\n"; got != want {
		t.Errorf("primero:\n got  %q\n want %q", got, want)
	}
}

func TestApplyMoveSection(t *testing.T) {
	src := "backend a\n    server s 1.1.1.1:80\n\n# b\nbackend b\n    server s 2.2.2.2:80\n\nbackend c\n    server s 3.3.3.3:80\n"
	cases := []struct {
		name string
		op   Op
		want string
	}{
		{"b al principio con su comentario", Op{Op: OpMoveSection, Section: 1, To: ip(0)},
			"# b\nbackend b\n    server s 2.2.2.2:80\n\nbackend a\n    server s 1.1.1.1:80\n\nbackend c\n    server s 3.3.3.3:80\n"},
		{"a al final", Op{Op: OpMoveSection, Section: 0, To: ip(2)},
			"# b\nbackend b\n    server s 2.2.2.2:80\n\nbackend c\n    server s 3.3.3.3:80\n\nbackend a\n    server s 1.1.1.1:80\n"},
		{"c al medio", Op{Op: OpMoveSection, Section: 2, To: ip(1)},
			"backend a\n    server s 1.1.1.1:80\n\nbackend c\n    server s 3.3.3.3:80\n\n# b\nbackend b\n    server s 2.2.2.2:80\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyText(t, src, c.op); got != c.want {
				t.Errorf("\n got  %q\n want %q", got, c.want)
			}
		})
	}
	if got := applyText(t, src, Op{Op: OpMoveSection, Section: 1, To: ip(1)}); got != src {
		t.Errorf("mover a la misma posición cambió el texto: %q", got)
	}
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: src}})
	if _, _, err := Apply(cfg, Op{Op: OpMoveSection, Section: 0, To: ip(3)}); err == nil {
		t.Error("destino fuera de rango debería fallar")
	}
}

func TestApplyRenameSection(t *testing.T) {
	src := "frontend web\n    default_backend app\n    use_backend app if x\n    stick-table type ip size 1m\n    http-request track-sc0 src table app\n\nbackend app # nota\n    stick on src table app\n    server s 1.1.1.1:80\n\nbackend other\n    server s 2.2.2.2:80\n"
	t.Run("sin refs solo cambia la cabecera", func(t *testing.T) {
		want := strings.Replace(src, "backend app # nota", "backend nuevo # nota", 1)
		if got := applyText(t, src, Op{Op: OpRenameSection, Section: 1, Name: "nuevo"}); got != want {
			t.Errorf("\n got  %q\n want %q", got, want)
		}
	})
	t.Run("con refs", func(t *testing.T) {
		want := strings.NewReplacer("default_backend app", "default_backend nuevo", "use_backend app", "use_backend nuevo", "table app", "table nuevo", "backend app #", "backend nuevo #").Replace(src)
		if got := applyText(t, src, Op{Op: OpRenameSection, Section: 1, Name: "nuevo", UpdateRefs: true}); got != want {
			t.Errorf("\n got  %q\n want %q", got, want)
		}
	})
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: src}})
	for name, op := range map[string]Op{
		"repetido": {Op: OpRenameSection, Section: 1, Name: "other"},
		"vacío":    {Op: OpRenameSection, Section: 1, Name: ""},
		"espacio":  {Op: OpRenameSection, Section: 1, Name: "a b"},
	} {
		if _, _, err := Apply(cfg, op); err == nil {
			t.Errorf("%s: debería fallar", name)
		}
	}
}

// El caso de varios archivos: renombrar un backend definido en 30-api.cfg
// actualiza la referencia en 20-web.cfg y deja intactos los demás archivos.
func TestApplyRenameAcrossFiles(t *testing.T) {
	srcs := readMulti(t, "multiok")
	cfg := NewConfig(srcs)
	m := cfg.Merge()
	idx := -1
	for i, s := range m.Sections {
		if s.Type == "backend" && s.Name == "api" {
			idx = i
		}
	}
	out, changes, err := Apply(cfg, Op{Op: OpRenameSection, Section: idx, Name: "api_v2", UpdateRefs: true})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range changes {
		paths = append(paths, c.Path)
	}
	if got := strings.Join(paths, ","); got != "20-web.cfg,30-api.cfg" {
		t.Fatalf("archivos modificados: %s", got)
	}
	if !strings.Contains(out.Serialize(1), "use_backend api_v2 if is_api") || !strings.Contains(out.Serialize(2), "backend api_v2") {
		t.Errorf("renombrado incompleto:\n%s\n%s", out.Serialize(1), out.Serialize(2))
	}
	if out.Serialize(0) != cfg.Serialize(0) {
		t.Error("10-global.cfg no debía cambiar")
	}
	if issues := Lint(out.Merge()); len(issues) > 0 {
		for _, i := range issues {
			t.Errorf("problema tras renombrar: %+v", i)
		}
	}
}

func TestApplyMoveSectionBetweenFiles(t *testing.T) {
	srcs := []Source{
		{Path: "a.cfg", Text: "backend a\n    server s 1.1.1.1:80\n\nbackend b\n    server s 2.2.2.2:80\n"},
		{Path: "b.cfg", Text: "backend c\n    server s 3.3.3.3:80\n"},
	}
	cfg := NewConfig(srcs)
	out, changes, err := Apply(cfg, Op{Op: OpMoveSection, Section: 1, To: ip(2), File: ip(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("esperaba 2 archivos modificados, hay %d", len(changes))
	}
	if got, want := out.Serialize(0), "backend a\n    server s 1.1.1.1:80\n"; got != want {
		t.Errorf("a.cfg:\n got  %q\n want %q", got, want)
	}
	if got, want := out.Serialize(1), "backend c\n    server s 3.3.3.3:80\n\nbackend b\n    server s 2.2.2.2:80\n"; got != want {
		t.Errorf("b.cfg:\n got  %q\n want %q", got, want)
	}
	if out.Files[1].Doc.Sections[1].File != 1 {
		t.Error("la sección movida debe pertenecer al archivo destino")
	}
}

func TestApplyIsAtomicAndPure(t *testing.T) {
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: base}})
	before := cfg.Serialize(0)
	_, changes, err := Apply(cfg,
		Op{Op: OpAddDirective, Section: 0, Text: "log /dev/log local0"},
		Op{Op: OpRemoveSection, Section: 42},
	)
	var ae *ApplyError
	if err == nil || changes != nil {
		t.Fatal("debería fallar sin cambios")
	}
	if e, ok := err.(*ApplyError); ok {
		ae = e
	}
	if ae == nil || ae.Index != 1 {
		t.Errorf("el error debe señalar la segunda operación: %v", err)
	}
	if cfg.Serialize(0) != before {
		t.Error("Apply modificó la configuración de entrada")
	}
}

func TestApplyUntouchedFilesNotReported(t *testing.T) {
	srcs := readMulti(t, "multiok")
	cfg := NewConfig(srcs)
	_, changes, err := Apply(cfg, Op{Op: OpAddDirective, Section: 0, Text: "log /dev/log local1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "10-global.cfg" {
		t.Errorf("solo debía cambiar 10-global.cfg: %+v", changes)
	}
}

func TestOpJSON(t *testing.T) {
	var ops []Op
	in := `[{"op":"update_directive","section":2,"lineIndex":1,"text":"x"},{"op":"move_section","section":0,"to":2,"file":1}]`
	if err := json.Unmarshal([]byte(in), &ops); err != nil {
		t.Fatal(err)
	}
	if ops[0].LineIndex != 1 || ops[1].To == nil || *ops[1].To != 2 || *ops[1].File != 1 {
		t.Errorf("JSON mal interpretado: %+v", ops)
	}
}

func TestTokenSpans(t *testing.T) {
	for _, in := range []string{
		`http-request set-header X "a b" # c`,
		`acl x path_beg /a\ b`,
		`log 'x y' local0`,
		`url#frag stays`,
		`a "" b`,
		`  espacios   varios  `,
		`ñandú "año nuevo" x`,
	} {
		toks, _ := Tokenize(in)
		spans := TokenSpans(in)
		if len(toks) != len(spans) {
			t.Errorf("%q: %d tokens y %d spans", in, len(toks), len(spans))
			continue
		}
		for i, sp := range spans {
			if sp.Value != toks[i] {
				t.Errorf("%q: span %d = %q, token %q", in, i, sp.Value, toks[i])
			}
			if sp.Start >= sp.End && sp.Value != "" {
				t.Errorf("%q: span %d vacío", in, i)
			}
		}
	}
	in := `server "a b" 1.1.1.1:80`
	sp := TokenSpans(in)
	if got := in[sp[1].Start:sp[1].End]; got != `"a b"` {
		t.Errorf("el span debe incluir las comillas: %q", got)
	}
}

// Las operaciones inversas deben devolver exactamente el texto original: es la
// mejor prueba de que no se tocan líneas ajenas. Se ejecuta sobre testdata y,
// si existe, sobre el corpus de configuraciones reales.
func TestApplyInverseOperations(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.cfg")
	more, _ := filepath.Glob("testdata/corpus/*/*.cfg")
	deep, _ := filepath.Glob("testdata/corpus/*/*/*.cfg")
	files = append(append(files, more...), deep...)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		orig := Serialize(Parse(string(raw))) // normaliza espacios finales y \r
		cfg := NewConfig([]Source{{Path: f, Text: orig}})
		n := len(cfg.Merge().Sections)

		check := func(name string, ops ...Op) {
			out, _, err := Apply(cfg, ops...)
			if err != nil {
				t.Errorf("%s %s: %v", f, name, err)
				return
			}
			if got := out.Serialize(0); got != orig {
				t.Errorf("%s %s: el texto cambió\n%s", f, name, firstDiff(orig, got))
			}
		}
		check("add+remove sección", Op{Op: OpAddSection, Type: "backend", Name: "zz_haro", Template: true}, Op{Op: OpRemoveSection, Section: n})
		if n == 0 {
			continue
		}
		// Añadir ante líneas sueltas fuera de sección obliga a separarlas con un
		// blanco que al quitar la sección no se retira: no es una inversa exacta.
		pre := explode(cfg.Files[0].Doc).pre
		cleanStart := len(pre) == 0 || endsBlank(pre)
		if cleanStart {
			check("add+remove sección al principio", Op{Op: OpAddSection, Type: "backend", Name: "zz_haro", At: ip(0)}, Op{Op: OpRemoveSection, Section: 0})
		}
		check("add+remove directiva", Op{Op: OpAddDirective, Section: n - 1, Text: "# zz"}, Op{Op: OpRemoveDirective, Section: n - 1, LineIndex: lastIndexOf(cfg, n-1, "# zz")})
		if n > 1 && cleanStart {
			check("mover y volver", Op{Op: OpMoveSection, Section: 0, To: ip(n - 1)}, Op{Op: OpMoveSection, Section: n - 1, To: ip(0)})
			check("mover y volver (medio)", Op{Op: OpMoveSection, Section: n - 1, To: ip(0)}, Op{Op: OpMoveSection, Section: 0, To: ip(n - 1)})
		}
		// Renombrar y volver, sin tocar referencias.
		for i, s := range cfg.Merge().Sections {
			if s.Name != "" && validName(s.Name) == nil && KB.Sections[s.Type].Named == "required" {
				check("renombrar y volver", Op{Op: OpRenameSection, Section: i, Name: "zz_haro"}, Op{Op: OpRenameSection, Section: i, Name: s.Name})
				break
			}
		}
	}
}

func lastIndexOf(cfg *Config, section int, text string) int {
	// Se localiza aplicando la operación y buscando el comentario.
	out, _, _ := Apply(cfg, Op{Op: OpAddDirective, Section: section, Text: text})
	for i, l := range out.Merge().Sections[section].Lines {
		if l.Kind == KindComment && l.Text == text {
			return i
		}
	}
	return 0
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("línea %d:\n  antes:   %q\n  después: %q", i+1, al[i], bl[i])
		}
	}
	return fmt.Sprintf("longitud distinta: %d vs %d líneas", len(al), len(bl))
}
