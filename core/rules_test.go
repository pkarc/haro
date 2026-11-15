package core

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	ok := map[string]Version{
		"2.8": {2, 8}, "2.8.3": {2, 8}, "v3.0": {3, 0}, "V3.1": {3, 1}, " 2.10 ": {2, 10}, "3.0-dev5": {3, 0}, "2.9.0-1": {2, 9},
	}
	for in, want := range ok {
		if got, good := ParseVersion(in); !good || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; quiero %v", in, got, good, want)
		}
	}
	for _, in := range []string{"", "2", "x.y", "2.x", "-1.0", "latest"} {
		if _, good := ParseVersion(in); good {
			t.Errorf("ParseVersion(%q) debería fallar", in)
		}
	}
	a, _ := ParseVersion("2.9")
	b, _ := ParseVersion("2.10")
	if !a.Less(b) || b.Less(a) || a.Less(a) {
		t.Error("la comparación debe ser numérica: 2.9 < 2.10")
	}
	if c, _ := ParseVersion("3.0"); !b.Less(c) {
		t.Error("2.10 < 3.0")
	}
}

func TestSinceFormat(t *testing.T) {
	re := regexp.MustCompile(`^\d+\.\d+$`)
	n := 0
	for name, d := range KB.Directives {
		if d.Since != "" {
			n++
			if !re.MatchString(d.Since) {
				t.Errorf("directiva %s: since %q no tiene formato A.B", name, d.Since)
			}
		}
	}
	for name, v := range KB.OptionSince {
		if !re.MatchString(v) {
			t.Errorf("option %s: since %q no tiene formato A.B", name, v)
		}
		if _, ok := KB.Text(LangEN).Options[name]; !ok {
			t.Errorf("optionSince de una opción desconocida: %s", name)
		}
	}
	for name, s := range KB.Sections {
		if s.Since != "" && !re.MatchString(s.Since) {
			t.Errorf("sección %s: since %q no tiene formato A.B", name, s.Since)
		}
	}
	if n == 0 {
		t.Error("ninguna directiva tiene since")
	}
}

func rulesOf(src string, opts LintOptions) map[string][]Issue {
	out := map[string][]Issue{}
	for _, i := range LintWith(Parse(src), opts) {
		out[i.Rule] = append(out[i.Rule], i)
	}
	return out
}

func TestNewerThanTarget(t *testing.T) {
	src := "global\n    thread-groups 2\n    tune.bufsize 16384\ndefaults\n    mode http\n    option disable-h2-upgrade\n    timeout client 1s\n    timeout connect 1s\n    timeout server 1s\nlog-forward x\n    dgram-bind :514\n"
	// sin versión objetivo no hace nada
	if got := rulesOf(src, LintOptions{Lang: LangES})["newer-than-target"]; len(got) != 0 {
		t.Errorf("sin versión objetivo: %v", got)
	}
	// una versión no válida se ignora
	if got := rulesOf(src, LintOptions{HAProxyVersion: "nada"})["newer-than-target"]; len(got) != 0 {
		t.Errorf("versión inválida: %v", got)
	}
	got := rulesOf(src, LintOptions{Lang: LangEN, HAProxyVersion: "v2.4.1"})["newer-than-target"]
	// thread-groups (2.7), dgram-bind (2.3 no, 2.4 >= 2.3), sección log-forward 2.3 no, disable-h2-upgrade 2.4 no
	if len(got) != 1 || !strings.Contains(got[0].Message, "thread-groups") || !strings.Contains(got[0].Message, "2.7") || !strings.Contains(got[0].Message, "2.4") || got[0].Severity != SevWarn || got[0].Line != 2 {
		t.Errorf("2.4: %+v", got)
	}
	got = rulesOf(src, LintOptions{Lang: LangES, HAProxyVersion: "2.2"})["newer-than-target"]
	var msgs []string
	for _, i := range got {
		msgs = append(msgs, i.Message)
	}
	all := strings.Join(msgs, "|")
	for _, want := range []string{"thread-groups", "disable-h2-upgrade", "log-forward", "dgram-bind", "requiere HAProxy ≥ 2.7", "versión objetivo 2.2"} {
		if !strings.Contains(all, want) {
			t.Errorf("2.2: falta %q en %v", want, msgs)
		}
	}
	// 2.10 es posterior a 2.9: con objetivo 2.10 no avisa de nada de 2.x
	if got := rulesOf(src, LintOptions{HAProxyVersion: "2.10"})["newer-than-target"]; len(got) != 0 {
		t.Errorf("2.10: %v", got)
	}
	if got := rulesOf("global\n    cpu-policy performance\n", LintOptions{HAProxyVersion: "3.0"})["newer-than-target"]; len(got) != 1 {
		t.Errorf("cpu-policy en 3.0: %v", got)
	}
	if got := rulesOf("global\n    cpu-policy performance\n", LintOptions{HAProxyVersion: "3.1"})["newer-than-target"]; len(got) != 0 {
		t.Errorf("cpu-policy en 3.1: %v", got)
	}
}

const rulesBase = "defaults\n    mode http\n    timeout connect 1s\n    timeout client 1s\n    timeout server 1s\n"

func hits(rule, src string) int { return len(rulesOf(src, LintOptions{Lang: LangEN})[rule]) }

func TestRuleStatsExposed(t *testing.T) {
	pos := []string{
		"listen s\n    bind *:8404\n    stats enable\n",
		"listen s\n    bind :8404\n    stats uri /\n",
		"frontend s\n    bind 0.0.0.0:8404\n    stats enable\n",
		"listen s\n    bind [::]:8404\n    stats enable\n",
	}
	neg := []string{
		"listen s\n    bind *:8404\n    stats enable\n    stats auth u:Zq8#vL2pX!\n",
		"listen s\n    bind *:8404\n    stats enable\n    stats admin if LOCALHOST\n",
		"listen s\n    bind *:8404\n    stats enable\n    http-request auth unless { http_auth(l) }\n",
		"listen s\n    bind *:8404\n    stats enable\n    acl lan src 10.0.0.0/8\n    http-request deny unless lan\n",
		"listen s\n    bind 127.0.0.1:8404\n    stats enable\n",
		"listen s\n    bind unix@/run/s.sock\n    stats enable\n",
		"listen s\n    bind *:8404\n    server a 1.1.1.1:80\n",
	}
	for _, s := range pos {
		if hits("stats-exposed", rulesBase+s) != 1 {
			t.Errorf("debería avisar: %q", s)
		}
	}
	for _, s := range neg {
		if hits("stats-exposed", rulesBase+s) != 0 {
			t.Errorf("no debería avisar: %q", s)
		}
	}
}

func TestRuleWeakUserlistPassword(t *testing.T) {
	if hits("weak-userlist-password", "userlist a\n    user x insecure-password test\n    user y insecure-password Haproxy\n") != 2 {
		t.Error("contraseñas débiles sin detectar")
	}
	if hits("weak-userlist-password", "userlist a\n    user x insecure-password Zq8#vL2pX!\n    user y password $5$s$h\n") != 0 {
		t.Error("falso positivo")
	}
	// el detector de estadísticas cubre ahora más contraseñas de ejemplo
	if hits("weak-stats-auth", rulesBase+"listen s\n    bind 127.0.0.1:1\n    stats auth admin:haproxy\n") != 1 {
		t.Error("stats auth admin:haproxy")
	}
}

func TestRuleNoForwardfor(t *testing.T) {
	tls := "frontend f\n    bind :443 ssl crt /x.pem alpn h2,http/1.1\n    default_backend b\n"
	be := "backend b\n    server a 1.1.1.1:80 check\n"
	if hits("no-forwardfor", rulesBase+tls+be) != 1 {
		t.Error("debería avisar")
	}
	for name, s := range map[string]string{
		"en el backend":   rulesBase + tls + "backend b\n    option forwardfor\n    server a 1.1.1.1:80 check\n",
		"en el frontend":  rulesBase + strings.Replace(tls, "    default", "    option forwardfor\n    default", 1) + be,
		"en defaults":     strings.Replace(rulesBase, "mode http\n", "mode http\n    option forwardfor\n", 1) + tls + be,
		"desactivado":     rulesBase + tls + "backend b\n    no option forwardfor\n    server a 1.1.1.1:80 check\n",
		"cabecera manual": rulesBase + tls + "backend b\n    http-request set-header X-Forwarded-For %[src]\n    server a 1.1.1.1:80 check\n",
		"sin TLS":         rulesBase + "frontend f\n    bind :80\n    default_backend b\n" + be,
		"sin servidores":  rulesBase + tls + "backend b\n",
		"modo tcp":        "defaults\n    mode tcp\n    timeout connect 1s\n    timeout client 1s\n    timeout server 1s\n" + tls + be,
	} {
		if n := hits("no-forwardfor", s); n != 0 {
			t.Errorf("%s: no debería avisar (%d)", name, n)
		}
	}
}

func TestRuleNoHTTPSRedirect(t *testing.T) {
	both := "frontend f\n    bind :80\n    bind :443 ssl crt /x.pem\n    default_backend b\n"
	if hits("no-https-redirect", rulesBase+both) != 1 {
		t.Error("debería avisar")
	}
	for name, s := range map[string]string{
		"redirect scheme":   "frontend f\n    bind :80\n    bind :443 ssl crt /x.pem\n    http-request redirect scheme https unless { ssl_fc }\n",
		"redirect location": "frontend f\n    bind :80\n    bind :443 ssl crt /x.pem\n    redirect location https://example.com if !{ ssl_fc }\n",
		"solo 80":           "frontend f\n    bind :80\n    default_backend b\n",
		"solo TLS":          "frontend f\n    bind :443 ssl crt /x.pem\n    default_backend b\n",
		"acme":              "frontend f\n    bind :80\n    bind :443 ssl crt /x.pem\n    acl acme path_beg /.well-known/acme-challenge/\n    use_backend acme if acme\n",
	} {
		if n := hits("no-https-redirect", rulesBase+s); n != 0 {
			t.Errorf("%s: no debería avisar", name)
		}
	}
}

func TestRuleInternalPortPublic(t *testing.T) {
	for _, port := range []string{"3306", "5432", "6379", "9200", "27017", "11211"} {
		if hits("internal-port-public", rulesBase+"listen d\n    mode tcp\n    bind *:"+port+"\n") != 1 {
			t.Errorf("puerto %s sin avisar", port)
		}
	}
	for name, s := range map[string]string{
		"loopback":          "listen d\n    bind 127.0.0.1:5432\n",
		"IP privada":        "listen d\n    bind 10.0.0.5:5432\n",
		"ACL por IP":        "listen d\n    bind *:5432\n    acl lan src 10.0.0.0/8\n    tcp-request connection reject unless lan\n",
		"ACL en el backend": "frontend d\n    bind *:9200\n    default_backend es\nbackend es\n    acl lan src 10.0.0.0/8\n    http-request deny unless lan\n",
		"mTLS":              "listen d\n    bind *:5432 ssl crt /x.pem ca-file /ca.pem verify required\n",
		"otro puerto":       "listen d\n    bind *:8080\n",
		"socket":            "listen d\n    bind unix@/run/pg.sock\n",
	} {
		if hits("internal-port-public", rulesBase+s) != 0 {
			t.Errorf("%s: no debería avisar", name)
		}
	}
}

func TestRuleTLSLegacy(t *testing.T) {
	for _, s := range []string{
		"frontend f\n    bind :443 ssl crt /x.pem ssl-min-ver TLSv1.0\n",
		"frontend f\n    bind :443 ssl crt /x.pem ssl-min-ver SSLv3\n",
		"frontend f\n    bind :443 ssl crt /x.pem force-tlsv11\n",
		"backend b\n    server a 1.1.1.1:443 ssl verify required ssl-min-ver TLSv1.1\n",
		"global\n    ssl-default-bind-options ssl-min-ver TLSv1.0\n",
	} {
		if hits("tls-legacy-protocol", s) != 1 {
			t.Errorf("debería avisar: %q", s)
		}
	}
	for _, s := range []string{
		"frontend f\n    bind :443 ssl crt /x.pem ssl-min-ver TLSv1.2\n",
		"frontend f\n    bind :443 ssl crt /x.pem no-tlsv10\n",
		"global\n    ssl-default-bind-options ssl-min-ver TLSv1.3 no-tls-tickets\n",
	} {
		if hits("tls-legacy-protocol", s) != 0 {
			t.Errorf("no debería avisar: %q", s)
		}
	}
}

func TestRuleNoHealthCheck(t *testing.T) {
	if hits("no-health-check", rulesBase+"backend b\n    server a 1.1.1.1:80\n    server b 1.1.1.2:80\n") != 1 {
		t.Error("debería avisar")
	}
	for name, s := range map[string]string{
		"con check":        rulesBase + "backend b\n    server a 1.1.1.1:80 check\n    server b 1.1.1.2:80\n",
		"default-server":   rulesBase + "backend b\n    default-server check\n    server a 1.1.1.1:80\n    server b 1.1.1.2:80\n",
		"en defaults":      strings.Replace(rulesBase, "mode http\n", "mode http\n    default-server check\n", 1) + "backend b\n    server a 1.1.1.1:80\n    server b 1.1.1.2:80\n",
		"un solo servidor": rulesBase + "backend b\n    server a 1.1.1.1:80\n",
		"track":            rulesBase + "backend b\n    server a 1.1.1.1:80 track o/a\n    server b 1.1.1.2:80 track o/b\n",
	} {
		if hits("no-health-check", s) != 0 {
			t.Errorf("%s: no debería avisar", name)
		}
	}
}

func TestEveryRuleHasBothDescriptions(t *testing.T) {
	for id := range RulesFor(LangES) {
		if RulesFor(LangEN)[id] == "" {
			t.Errorf("regla sin descripción en inglés: %s", id)
		}
	}
	for _, id := range []string{"stats-exposed", "weak-userlist-password", "no-forwardfor", "no-https-redirect", "internal-port-public", "tls-legacy-protocol", "no-health-check", "newer-than-target"} {
		if Rules[id] == "" {
			t.Errorf("falta la regla %s", id)
		}
	}
}

func TestEveryDirectiveHasText(t *testing.T) {
	for name := range KB.Directives {
		if _, ok := describers[name]; ok {
			continue
		}
		for _, l := range Langs {
			if KB.Text(l).Directives[name] == "" {
				t.Errorf("directiva sin texto ni describer (%s): %s", l, name)
			}
		}
	}
}
