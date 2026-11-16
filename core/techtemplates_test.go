package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Parámetros típicos de cada plantilla (los que usaría una persona).
var typicalParams = map[string]map[string]any{
	"proxmox":       {"domain": "pve.example.com", "cert": "/etc/haproxy/certs/pve.pem", "servers": []any{"10.0.0.11:8006", "10.0.0.12:8006"}, "ca_file": "/etc/haproxy/certs/pve-root-ca.pem"},
	"homeassistant": {"domain": "ha.example.com", "cert": "/etc/haproxy/certs/ha.pem", "servers": []any{"10.0.0.20:8123"}},
	"nextcloud":     {"domain": "cloud.example.com", "cert": "/etc/haproxy/certs/cloud.pem", "servers": []any{"10.0.0.10:80"}},
	"jellyfin":      {"domain": "tv.example.com", "cert": "/etc/haproxy/certs/tv.pem", "servers": []any{"10.0.0.30:8096"}},
	"gitea":         {"domain": "git.example.com", "cert": "/etc/haproxy/certs/git.pem", "servers": []any{"10.0.0.40:3000"}, "ssh": true, "ssh_port": 22, "ssh_servers": []any{"10.0.0.40:2222"}},
	"grafana":       {"domain": "graf.example.com", "cert": "/etc/haproxy/certs/graf.pem", "servers": []any{"10.0.0.50:3000"}},
	"vaultwarden":   {"domain": "vault.example.com", "cert": "/etc/haproxy/certs/vault.pem", "servers": []any{"10.0.0.60:80"}},
	"elasticsearch": {"domain": "es.example.com", "cert": "/etc/haproxy/certs/es.pem", "servers": []any{"10.0.0.71:9200", "10.0.0.72:9200", "10.0.0.73:9200"}, "ssl": true, "ca_file": "/etc/haproxy/certs/es-ca.pem"},
	"postgresql":    {"bind": "10.0.0.5:5432", "servers": []any{"10.0.0.10:5432", "10.0.0.11:5432"}},
	"redis":         {"bind": "10.0.0.5:6379", "servers": []any{"10.0.0.20:6379", "10.0.0.21:6379"}, "only_master": true},
	"kubernetes":    {"servers": []any{"10.0.0.81:6443", "10.0.0.82:6443", "10.0.0.83:6443"}},
}

func loadBase(t *testing.T) *Config {
	t.Helper()
	b, err := os.ReadFile("testdata/templates/base.cfg")
	if err != nil {
		t.Fatal(err)
	}
	return NewConfig([]Source{{Path: "haproxy.cfg", Text: string(b)}})
}

func renderOn(t *testing.T, cfg *Config, id string, params map[string]any, lang Lang) (*Config, []Op) {
	t.Helper()
	ops, err := RenderTechTemplate(id, params, cfg, lang)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	out, _, err := Apply(cfg, ops...)
	if err != nil {
		t.Fatalf("%s: Apply: %v", id, err)
	}
	return out, ops
}

func issuesOf(cfg *Config) []Issue { return Lint(cfg.Merge()) }

func TestTechTemplatesList(t *testing.T) {
	es, en := TechTemplates(LangES), TechTemplates(LangEN)
	if len(es) != len(typicalParams) || len(en) != len(es) {
		t.Fatalf("hay %d plantillas y %d casos de prueba", len(es), len(typicalParams))
	}
	for i, tp := range es {
		if _, ok := typicalParams[tp.ID]; !ok {
			t.Errorf("la plantilla %s no tiene parámetros típicos en el test", tp.ID)
		}
		if tp.Name == "" || tp.Description == "" || len(tp.Params) == 0 {
			t.Errorf("%s: metadatos incompletos", tp.ID)
		}
		if en[i].Description == tp.Description {
			t.Errorf("%s: la descripción en inglés es igual a la española", tp.ID)
		}
		if len(en[i].Params) != len(tp.Params) {
			t.Fatalf("%s: distinto número de parámetros por idioma", tp.ID)
		}
		for k, p := range tp.Params {
			if p.Label == en[i].Params[k].Label {
				t.Errorf("%s.%s: la etiqueta en inglés es igual a la española", tp.ID, p.Key)
			}
		}
	}
}

// Cada plantilla, con parámetros típicos, produce una configuración que
// parsea sin errores y sin ningún aviso ni sugerencia del detector.
func TestTechTemplatesLintClean(t *testing.T) {
	for id, params := range typicalParams {
		for _, lang := range Langs {
			t.Run(id+"/"+string(lang), func(t *testing.T) {
				out, _ := renderOn(t, loadBase(t), id, params, lang)
				for _, i := range LintWith(out.Merge(), LintOptions{Lang: lang}) {
					t.Errorf("%s:%d %s [%s] %s\n%s", id, i.Line, i.Severity, i.Rule, i.Message, out.Serialize(0))
				}
			})
		}
	}
}

func TestTechTemplatesOnEmptyConfig(t *testing.T) {
	for id, params := range typicalParams {
		t.Run(id, func(t *testing.T) {
			cfg := NewConfig([]Source{{Path: "haproxy.cfg"}})
			out, _ := renderOn(t, cfg, id, params, LangES)
			text := out.Serialize(0)
			if strings.Count(text, "\nglobal")+btoi(strings.HasPrefix(text, "global")) != 1 {
				t.Errorf("debe haber exactamente un global:\n%s", text)
			}
			if !strings.Contains(text, "defaults") {
				t.Errorf("falta defaults:\n%s", text)
			}
			for _, i := range issuesOf(out) {
				t.Errorf("%s:%d [%s] %s\n%s", id, i.Line, i.Rule, i.Message, text)
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("falta %s; ejecuta go test ./core -update", path)
	}
	if string(want) != got {
		t.Errorf("%s no coincide con el resultado actual; revisa el diff y ejecuta -update si el cambio es intencionado.\n--- obtenido ---\n%s", path, got)
	}
}

// El texto resultante sobre la base fija, en español y en inglés.
func TestTechTemplatesGolden(t *testing.T) {
	for id, params := range typicalParams {
		for _, lang := range Langs {
			t.Run(id+"/"+string(lang), func(t *testing.T) {
				out, _ := renderOn(t, loadBase(t), id, params, lang)
				name := "testdata/templates/" + id + ".golden"
				if lang == LangEN {
					name += ".en"
				}
				checkGolden(t, name+".cfg", out.Serialize(0))
			})
		}
	}
}

func TestTechTemplatesAddTwiceAvoidsCollisions(t *testing.T) {
	for _, id := range []string{"nextcloud", "postgresql"} {
		t.Run(id, func(t *testing.T) {
			cfg := loadBase(t)
			p := typicalParams[id]
			out, _ := renderOn(t, cfg, id, p, LangES)
			out, _ = renderOn(t, out, id, p, LangES)
			text := out.Serialize(0)
			if !strings.Contains(text, "backend "+id+"_2") {
				t.Errorf("la segunda copia debe llamarse %s_2:\n%s", id, text)
			}
			if strings.Count(text, "\nglobal")+btoi(strings.HasPrefix(text, "global")) != 1 || strings.Count(text, "\ndefaults") != 1 {
				t.Errorf("global y defaults no deben duplicarse:\n%s", text)
			}
			seen := map[string]bool{}
			for _, s := range out.Merge().Sections {
				if s.Name == "" {
					continue
				}
				if seen[s.Name] {
					t.Errorf("nombre repetido: %s", s.Name)
				}
				seen[s.Name] = true
			}
			for _, i := range Lint(out.Merge()) {
				if i.Severity == SevError {
					t.Errorf("[%s] %s", i.Rule, i.Message)
				}
			}
		})
	}
}

func TestTechTemplatesNameCollisionWithExisting(t *testing.T) {
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: "global\n    daemon\n\ndefaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nbackend nextcloud\n    server s 127.0.0.1:1 check\n\nfrontend nextcloud_https\n    bind 127.0.0.1:81\n    default_backend nextcloud\n"}})
	out, _ := renderOn(t, cfg, "nextcloud", typicalParams["nextcloud"], LangES)
	text := out.Serialize(0)
	for _, want := range []string{"backend nextcloud_2", "frontend nextcloud_2_http", "frontend nextcloud_2_https"} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q:\n%s", want, text)
		}
	}
	// Un frontend con el nombre de un backend también cuenta como colisión (HAProxy 3.3+).
	cfg = NewConfig([]Source{{Path: "a.cfg", Text: "frontend postgresql\n    bind 127.0.0.1:1\n"}})
	out, _ = renderOn(t, cfg, "postgresql", typicalParams["postgresql"], LangES)
	if strings.Contains(out.Serialize(0), "backend postgresql\n") {
		t.Errorf("el backend no puede llamarse como un frontend existente:\n%s", out.Serialize(0))
	}
}

func TestTechTemplatesReuseGlobalAndDefaultsAcrossFiles(t *testing.T) {
	cfg := NewConfig([]Source{
		{Path: "00-global.cfg", Text: "global\n    daemon\n"},
		{Path: "10-defaults.cfg", Text: "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n"},
		{Path: "20-apps.cfg", Text: "backend legacy\n    server s 127.0.0.1:1 check\n"},
	})
	ops, err := RenderTechTemplate("nextcloud", typicalParams["nextcloud"], cfg, LangES)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Op == OpAddSection && (op.Type == "global" || op.Type == "defaults") {
			t.Errorf("no debe añadir %s: ya existe", op.Type)
		}
	}
	_, changes, err := Apply(cfg, ops...)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "20-apps.cfg" {
		t.Errorf("solo debe cambiar el último archivo: %+v", changes)
	}
}

// Global presente y defaults ausente: el defaults nuevo va detrás del global,
// aunque esté en otro archivo.
func TestTechTemplatesAddsOnlyMissingDefaults(t *testing.T) {
	cfg := NewConfig([]Source{
		{Path: "a.cfg", Text: "frontend f\n    bind 127.0.0.1:1\n    default_backend b\n\nbackend b\n    server s 127.0.0.1:1\n"},
		{Path: "b.cfg", Text: "global\n    daemon\n\nbackend c\n    server s 127.0.0.1:1\n"},
	})
	out, _ := renderOn(t, cfg, "nextcloud", typicalParams["nextcloud"], LangES)
	if strings.Contains(out.Serialize(0), "defaults") {
		t.Errorf("defaults no debe ir en a.cfg:\n%s", out.Serialize(0))
	}
	b := out.Serialize(1)
	if !strings.HasPrefix(b, "global\n") || strings.Count(b, "\nglobal") != 0 || strings.Index(b, "defaults") < strings.Index(b, "global") || strings.Index(b, "defaults") > strings.Index(b, "backend c") {
		t.Errorf("defaults debe ir justo detrás del global en b.cfg:\n%s", b)
	}
}

func TestTechTemplatesTimeoutsWhenDefaultsLackThem(t *testing.T) {
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: "global\n    daemon\n\ndefaults\n    mode http\n    option httplog\n"}})
	out, _ := renderOn(t, cfg, "nextcloud", typicalParams["nextcloud"], LangES)
	text := out.Serialize(0)
	for _, want := range []string{"timeout client 30s", "timeout connect 5s", "timeout server 10m"} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "timeout server") != 1 {
		t.Errorf("el timeout server de la plantilla no debe duplicarse:\n%s", text)
	}
	for _, i := range Lint(out.Merge()) {
		if i.Rule == "timeout-missing" {
			t.Errorf("sigue faltando un timeout: %s\n%s", i.Message, text)
		}
	}
}

// Con un frontend TLS en el 443 ya existente (como example.cfg), una segunda
// aplicación se une a él en vez de abrir otro bind en el mismo puerto.
func TestTechTemplatesJoinExistingHTTPS(t *testing.T) {
	b, err := os.ReadFile("testdata/example.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig([]Source{{Path: "example.cfg", Text: string(b)}})
	out, ops := renderOn(t, cfg, "nextcloud", typicalParams["nextcloud"], LangES)
	text := out.Serialize(0)
	for _, op := range ops {
		if op.Op == OpAddSection && op.Type == "frontend" {
			t.Errorf("no debe crear frontends: %+v", op)
		}
	}
	for _, want := range []string{
		"bind *:443 ssl crt /etc/haproxy/certs/sitio.pem crt /etc/haproxy/certs/cloud.pem alpn h2,http/1.1",
		"    acl host_nextcloud hdr(host),field(1,:) -i cloud.example.com\n    use_backend nextcloud if host_nextcloud\n    default_backend web_servers",
		"backend nextcloud\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "bind *:443") != 1 || strings.Count(text, "bind *:80") != 1 {
		t.Errorf("no debe haber binds duplicados:\n%s", text)
	}
	if strings.Contains(text, "redirect scheme https code 301 if { hdr") {
		t.Errorf("http_in ya redirige todo: no hace falta una redirección por host:\n%s", text)
	}
	// El resto del archivo no cambia.
	if d := UnifiedDiff("example.cfg", string(b), text, 0); strings.Count(d, "\n-") > 2 {
		t.Errorf("el diff toca demasiadas líneas:\n%s", d)
	}
	// Un segundo servicio (que ya usa otro certificado) vuelve a unirse y el bind no repite el certificado.
	out2, _ := renderOn(t, out, "grafana", typicalParams["grafana"], LangES)
	if n := strings.Count(out2.Serialize(0), "bind *:443"); n != 1 {
		t.Errorf("bind *:443 aparece %d veces", n)
	}
	for _, i := range Lint(out2.Merge()) {
		if i.Severity == SevError {
			t.Errorf("[%s] %s", i.Rule, i.Message)
		}
	}
}

func TestTechTemplatesJoinHTTPWithoutRedirect(t *testing.T) {
	cfg := NewConfig([]Source{{Path: "a.cfg", Text: "global\n    daemon\n\ndefaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend web\n    bind *:80\n    default_backend x\n\nbackend x\n    server s 127.0.0.1:1 check\n"}})
	out, _ := renderOn(t, cfg, "nextcloud", typicalParams["nextcloud"], LangES)
	text := out.Serialize(0)
	if !strings.Contains(text, "http-request redirect scheme https code 301 if { hdr(host),field(1,:) -i cloud.example.com }") {
		t.Errorf("falta la redirección por dominio en el frontend del 80:\n%s", text)
	}
	if strings.Contains(text, "frontend nextcloud_http\n") || !strings.Contains(text, "frontend nextcloud_https") {
		t.Errorf("solo debe crear el frontend del 443:\n%s", text)
	}
}

func TestTechTemplatesNoVerifyWarnsOnlyWithoutCA(t *testing.T) {
	p := map[string]any{}
	for k, v := range typicalParams["proxmox"] {
		p[k] = v
	}
	delete(p, "ca_file")
	out, _ := renderOn(t, loadBase(t), "proxmox", p, LangES)
	var rules []string
	for _, i := range Lint(out.Merge()) {
		rules = append(rules, i.Rule)
	}
	if len(rules) == 0 {
		t.Error("sin CA debe avisar de tls-no-verify")
	}
	for _, r := range rules {
		if r != "tls-no-verify" {
			t.Errorf("sin CA debe avisar solo de tls-no-verify: %v", rules)
		}
	}
}

func TestTechTemplatesOptionalSections(t *testing.T) {
	p := map[string]any{}
	for k, v := range typicalParams["gitea"] {
		p[k] = v
	}
	p["ssh"] = false
	delete(p, "ssh_servers")
	out, _ := renderOn(t, loadBase(t), "gitea", p, LangES)
	if strings.Contains(out.Serialize(0), "_ssh") || strings.Contains(out.Serialize(0), "mode tcp") {
		t.Errorf("sin ssh no debe haber secciones TCP:\n%s", out.Serialize(0))
	}
	// Con ssh, ssh_servers pasa a ser obligatorio.
	p["ssh"] = true
	if _, err := RenderTechTemplate("gitea", p, loadBase(t), LangES); err == nil || !strings.Contains(err.Error(), "ssh_servers") {
		t.Errorf("debe exigir ssh_servers con ssh: %v", err)
	}
}

func TestTechTemplatesPostgresFailover(t *testing.T) {
	p := map[string]any{"bind": "10.0.0.5:5432", "servers": "10.0.0.10:5432, 10.0.0.11:5432\n10.0.0.12:5432", "failover": "false", "check_user": "hc"}
	out, _ := renderOn(t, loadBase(t), "postgresql", p, LangES)
	text := out.Serialize(0)
	if strings.Contains(text, "backup") || strings.Count(text, "server pg") != 3 || !strings.Contains(text, "option pgsql-check user hc") {
		t.Errorf("sin failover los tres servidores van iguales:\n%s", text)
	}
	delete(p, "failover")
	out, _ = renderOn(t, loadBase(t), "postgresql", p, LangES)
	text = out.Serialize(0)
	if strings.Count(text, " backup") != 2 || strings.Contains(text, "pg1 10.0.0.10:5432 check inter 3s fall 3 rise 2 backup") {
		t.Errorf("por defecto, solo el primero activo:\n%s", text)
	}
}

func TestTechTemplatesParamErrors(t *testing.T) {
	ok := func() map[string]any {
		m := map[string]any{}
		for k, v := range typicalParams["nextcloud"] {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		name   string
		id     string
		mut    func(m map[string]any)
		param  string
		es, en string
	}{
		{"sin dominio", "nextcloud", func(m map[string]any) { delete(m, "domain") }, "domain", "falta el parámetro «domain»", "missing parameter “domain”"},
		{"sin servidores", "nextcloud", func(m map[string]any) { m["servers"] = []any{} }, "servers", "falta el parámetro «servers»", "missing parameter “servers”"},
		{"dominio con espacio", "nextcloud", func(m map[string]any) { m["domain"] = "mi nube.com" }, "domain", "no es un dominio válido", "is not a valid domain"},
		{"dominio con https", "nextcloud", func(m map[string]any) { m["domain"] = "https://x.com" }, "domain", "no es un dominio válido", "is not a valid domain"},
		{"servidor sin puerto", "nextcloud", func(m map[string]any) { m["servers"] = []any{"10.0.0.1"} }, "servers", "no es un servidor válido", "is not a valid server"},
		{"puerto fuera de rango", "nextcloud", func(m map[string]any) { m["servers"] = []any{"10.0.0.1:99999"} }, "servers", "no es un servidor válido", "is not a valid server"},
		{"ipv6", "nextcloud", func(m map[string]any) { m["servers"] = []any{"[::1]:80"} }, "servers", "IPv6", "IPv6"},
		{"cert con espacio", "nextcloud", func(m map[string]any) { m["cert"] = "/etc/mi cert.pem" }, "cert", "no puede estar vacío ni contener espacios", "cannot be empty or contain spaces"},
		{"cert con inyección", "nextcloud", func(m map[string]any) { m["cert"] = "/a.pem\nbackend x" }, "cert", "no puede estar vacío", "cannot be empty"},
		{"tiempo inválido", "nextcloud", func(m map[string]any) { m["upload_timeout"] = "mucho" }, "upload_timeout", "no es un tiempo válido", "is not a valid time"},
		{"bool inválido", "nextcloud", func(m map[string]any) { m["hsts"] = "quizá" }, "hsts", "debe ser verdadero o falso", "must be true or false"},
		{"parámetro desconocido", "nextcloud", func(m map[string]any) { m["dominio"] = "x" }, "dominio", "parámetro desconocido «dominio»", "unknown parameter “dominio”"},
		{"bind inválido", "postgresql", func(m map[string]any) { m["bind"] = "5432" }, "bind", "no es una dirección de escucha válida", "is not a valid listen address"},
		{"int inválido", "gitea", func(m map[string]any) { m["ssh"], m["ssh_port"] = true, 70000 }, "ssh_port", "debe estar entre 1 y 65535", "must be between 1 and 65535"},
		{"lista de otro tipo", "nextcloud", func(m map[string]any) { m["servers"] = 5 }, "servers", "debe ser una lista", "must be a list"},
	}
	for _, c := range cases {
		for _, lang := range Langs {
			t.Run(c.name+"/"+string(lang), func(t *testing.T) {
				p := ok()
				if c.id != "nextcloud" {
					p = map[string]any{}
					for k, v := range typicalParams[c.id] {
						p[k] = v
					}
				}
				c.mut(p)
				_, err := RenderTechTemplate(c.id, p, loadBase(t), lang)
				te, isTE := err.(*TemplateError)
				if !isTE {
					t.Fatalf("debe devolver *TemplateError, devolvió %v", err)
				}
				want := lang.t(c.es, c.en)
				if !strings.Contains(te.Msg, want) || te.Param != c.param {
					t.Errorf("param=%q msg=%q; quería %q en %q", te.Param, te.Msg, want, c.param)
				}
				if lang == LangEN && strings.ContainsAny(te.Msg, "«»áéíóúñ") {
					t.Errorf("restos de español en el mensaje en inglés: %s", te.Msg)
				}
			})
		}
	}
	for _, lang := range Langs {
		_, err := RenderTechTemplate("nope", nil, nil, lang)
		if err == nil || !strings.Contains(err.Error(), lang.t("plantilla desconocida", "unknown template")) {
			t.Errorf("%s: plantilla desconocida: %v", lang, err)
		}
	}
}

// Los parámetros pueden llegar como llegan de JSON, de la CLI (todo texto) o desde Go.
func TestTechTemplatesParamCoercion(t *testing.T) {
	a := map[string]any{"domain": "g.example.com", "cert": "/c.pem", "servers": "10.0.0.1:3000,10.0.0.2:3000", "ssh": "yes", "ssh_port": "2222", "ssh_servers": []string{"10.0.0.1:22"}}
	b := map[string]any{"domain": "g.example.com", "cert": "/c.pem", "servers": []any{"10.0.0.1:3000", "10.0.0.2:3000"}, "ssh": true, "ssh_port": float64(2222), "ssh_servers": []any{"10.0.0.1:22"}}
	oa, _ := renderOn(t, loadBase(t), "gitea", a, LangES)
	ob, _ := renderOn(t, loadBase(t), "gitea", b, LangES)
	if oa.Serialize(0) != ob.Serialize(0) || !strings.Contains(oa.Serialize(0), "bind *:2222") {
		t.Errorf("las dos formas deben dar lo mismo:\n%s\n---\n%s", oa.Serialize(0), ob.Serialize(0))
	}
	if _, err := RenderTechTemplate("gitea", map[string]any{"domain": "g.example.com", "cert": "/c.pem", "servers": []any{"10.0.0.1:3000"}, "ssh_port": 1.5, "ssh": true, "ssh_servers": "a:1"}, loadBase(t), LangES); err == nil {
		t.Error("1.5 no es un puerto")
	}
}

// Las ops devueltas son serializables y reproducibles: aplicarlas dos veces da lo mismo.
func TestTechTemplatesOpsDeterministic(t *testing.T) {
	cfg := loadBase(t)
	a, _ := RenderTechTemplate("redis", typicalParams["redis"], cfg, LangES)
	b, _ := RenderTechTemplate("redis", typicalParams["redis"], cfg, LangES)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatal("ops vacías o distintas")
	}
	for i := range a {
		if a[i].Op != b[i].Op || a[i].Text != b[i].Text || a[i].Name != b[i].Name {
			t.Errorf("op %d distinta", i)
		}
	}
}

// Todo marcador de las plantillas se resuelve (lo valida al cargar, aquí se
// comprueba con el recorrido de los archivos).
func TestTechTemplateFilesMatchIDs(t *testing.T) {
	files, _ := filepath.Glob("knowledge/templates/*.json")
	if len(files) != len(techDefs) {
		t.Errorf("%d archivos y %d plantillas cargadas", len(files), len(techDefs))
	}
}
