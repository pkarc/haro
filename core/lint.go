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
	Severity Severity `json:"severity"`
	Line     int      `json:"line"`
	Section  int      `json:"section"` // -1 si está fuera de secciones
	Message  string   `json:"message"`
}

// Rules documenta cada regla del detector. El identificador es estable y
// sirve para desactivar reglas desde la configuración del usuario o en CI.
var Rules = map[string]string{
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
}

var reGlobalPrefix = regexp.MustCompile(`^(tune\.|ssl-|quic|httpclient|h1-|h2-|ocsp)`)
var reWeakPass = regexp.MustCompile(`(?i):(admin|password|123456|cambiame|changeme|cambiar|secret)$`)

// Lint analiza el documento y devuelve los problemas ordenados por línea.
// Es orientativo: la validación definitiva es `haproxy -c`.
func Lint(doc *Doc) []Issue {
	issues := []Issue{}
	add := func(rule string, sev Severity, line, sec int, format string, a ...any) {
		issues = append(issues, Issue{Rule: rule, Severity: sev, Line: line, Section: sec, Message: fmt.Sprintf(format, a...)})
	}
	for _, l := range doc.Pre {
		if l.Kind == KindDirective {
			add("outside-section", SevError, l.Line, -1, "«%s» está fuera de cualquier sección. Toda directiva debe ir dentro de global, defaults, frontend, backend…", l.Keyword)
		}
	}
	eff := ComputeEffective(doc)
	seen := map[string]int{}
	targets := map[string]int{}
	globals := 0
	for i, s := range doc.Sections {
		info := Sections[s.Type]
		if s.Type == "global" {
			globals++
			if globals == 2 {
				add("multiple-global", SevWarn, s.Line, i, "Hay más de una sección global; HAProxy las combina, pero es mejor tener una sola.")
			}
		}
		if info.Named == "required" && s.Name == "" {
			add("section-name", SevError, s.Line, i, "La sección %s necesita un nombre.", s.Type)
		}
		if s.Name != "" {
			keys := []string{s.Type}
			if s.Type == "listen" {
				keys = []string{"frontend", "backend"}
			}
			for _, k := range keys {
				key := k + ":" + s.Name
				if prev, ok := seen[key]; ok {
					add("duplicate-name", SevError, s.Line, i, "Ya existe otra sección con capacidad de %s llamada «%s» (línea %d).", k, s.Name, prev)
				} else {
					seen[key] = s.Line
				}
			}
		}
		if (s.Type == "backend" || s.Type == "listen") && s.Name != "" {
			targets[s.Name] = i
		}
	}

	for i, s := range doc.Sections {
		dirs := s.Directives()
		e := eff[i]
		checkPlacement := proxySections[s.Type]
		for _, l := range dirs {
			d, known := KB.Directives[l.Keyword]
			if known && checkPlacement && !d.AllowedIn(s.Type) {
				add("misplaced", SevWarn, l.Line, i, "«%s» no es válida en una sección %s.", l.Keyword, s.Type)
			}
			if !known && l.IndentSet && l.Indent == "" && !reGlobalPrefix.MatchString(l.Keyword) {
				add("unknown-section", SevError, l.Line, i, "«%s» al inicio de línea no es una sección conocida. ¿Está mal escrita?", l.Keyword)
			}
		}
		if s.Type != "frontend" && s.Type != "backend" && s.Type != "listen" {
			continue
		}
		isFE, isBE := s.Type != "backend", s.Type != "frontend"
		label := fmt.Sprintf("%s «%s»", s.Type, s.Name)
		has := func(kw string) bool {
			for _, l := range dirs {
				if l.Keyword == kw {
					return true
				}
			}
			return false
		}
		if isFE && !has("bind") {
			add("no-bind", SevError, s.Line, i, "%s no tiene ninguna línea bind: no escucha en ningún puerto.", label)
		}
		for _, tk := range []struct {
			name string
			need bool
		}{{"client", isFE}, {"connect", isBE}, {"server", isBE}} {
			if tk.need && !e.Timeouts[tk.name] {
				add("timeout-missing", SevWarn, s.Line, i, "%s: falta «timeout %s» (ni aquí ni en defaults).", label, tk.name)
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
			add("no-servers", SevWarn, s.Line, i, "%s no tiene servidores: responderá 503.", label)
		}
		if s.Type == "listen" && !hasServers && !selfAnswers {
			add("no-servers", SevWarn, s.Line, i, "%s no tiene servidores ni responde por sí mismo.", label)
		}
		if s.Type == "frontend" && !has("default_backend") && !has("use_backend") && !selfAnswers {
			add("no-route", SevWarn, s.Line, i, "%s no envía el tráfico a ningún backend.", label)
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
					add("backend-missing", SevError, l.Line, i, "El backend «%s» no existe.", target)
				} else if e.Mode == "http" && eff[ti].Mode == "tcp" {
					add("mode-mismatch", SevError, l.Line, i, "Un %s en modo http no puede enviar tráfico al backend «%s», que está en modo tcp.", s.Type, target)
				}
			}
			if l.Keyword != "acl" {
				if _, cd := SplitCond(l.Args); cd != nil {
					for _, n := range cd.ACLNames() {
						if !acls[n] && !KB.builtinACLs[n] {
							add("acl-undefined", SevError, l.Line, i, "La ACL «%s» no está definida en esta sección.", n)
						}
					}
				}
			}
			switch {
			case l.Keyword == "stats" && arg(l.Args, 0) == "auth" && reWeakPass.MatchString(arg(l.Args, 1)):
				add("weak-stats-auth", SevInfo, l.Line, i, "La contraseña de la página de estadísticas parece de ejemplo. Cámbiala.")
			case l.Keyword == "server" && toSet(l.Args)["ssl"] && kv(l.Args, "verify") == "none":
				add("tls-no-verify", SevInfo, l.Line, i, "El servidor «%s» usa TLS sin verificar el certificado.", arg(l.Args, 0))
			case l.Keyword == "bind" && e.Mode == "http" && toSet(l.Args)["ssl"] && !toSet(l.Args)["alpn"]:
				add("tls-no-alpn", SevInfo, l.Line, i, "Este bind TLS no declara «alpn h2,http/1.1»: los clientes no podrán usar HTTP/2.")
			}
		}
	}
	if len(doc.Sections) > 0 && globals == 0 {
		add("no-global", SevInfo, 1, -1, "No hay sección global (es opcional, pero suele usarse para logs, usuario y límites).")
	}
	sort.SliceStable(issues, func(a, b int) bool {
		if issues[a].Line != issues[b].Line {
			return issues[a].Line < issues[b].Line
		}
		return sevRank[issues[a].Severity] < sevRank[issues[b].Severity]
	})
	return issues
}
