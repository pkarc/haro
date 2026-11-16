package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Estos tests contrastan los casos de testdata con `haproxy -c`, para comprobar
// que los casos «válidos» lo son de verdad. Buscan el binario en HARO_HAPROXY o
// en el PATH. Sin binario se omiten, salvo que HARO_REQUIRE_HAPROXY=1 (CI).

func haproxyBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("HARO_HAPROXY")
	if bin == "" {
		bin = "haproxy"
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		if os.Getenv("HARO_REQUIRE_HAPROXY") == "1" {
			t.Fatalf("falta el binario de haproxy: %v", err)
		}
		t.Skip("sin haproxy; define HARO_HAPROXY o instálalo")
	}
	return p
}

// openTempDir crea un directorio temporal legible por otros usuarios: cuando
// haproxy corre en un contenedor, lo ejecuta otro usuario que debe poder leerlo
// (t.TempDir lo crea con permisos 0700).
func openTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "haro-haproxy-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// selfSignedPEM escribe un certificado autofirmado con su clave (el formato
// que espera `crt`) y devuelve su ruta.
func selfSignedPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "haro.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"haro.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var b []byte
	b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	b = append(b, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	p := filepath.Join(openTempDir(t), "cert.pem")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// haproxyCheck ejecuta `haproxy -c -f ...` sobre los textos dados (en un
// directorio temporal) y devuelve si fue aceptada y la salida.
func haproxyCheck(t *testing.T, bin string, texts []string) (bool, string) {
	t.Helper()
	return haproxyCheckWith(t, bin, texts, "-c")
}

// haproxyCheckWith es haproxyCheck con las opciones dadas (p. ej. -dW, que
// convierte los avisos en error: con -c salen con código 2).
func haproxyCheckWith(t *testing.T, bin string, texts []string, opts ...string) (bool, string) {
	t.Helper()
	dir := openTempDir(t)
	args := append([]string{}, opts...)
	for i, txt := range texts {
		p := filepath.Join(dir, strings.Repeat("0", 2)+string(rune('0'+i))+".cfg")
		if err := os.WriteFile(p, []byte(txt), 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-f", p)
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	return err == nil, string(out)
}

func TestHAProxyAcceptsValidCases(t *testing.T) {
	bin := haproxyBin(t)
	pemPath := selfSignedPEM(t)
	fix := func(s string) string {
		return strings.NewReplacer("/etc/haproxy/certs/sitio.pem", pemPath).Replace(s)
	}

	t.Run("example.cfg", func(t *testing.T) {
		b, err := os.ReadFile("testdata/example.cfg")
		if err != nil {
			t.Fatal(err)
		}
		if ok, out := haproxyCheck(t, bin, []string{fix(string(b))}); !ok {
			t.Errorf("haproxy -c rechaza example.cfg:\n%s", out)
		}
	})

	load := func(dir string) []string {
		names, _ := filepath.Glob("testdata/" + dir + "/*.cfg")
		sort.Strings(names)
		var texts []string
		for _, n := range names {
			b, _ := os.ReadFile(n)
			texts = append(texts, fix(string(b)))
		}
		return texts
	}

	t.Run("multiok", func(t *testing.T) {
		if ok, out := haproxyCheck(t, bin, load("multiok")); !ok {
			t.Errorf("haproxy -c rechaza testdata/multiok:\n%s", out)
		}
	})

	// testdata/multi es un caso de errores a propósito (nombre repetido entre
	// archivos y una directiva fuera de sección): HAProxy debe rechazarlo.
	t.Run("multi-rechazado", func(t *testing.T) {
		if ok, _ := haproxyCheck(t, bin, load("multi")); ok {
			t.Error("haproxy -c acepta testdata/multi, que contiene errores a propósito")
		}
	})
}

// TestHAProxyAgreesOnProblems compara los errores del detector con el veredicto
// de haproxy en problems.cfg. Solo informa: el detector es orientativo y puede
// ser más estricto que HAProxy (avisos, no errores).
func TestHAProxyAgreesOnProblems(t *testing.T) {
	bin := haproxyBin(t)
	b, err := os.ReadFile("testdata/problems.cfg")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ReplaceAll(string(b), "/etc/haproxy/certs/site.pem", selfSignedPEM(t))
	hasErr := false
	for _, i := range Analyze(src).Issues {
		if i.Severity == SevError {
			hasErr = true
		}
	}
	ok, out := haproxyCheck(t, bin, []string{src})
	if hasErr && ok {
		t.Errorf("el detector reporta errores pero haproxy -c acepta problems.cfg")
	}
	t.Logf("detector con errores=%v, haproxy aceptó=%v\n%s", hasErr, ok, out)
}

// Las plantillas de sección deben producir configuraciones que haproxy acepte.
func TestHAProxyAcceptsTemplates(t *testing.T) {
	bin := haproxyBin(t)
	base := "global\n    log /dev/log local0\n\ndefaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend base_in\n    bind *:80\n    default_backend base\n\nbackend base\n    server s 127.0.0.1:9000\n"
	for typ := range Templates {
		if typ == "global" || typ == "defaults" {
			continue
		}
		t.Run(typ, func(t *testing.T) {
			out, _, err := Apply(NewConfig([]Source{{Path: "a.cfg", Text: base}}), Op{Op: OpAddSection, Type: typ, Template: true})
			if err != nil {
				t.Fatal(err)
			}
			if ok, msg := haproxyCheck(t, bin, []string{out.Serialize(0)}); !ok {
				t.Errorf("haproxy -c rechaza la plantilla %s:\n%s\n%s", typ, out.Serialize(0), msg)
			}
		})
	}
	for _, typ := range []string{"global", "defaults"} {
		t.Run(typ, func(t *testing.T) {
			out, _, err := Apply(NewConfig([]Source{{Path: "a.cfg", Text: "frontend f\n    bind *:80\n    default_backend base\n\nbackend base\n    server s 127.0.0.1:9000\n"}}), Op{Op: OpAddSection, Type: typ, Template: true})
			if err != nil {
				t.Fatal(err)
			}
			if ok, msg := haproxyCheck(t, bin, []string{out.Serialize(0)}); !ok {
				t.Errorf("haproxy -c rechaza la plantilla %s:\n%s\n%s", typ, out.Serialize(0), msg)
			}
		})
	}
}

// Cada plantilla por tecnología, con parámetros típicos, debe pasar haproxy -c
// sin ningún aviso (-dW): sobre una base mínima, sobre una configuración vacía
// y, en las web, uniéndose a un frontend TLS ya existente. Los certificados
// son el .pem autofirmado de la prueba (también como CA de los servidores).
func TestHAProxyAcceptsTechTemplates(t *testing.T) {
	bin := haproxyBin(t)
	pemPath := selfSignedPEM(t)
	fix := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			switch k {
			case "cert", "ca_file":
				v = pemPath
			}
			out[k] = v
		}
		return out
	}
	base, err := os.ReadFile("testdata/templates/base.cfg")
	if err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile("testdata/example.cfg")
	if err != nil {
		t.Fatal(err)
	}
	example = []byte(strings.ReplaceAll(string(example), "/etc/haproxy/certs/sitio.pem", pemPath))

	check := func(t *testing.T, cfg *Config, opts ...string) {
		t.Helper()
		texts := make([]string, len(cfg.Files))
		for i := range cfg.Files {
			texts[i] = cfg.Serialize(i)
		}
		if ok, out := haproxyCheckWith(t, bin, texts, opts...); !ok {
			t.Errorf("haproxy %s rechaza el resultado:\n%s\n%s", strings.Join(opts, " "), strings.Join(texts, "\n--\n"), out)
		}
	}
	strict := func(t *testing.T, cfg *Config) { t.Helper(); check(t, cfg, "-c", "-dW") }
	for _, tpl := range TechTemplates(LangES) {
		p := fix(typicalParams[tpl.ID])
		for _, c := range []struct {
			name string
			text string
		}{{"base", string(base)}, {"vacia", ""}, {"example", string(example)}} {
			t.Run(tpl.ID+"/"+c.name, func(t *testing.T) {
				cfg := NewConfig([]Source{{Path: "haproxy.cfg", Text: c.text}})
				out, _ := renderOn(t, cfg, tpl.ID, p, LangES)
				if c.name == "example" && strings.Contains(out.Serialize(0), "mode tcp") {
					// El defaults de example.cfg tiene option forwardfor, que HAProxy
					// avisa de que ignora en las secciones TCP que lo heredan: es del
					// defaults del usuario, no de la plantilla. Se exige que lo acepte.
					check(t, out, "-c")
					return
				}
				strict(t, out)
			})
		}
	}
	// Variantes de parámetros que cambian las directivas generadas.
	variants := []struct {
		id string
		p  map[string]any
	}{
		{"proxmox", map[string]any{"domain": "pve.example.com", "cert": pemPath, "servers": "10.0.0.11:8006"}},
		{"elasticsearch", map[string]any{"domain": "es.example.com", "cert": pemPath, "servers": "10.0.0.71:9200"}},
		{"elasticsearch", map[string]any{"domain": "es.example.com", "cert": pemPath, "servers": "10.0.0.71:9200", "ssl": true}},
		{"nextcloud", map[string]any{"domain": "c.example.com", "cert": pemPath, "servers": "10.0.0.10:80", "hsts": false, "upload_timeout": "90s"}},
		{"gitea", map[string]any{"domain": "g.example.com", "cert": pemPath, "servers": "10.0.0.40:3000"}},
		{"postgresql", map[string]any{"bind": "10.0.0.5:5432", "servers": "10.0.0.10:5432,10.0.0.11:5432,10.0.0.12:5432", "failover": false}},
		{"redis", map[string]any{"bind": "10.0.0.5:6379", "servers": "10.0.0.20:6379"}},
		{"kubernetes", map[string]any{"servers": "10.0.0.81:6443", "ssl_hello": false, "bind": "10.0.0.5:6443"}},
	}
	for i, v := range variants {
		t.Run("variante/"+v.id+"/"+string(rune('a'+i)), func(t *testing.T) {
			out, _ := renderOn(t, NewConfig([]Source{{Path: "haproxy.cfg", Text: string(base)}}), v.id, v.p, LangEN)
			strict(t, out)
		})
	}
	// Todas las plantillas a la vez en una misma configuración (los frontends
	// web se unen en un único bind 443) y repetidas (nombres _2).
	t.Run("todas-juntas", func(t *testing.T) {
		cfg := NewConfig([]Source{{Path: "haproxy.cfg", Text: string(base)}})
		for round := 0; round < 2; round++ {
			for _, tpl := range TechTemplates(LangES) {
				cfg, _ = renderOn(t, cfg, tpl.ID, fix(typicalParams[tpl.ID]), LangES)
			}
		}
		strict(t, cfg)
	})
	// En varios archivos (global, defaults y secciones repartidos).
	t.Run("varios-archivos", func(t *testing.T) {
		cfg := NewConfig([]Source{
			{Path: "00-global.cfg", Text: "global\n    daemon\n"},
			{Path: "10-defaults.cfg", Text: "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n"},
			{Path: "20-apps.cfg", Text: ""},
		})
		cfg, _ = renderOn(t, cfg, "nextcloud", fix(typicalParams["nextcloud"]), LangES)
		cfg, _ = renderOn(t, cfg, "postgresql", fix(typicalParams["postgresql"]), LangES)
		strict(t, cfg)
	})
}
