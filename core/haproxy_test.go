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
	dir := openTempDir(t)
	args := []string{"-c"}
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
