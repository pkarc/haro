package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Severity es la gravedad de un problema.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
	SevInfo  Severity = "info"
)

var sevRank = map[Severity]int{SevError: 0, SevWarn: 1, SevInfo: 2}

// Issue es un problema detectado en la configuración.
type Issue struct {
	Rule     string   `json:"rule"`
	File     int      `json:"file"` // índice del archivo dentro del Config
	Severity Severity `json:"severity"`
	Line     int      `json:"line"`
	Section  int      `json:"section"` // -1 si está fuera de secciones
	Message  string   `json:"message"`
}

// Rules documenta cada regla del detector, en español. El identificador es
// estable y sirve para desactivar reglas desde la configuración del usuario o
// en CI. Para otro idioma, usa RulesFor.
var Rules = rulesES

var rulesES = map[string]string{
	"outside-section": "Directiva fuera de cualquier sección",
	"section-name":    "Sección que requiere nombre y no lo tiene",
	"duplicate-name":  "Dos secciones con el mismo nombre",
	"multiple-global": "Más de una sección global",
	"misplaced":       "Directiva que no es válida en ese tipo de sección",
	"unknown-section": "Palabra al inicio de línea que no es una sección conocida",
	"no-bind":         "Frontend o listen sin bind",
	"timeout-missing": "Falta timeout client, connect o server",
	"no-servers":      "Backend sin servidores",
	"no-route":        "Frontend que no envía tráfico a ningún backend",
	"backend-missing": "Referencia a un backend que no existe",
	"mode-mismatch":   "Frontend http enviando a backend tcp",
	"acl-undefined":   "Condición que usa una ACL no definida",
	"weak-stats-auth": "Contraseña de estadísticas que parece de ejemplo",
	"tls-no-verify":   "Servidor TLS sin verificar el certificado",
	"tls-no-alpn":     "Bind TLS en modo http sin ALPN (sin HTTP/2)",
	"no-global":       "No hay sección global",

	"stats-exposed":          "Estadísticas en una dirección pública sin autenticación ni restricción por IP",
	"weak-userlist-password": "Contraseña de usuario (userlist) de ejemplo o débil",
	"no-forwardfor":          "Backend HTTP sin option forwardfor (los servidores no ven la IP del cliente)",
	"no-https-redirect":      "Escucha en el puerto 80 y en TLS sin redirigir a HTTPS",
	"internal-port-public":   "Puerto de un servicio interno (base de datos, caché…) abierto a todas las interfaces",
	"tls-legacy-protocol":    "Protocolo TLS obsoleto permitido (SSLv3, TLS 1.0 o 1.1)",
	"no-health-check":        "Backend con varios servidores y ninguno con check",
	"newer-than-target":      "Directiva más reciente que la versión de HAProxy objetivo (--haproxy-version)",
}

var rulesEN = map[string]string{
	"outside-section": "Directive outside any section",
	"section-name":    "Section that requires a name and has none",
	"duplicate-name":  "Two sections with the same name",
	"multiple-global": "More than one global section",
	"misplaced":       "Directive that is not valid in that section type",
	"unknown-section": "Word at the start of a line that is not a known section",
	"no-bind":         "Frontend or listen without a bind",
	"timeout-missing": "Missing timeout client, connect or server",
	"no-servers":      "Backend without servers",
	"no-route":        "Frontend that sends traffic to no backend",
	"backend-missing": "Reference to a backend that does not exist",
	"mode-mismatch":   "HTTP frontend sending to a TCP backend",
	"acl-undefined":   "Condition that uses an undefined ACL",
	"weak-stats-auth": "Statistics password that looks like a placeholder",
	"tls-no-verify":   "TLS server without certificate verification",
	"tls-no-alpn":     "TLS bind in http mode without ALPN (no HTTP/2)",
	"no-global":       "There is no global section",

	"stats-exposed":          "Statistics on a public address with no authentication or IP restriction",
	"weak-userlist-password": "Placeholder or weak userlist password",
	"no-forwardfor":          "HTTP backend without option forwardfor (servers do not see the client IP)",
	"no-https-redirect":      "Listens on port 80 and on TLS without redirecting to HTTPS",
	"internal-port-public":   "Port of an internal service (database, cache…) open on all interfaces",
	"tls-legacy-protocol":    "Obsolete TLS protocol allowed (SSLv3, TLS 1.0 or 1.1)",
	"no-health-check":        "Backend with several servers and none with check",
	"newer-than-target":      "Directive newer than the target HAProxy version (--haproxy-version)",
}

// RulesFor devuelve las descripciones de las reglas en el idioma pedido.
func RulesFor(lang Lang) map[string]string {
	if lang == LangEN {
		return rulesEN
	}
	return rulesES
}

var reGlobalPrefix = regexp.MustCompile(`^(tune\.|ssl-|quic|httpclient|h1-|h2-|ocsp)`)
var reWeakPass = regexp.MustCompile(`(?i):(admin|password|123456|cambiame|changeme|cambiar|secret|test|haproxy|root|1234|pass|qwerty|letmein)$`)

// Lint analiza el documento y devuelve los problemas ordenados por línea,
// con los mensajes en español. Es orientativo: la validación definitiva es
// `haproxy -c`.
func Lint(doc *Doc) []Issue { return LintLang(doc, LangES) }

// LintLang es Lint con los mensajes en el idioma pedido.
func LintLang(doc *Doc, lang Lang) []Issue { return LintWith(doc, LintOptions{Lang: lang}) }

// LintOptions son las opciones de LintWith.
type LintOptions struct {
	Lang Lang
	// HAProxyVersion es la versión objetivo («2.8», «v3.0», «2.8.3»). Si no
	// está vacía y es válida, se avisa de lo que requiera una versión
	// posterior (regla newer-than-target).
	HAProxyVersion string
}

// LintWith es Lint con opciones: idioma y versión objetivo de HAProxy.
func LintWith(doc *Doc, opts LintOptions) []Issue {
	lang := opts.Lang
	issues := []Issue{}
	file := 0
	add := func(rule string, sev Severity, line, sec int, es, en string, a ...any) {
		issues = append(issues, Issue{Rule: rule, File: file, Severity: sev, Line: line, Section: sec, Message: fmt.Sprintf(lang.t(es, en), a...)})
	}
	for _, l := range doc.Pre {
		if l.Kind == KindDirective {
			file = l.File
			add("outside-section", SevError, l.Line, -1, "«%s» está fuera de cualquier sección. Toda directiva debe ir dentro de global, defaults, frontend, backend…", "“%s” is outside any section. Every directive must go inside global, defaults, frontend, backend…", l.Keyword)
		}
	}
	eff := ComputeEffective(doc)
	type pos struct{ file, line int }
	seen := map[string]pos{}
	targets := map[string]int{}
	globals := 0
	for i, s := range doc.Sections {
		file = s.File
		info := Sections[s.Type]
		if s.Type == "global" {
			globals++
			if globals == 2 {
				add("multiple-global", SevWarn, s.Line, i, "Hay más de una sección global; HAProxy las combina, pero es mejor tener una sola.", "There is more than one global section; HAProxy merges them, but it is better to have just one.")
			}
		}
		if info.Named == "required" && s.Name == "" {
			add("section-name", SevError, s.Line, i, "La sección %s necesita un nombre.", "The %s section needs a name.", s.Type)
		}
		if s.Name != "" {
			keys := []string{s.Type}
			if s.Type == "listen" {
				keys = []string{"frontend", "backend"}
			}
			for _, k := range keys {
				key := k + ":" + s.Name
				if prev, ok := seen[key]; ok {
					where := fmt.Sprintf(lang.t("línea %d", "line %d"), prev.line)
					if prev.file != s.File {
						where = fmt.Sprintf(lang.t("%s, línea %d", "%s, line %d"), fileLabel(doc, prev.file, lang), prev.line)
					}
					add("duplicate-name", SevError, s.Line, i, "Ya existe otra sección con capacidad de %s llamada «%s» (%s).", "Another section acting as a %s named “%s” already exists (%s).", k, s.Name, where)
				} else {
					seen[key] = pos{s.File, s.Line}
				}
			}
		}
		if (s.Type == "backend" || s.Type == "listen") && s.Name != "" {
			targets[s.Name] = i
		}
	}

	for i, s := range doc.Sections {
		file = s.File
		dirs := s.Directives()
		e := eff[i]
		checkPlacement := proxySections[s.Type]
		for _, l := range dirs {
			d, known := KB.Directives[l.Keyword]
			if known && checkPlacement && !d.AllowedIn(s.Type) {
				add("misplaced", SevWarn, l.Line, i, "«%s» no es válida en una sección %s.", "“%s” is not valid in a %s section.", l.Keyword, s.Type)
			}
			if !known && l.IndentSet && l.Indent == "" && !reGlobalPrefix.MatchString(l.Keyword) {
				add("unknown-section", SevError, l.Line, i, "«%s» al inicio de línea no es una sección conocida. ¿Está mal escrita?", "“%s” at the start of a line is not a known section. Is it misspelled?", l.Keyword)
			}
		}
		if s.Type != "frontend" && s.Type != "backend" && s.Type != "listen" {
			continue
		}
		isFE, isBE := s.Type != "backend", s.Type != "frontend"
		label := fmt.Sprintf(lang.t("%s «%s»", "%s “%s”"), s.Type, s.Name)
		has := func(kw string) bool {
			for _, l := range dirs {
				if l.Keyword == kw {
					return true
				}
			}
			return false
		}
		if isFE && !has("bind") {
			add("no-bind", SevError, s.Line, i, "%s no tiene ninguna línea bind: no escucha en ningún puerto.", "%s has no bind line: it does not listen on any port.", label)
		}
		for _, tk := range []struct {
			name string
			need bool
		}{{"client", isFE}, {"connect", isBE}, {"server", isBE}} {
			if tk.need && !e.Timeouts[tk.name] {
				add("timeout-missing", SevWarn, s.Line, i, "%s: falta «timeout %s» (ni aquí ni en defaults).", "%s: missing “timeout %s” (neither here nor in defaults).", label, tk.name)
			}
		}
		hasServers := has("server") || has("server-template") || has("dispatch")
		selfAnswers := has("redirect")
		for _, l := range dirs {
			if (l.Keyword == "stats" && arg(l.Args, 0) == "enable") || (l.Keyword == "http-request" && regexp.MustCompile(`^(return|redirect|use-service|deny)`).MatchString(arg(l.Args, 0))) {
				selfAnswers = true
			}
		}
		if s.Type == "backend" && !hasServers {
			add("no-servers", SevWarn, s.Line, i, "%s no tiene servidores: responderá 503.", "%s has no servers: it will answer 503.", label)
		}
		if s.Type == "listen" && !hasServers && !selfAnswers {
			add("no-servers", SevWarn, s.Line, i, "%s no tiene servidores ni responde por sí mismo.", "%s has no servers and does not answer by itself.", label)
		}
		if s.Type == "frontend" && !has("default_backend") && !has("use_backend") && !selfAnswers {
			add("no-route", SevWarn, s.Line, i, "%s no envía el tráfico a ningún backend.", "%s does not send traffic to any backend.", label)
		}
		acls := map[string]bool{}
		for _, l := range dirs {
			if l.Keyword == "acl" {
				acls[arg(l.Args, 0)] = true
			}
		}
		for _, l := range dirs {
			target := ""
			switch l.Keyword {
			case "default_backend":
				target = arg(l.Args, 0)
			case "use_backend":
				main, _ := SplitCond(l.Args)
				target = arg(main, 0)
			}
			if target != "" && !strings.Contains(target, "%[") {
				ti, ok := targets[target]
				if !ok {
					add("backend-missing", SevError, l.Line, i, "El backend «%s» no existe.", "The backend “%s” does not exist.", target)
				} else if e.Mode == "http" && eff[ti].Mode == "tcp" {
					add("mode-mismatch", SevError, l.Line, i, "Un %s en modo http no puede enviar tráfico al backend «%s», que está en modo tcp.", "An http-mode %s cannot send traffic to the backend “%s”, which is in tcp mode.", s.Type, target)
				}
			}
			if l.Keyword != "acl" {
				if _, cd := SplitCond(l.Args); cd != nil {
					for _, n := range cd.ACLNames() {
						if !acls[n] && !KB.builtinACLs[n] {
							add("acl-undefined", SevError, l.Line, i, "La ACL «%s» no está definida en esta sección.", "The ACL “%s” is not defined in this section.", n)
						}
					}
				}
			}
			switch {
			case l.Keyword == "stats" && arg(l.Args, 0) == "auth" && reWeakPass.MatchString(arg(l.Args, 1)):
				add("weak-stats-auth", SevInfo, l.Line, i, "La contraseña de la página de estadísticas parece de ejemplo. Cámbiala.", "The statistics page password looks like a placeholder. Change it.")
			case l.Keyword == "server" && toSet(l.Args)["ssl"] && kv(l.Args, "verify") == "none":
				add("tls-no-verify", SevInfo, l.Line, i, "El servidor «%s» usa TLS sin verificar el certificado.", "The server “%s” uses TLS without verifying the certificate.", arg(l.Args, 0))
			case l.Keyword == "bind" && e.Mode == "http" && toSet(l.Args)["ssl"] && !toSet(l.Args)["alpn"]:
				add("tls-no-alpn", SevInfo, l.Line, i, "Este bind TLS no declara «alpn h2,http/1.1»: los clientes no podrán usar HTTP/2.", "This TLS bind does not declare “alpn h2,http/1.1”: clients will not be able to use HTTP/2.")
			}
		}
	}
	if len(doc.Sections) > 0 && globals == 0 {
		file = 0
		add("no-global", SevInfo, 1, -1, "No hay sección global (es opcional, pero suele usarse para logs, usuario y límites).", "There is no global section (it is optional, but it is usually used for logging, user and limits).")
	}
	issues = append(issues, lintExtra(doc, eff, lang, opts)...)
	sort.SliceStable(issues, func(a, b int) bool {
		if issues[a].File != issues[b].File {
			return issues[a].File < issues[b].File
		}
		if issues[a].Line != issues[b].Line {
			return issues[a].Line < issues[b].Line
		}
		return sevRank[issues[a].Severity] < sevRank[issues[b].Severity]
	})
	return issues
}

// fileLabel devuelve el nombre del archivo para los mensajes, si se conoce.
func fileLabel(doc *Doc, i int, lang Lang) string {
	if i >= 0 && i < len(doc.Files) && doc.Files[i] != "" {
		return doc.Files[i]
	}
	return fmt.Sprintf(lang.t("archivo %d", "file %d"), i+1)
}
