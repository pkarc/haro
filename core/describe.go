package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Las explicaciones usan un markdown mínimo: **negrita** y `código`.
// La web lo convierte a HTML y la TUI a estilos de terminal.

// Explanation es la explicación legible de una directiva.
type Explanation struct {
	Text     string `json:"text"`
	Category string `json:"category"`
	Known    bool   `json:"known"`
}

func b(s string) string { return "**" + s + "**" }
func c(s string) string { return "`" + s + "`" }

var reDuration = regexp.MustCompile(`^(\d+(?:\.\d+)?)(us|ms|s|m|h|d)?$`)

func dur(v, defUnit string) string {
	m := reDuration.FindStringSubmatch(v)
	if m == nil {
		return c(v)
	}
	unit := m[2]
	if unit == "" {
		if defUnit == "" {
			return b(m[1])
		}
		unit = defUnit
	}
	names := map[string]string{"us": "µs", "ms": "ms", "s": "s", "m": "min", "h": "h", "d": "días"}
	return b(m[1] + " " + names[unit])
}

func addr(s string) string {
	if s == "" {
		return "(sin dirección)"
	}
	x, pre := s, ""
	if i := strings.Index(x, "@"); i > 0 && regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(x[:i]) {
		pre, x = strings.ToLower(x[:i]), x[i+1:]
		switch pre {
		case "unix":
			return "socket Unix " + c(x)
		case "abns", "abnsz":
			return "socket abstracto " + c(x)
		case "fd":
			return "descriptor " + c(x)
		case "sockpair":
			return "socketpair " + c(x)
		}
	}
	if strings.HasPrefix(x, "/") {
		return "socket Unix " + c(x)
	}
	proto := ""
	if strings.HasPrefix(pre, "quic") {
		proto = " (QUIC / HTTP/3)"
	} else if strings.HasPrefix(pre, "udp") {
		proto = " (UDP)"
	}
	host, port := x, ""
	if strings.HasPrefix(x, "[") {
		if e := strings.Index(x, "]"); e > 0 {
			host = x[1:e]
			if len(x) > e+2 {
				port = x[e+2:]
			}
		}
	} else if i := strings.LastIndex(x, ":"); i >= 0 {
		host, port = x[:i], x[i+1:]
	}
	var h string
	switch host {
	case "", "*", "0.0.0.0":
		h = "todas las interfaces"
	case "::":
		h = "todas las interfaces (IPv4 + IPv6)"
	default:
		h = c(host)
	}
	if port != "" {
		return h + ", puerto " + c(port) + proto
	}
	return h + proto
}

func addrList(s string) string {
	parts := strings.Split(s, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = addr(p)
	}
	return strings.Join(out, " y ")
}

var valued = toSet(strings.Fields(`crt crt-list ca-file crl-file alpn npn ciphers ciphersuites curves ecdhe inter fastinter downinter rise fall weight maxconn maxqueue minconn cookie port addr check-sni sni resolvers init-addr resolve-prefer resolve-opts slowstart on-marked-down on-marked-up on-error verify ssl-min-ver ssl-max-ver name id process thread interface user group mode level uid gid source track agent-port agent-inter agent-addr agent-send proto check-proto check-alpn pool-max-conn pool-purge-delay pool-low-conn max-reuse ws observe error-limit namespace tcp-ut nice backlog mss shards quic-cc-algo severity-output tcp-md5sig redir log-proto ca-sign-file tls-ticket-keys crt-ignore-err ca-ignore-err sigalgs client-sigalgs label hash-key guid`))

type opt struct{ k, v string }

func parseOpts(args []string) []opt {
	var out []opt
	for i := 0; i < len(args); i++ {
		k := args[i]
		if valued[k] && i+1 < len(args) {
			out = append(out, opt{k, args[i+1]})
			i++
		} else {
			out = append(out, opt{k: k})
		}
	}
	return out
}

func rawOpt(o opt) string {
	if o.v != "" {
		return c(o.k + " " + o.v)
	}
	return c(o.k)
}

func serverOpts(args []string) []string {
	var p []string
	for _, o := range parseOpts(args) {
		switch o.k {
		case "check":
			p = append(p, "health checks activos")
		case "no-check":
			p = append(p, "sin health checks")
		case "inter":
			p = append(p, "check cada "+dur(o.v, "ms"))
		case "fastinter":
			p = append(p, "check cada "+dur(o.v, "ms")+" mientras cambia de estado")
		case "downinter":
			p = append(p, "check cada "+dur(o.v, "ms")+" si está caído")
		case "rise":
			p = append(p, "vuelve a estar sano tras "+b(o.v)+" checks OK")
		case "fall":
			p = append(p, "se marca caído tras "+b(o.v)+" fallos")
		case "weight":
			p = append(p, "peso "+b(o.v))
		case "backup":
			p = append(p, b("respaldo")+": solo recibe tráfico si caen los demás")
		case "maxconn":
			p = append(p, "máx. "+b(o.v)+" conexiones")
		case "maxqueue":
			p = append(p, "cola máx. "+b(o.v))
		case "ssl":
			p = append(p, "conexión "+b("TLS")+" hacia el servidor")
		case "verify":
			if o.v == "none" {
				p = append(p, "⚠ sin verificar su certificado")
			} else {
				p = append(p, "verificación TLS "+c(o.v))
			}
		case "ca-file":
			p = append(p, "CA "+c(o.v))
		case "sni":
			p = append(p, "SNI "+c(o.v))
		case "cookie":
			p = append(p, "valor de cookie "+c(o.v))
		case "send-proxy", "send-proxy-v2", "send-proxy-v2-ssl":
			p = append(p, "envía protocolo PROXY (IP real del cliente)")
		case "disabled":
			p = append(p, b("deshabilitado"))
		case "resolvers":
			p = append(p, "resuelve su nombre por DNS con "+c(o.v))
		case "init-addr":
			p = append(p, "resolución al arrancar: "+c(o.v))
		case "slowstart":
			p = append(p, "arranque progresivo durante "+dur(o.v, "ms"))
		case "port":
			p = append(p, "checks al puerto "+c(o.v))
		case "addr":
			p = append(p, "checks a "+c(o.v))
		case "check-ssl":
			p = append(p, "checks por TLS")
		case "agent-check":
			p = append(p, "agent-check activo")
		case "on-marked-down":
			p = append(p, "al caer: "+c(o.v))
		case "proto":
			p = append(p, "protocolo "+c(o.v))
		case "alpn":
			p = append(p, "ALPN "+c(o.v))
		case "track":
			p = append(p, "copia el estado de "+c(o.v))
		case "non-stick":
			p = append(p, "excluido de la persistencia")
		default:
			p = append(p, rawOpt(o))
		}
	}
	return p
}

func describeBind(a []string) string {
	p := []string{"Escucha en " + addrList(arg(a, 0))}
	for _, o := range parseOpts(rest(a, 1)) {
		switch o.k {
		case "ssl":
			p = append(p, b("TLS / HTTPS"))
		case "crt":
			p = append(p, "certificado "+c(o.v))
		case "crt-list":
			p = append(p, "lista de certificados "+c(o.v))
		case "alpn":
			s := "ALPN " + c(o.v)
			if regexp.MustCompile(`\bh2\b`).MatchString(o.v) {
				s += " (habilita HTTP/2)"
			}
			p = append(p, s)
		case "accept-proxy":
			p = append(p, "acepta protocolo PROXY")
		case "v4v6":
			p = append(p, "IPv4 e IPv6")
		case "v6only":
			p = append(p, "solo IPv6")
		case "transparent":
			p = append(p, "modo transparente")
		case "ssl-min-ver":
			p = append(p, "TLS mínimo "+c(o.v))
		case "strict-sni":
			p = append(p, "rechaza SNI sin certificado")
		case "verify":
			switch o.v {
			case "required":
				p = append(p, b("exige certificado de cliente")+" (mTLS)")
			case "optional":
				p = append(p, "certificado de cliente opcional")
			default:
				p = append(p, "verify "+c(o.v))
			}
		case "ca-file":
			p = append(p, "CA de clientes "+c(o.v))
		case "name":
			p = append(p, "nombre "+c(o.v))
		case "interface":
			p = append(p, "solo en la interfaz "+c(o.v))
		case "mode":
			p = append(p, "permisos "+c(o.v))
		case "level":
			p = append(p, "nivel "+c(o.v))
		default:
			p = append(p, rawOpt(o))
		}
	}
	return strings.Join(p, " · ")
}

var reFetch = regexp.MustCompile(`^([\w.\-]+)\(([^)]*)\)(.*)$`)
var reMatchSuffix = regexp.MustCompile(`^(.*?)_(beg|end|sub|reg|dom|dir|len|found|str|ip|int|bool|cnt)$`)
var reScFetch = regexp.MustCompile(`^sc_(http_req_rate|conn_cur|conn_rate|http_err_rate)$`)

func fetchSubject(fn, a string) (string, bool) {
	host := strings.EqualFold(a, "host")
	switch fn {
	case "hdr", "req.hdr":
		if host {
			return "el Host", true
		}
		return "la cabecera " + c(a), true
	case "req.fhdr":
		return "la cabecera " + c(a), true
	case "res.hdr", "shdr":
		return "la cabecera de respuesta " + c(a), true
	case "urlp", "url_param":
		return "el parámetro " + c(a) + " de la URL", true
	case "cook", "req.cook":
		return "la cookie " + c(a), true
	case "nbsrv":
		return "el nº de servidores activos en " + c(a), true
	case "http_auth":
		return "hay credenciales válidas de " + c(a), true
	case "be_conn":
		return "las conexiones del backend " + c(a), true
	case "var":
		return "la variable " + c(a), true
	case "env":
		return "la variable de entorno " + c(a), true
	}
	if m := reScFetch.FindStringSubmatch(fn); m != nil {
		if a == "" {
			a = "0"
		}
		names := map[string]string{"http_req_rate": "el ritmo de peticiones", "conn_cur": "las conexiones abiertas", "conn_rate": "el ritmo de conexiones", "http_err_rate": "el ritmo de errores"}
		return names[m[1]] + " del cliente (contador " + a + ")", true
	}
	if s, ok := KB.Fetches[fn]; ok {
		return s, true
	}
	return "", false
}

// ACLPhrase explica el criterio de una ACL: `path_beg -i /api /v2`.
func ACLPhrase(t []string) string {
	if len(t) == 0 {
		return ""
	}
	crit := t[0]
	i := 1
	flags := map[string]string{}
	for i < len(t) && len(t[i]) > 1 && t[i][0] == '-' && t[i] != "--" {
		f := t[i]
		if f == "-m" || f == "-f" || f == "-M" || f == "-u" {
			flags[f] = arg(t, i+1)
			i += 2
		} else {
			flags[f] = "1"
			i++
		}
	}
	if i < len(t) && t[i] == "--" {
		i++
	}
	vals := rest(t, i)
	fn, a, conv := crit, "", ""
	hasParen := false
	if m := reFetch.FindStringSubmatch(crit); m != nil {
		fn, a, conv, hasParen = m[1], m[2], m[3], true
	} else if ci := strings.Index(crit, ","); ci > 0 {
		fn, conv = crit[:ci], crit[ci:]
	}
	match := flags["-m"]
	if _, known := fetchSubject(fn, a); !known {
		if m := reMatchSuffix.FindStringSubmatch(fn); m != nil {
			if _, ok := fetchSubject(m[1], a); ok {
				fn = m[1]
				if match == "" {
					match = m[2]
				}
			}
		}
	}
	subject, known := fetchSubject(fn, a)
	if !known {
		if hasParen {
			subject = c(fn + "(" + a + ")")
		} else {
			subject = c(fn)
		}
	}
	if conv != "" {
		subject += " " + c(conv)
	}
	ipish := fn == "src" || fn == "dst"
	var out string
	switch {
	case flags["-f"] != "":
		verb := KB.Matchers[match]
		if match == "" {
			verb = "coincide con"
			if ipish {
				verb = "está en"
			}
		}
		out = subject + " " + verb + " la lista del fichero " + c(flags["-f"])
	case flags["-M"] != "":
		out = subject + " está en el mapa " + c(flags["-M"])
	case len(vals) > 0 && KB.Operators[vals[0]] != "":
		out = subject + " es " + KB.Operators[vals[0]] + " " + b(arg(vals, 1))
	case len(vals) == 0:
		if match == "found" {
			out = subject + " existe"
		} else if KB.booleanFetches[fn] || !known {
			out = subject
		} else {
			out = subject + " existe"
		}
	default:
		verb, ok := KB.Matchers[match]
		if !ok {
			verb = "coincide (" + match + ") con"
		}
		if match == "" && ipish {
			verb = "está en"
		}
		cv := make([]string, len(vals))
		for k, v := range vals {
			cv[k] = c(v)
		}
		out = subject + " " + verb + " " + strings.Join(cv, " o ")
	}
	if flags["-i"] != "" {
		out += " (sin distinguir mayúsculas)"
	}
	return out
}

// CondPhrase explica una condición completa.
func CondPhrase(cd *Cond) string {
	var out []string
	prevTerm := false
	t := cd.Tokens
	for i := 0; i < len(t); i++ {
		tok := t[i]
		if tok == "||" || strings.EqualFold(tok, "or") {
			out = append(out, "o")
			prevTerm = false
			continue
		}
		neg := false
		if strings.HasPrefix(tok, "!") {
			neg = true
			tok = tok[1:]
			if tok == "" {
				i++
				if i >= len(t) {
					break
				}
				tok = t[i]
			}
		}
		var term string
		if strings.HasPrefix(tok, "{") {
			var inner []string
			if len(tok) > 1 {
				inner = append(inner, tok[1:])
			}
			i++
			for i < len(t) && t[i] != "}" {
				inner = append(inner, t[i])
				i++
			}
			term = "«" + ACLPhrase(inner) + "»"
		} else {
			term = c(tok)
		}
		if prevTerm {
			out = append(out, "y")
		}
		if neg {
			term = b("no") + " " + term
		}
		out = append(out, term)
		prevTerm = true
	}
	return strings.Join(out, " ")
}

func condSuffix(cd *Cond) string {
	if cd == nil {
		return ""
	}
	if cd.Negated {
		return " — salvo si " + CondPhrase(cd)
	}
	return " — si " + CondPhrase(cd)
}

func kv(a []string, k string) string {
	for i, v := range a {
		if v == k {
			return arg(a, i+1)
		}
	}
	return ""
}

func redirectPhrase(r []string) string {
	o := map[string]string{}
	for i := 0; i < len(r); i++ {
		switch r[i] {
		case "location", "prefix", "scheme", "code", "set-cookie", "clear-cookie":
			o[r[i]] = arg(r, i+1)
			i++
		default:
			o[r[i]] = "1"
		}
	}
	var s string
	switch {
	case o["scheme"] != "":
		s = "Redirige a " + b(strings.ToUpper(o["scheme"]))
	case o["location"] != "":
		s = "Redirige a " + c(o["location"])
	case o["prefix"] != "":
		s = "Redirige anteponiendo " + c(o["prefix"])
	default:
		s = "Redirige"
	}
	if code := o["code"]; code != "" {
		if n := KB.RedirectCodes[code]; n != "" {
			s += " (" + code + " " + n + ")"
		} else {
			s += " (" + code + ")"
		}
	}
	if o["drop-query"] != "" {
		s += ", sin la query string"
	}
	return s
}

var reParen = regexp.MustCompile(`\((.*)\)`)

func actionPhrase(act string, r []string) string {
	name := act
	if i := strings.Index(act, "("); i >= 0 {
		name = act[:i]
	}
	inner := ""
	if m := reParen.FindStringSubmatch(act); m != nil {
		inner = m[1]
	}
	rs := strings.Join(r, " ")
	switch name {
	case "redirect":
		return redirectPhrase(r)
	case "deny":
		st := kv(r, "deny_status")
		if st == "" {
			st = kv(r, "status")
		}
		if st == "" {
			st = "403"
		}
		return b("Rechaza") + " la petición (" + st + ")"
	case "allow":
		return "Permite y deja de evaluar reglas"
	case "accept":
		return "Acepta"
	case "auth":
		s := "Pide usuario y contraseña (HTTP Basic)"
		if rl := kv(r, "realm"); rl != "" {
			s += " · realm " + c(rl)
		}
		return s
	case "tarpit":
		return "Retiene y luego rechaza (tarpit, frena abusos)"
	case "reject":
		return b("Corta") + " la conexión sin responder"
	case "silent-drop":
		return "Descarta la conexión en silencio"
	case "set-header":
		return "Fija la cabecera " + c(arg(r, 0)) + " = " + c(strings.Join(rest(r, 1), " "))
	case "add-header":
		return "Añade la cabecera " + c(arg(r, 0)) + ": " + c(strings.Join(rest(r, 1), " "))
	case "del-header":
		return "Elimina la cabecera " + c(arg(r, 0))
	case "replace-header":
		return "Reescribe la cabecera " + c(arg(r, 0)) + " (" + c(arg(r, 1)) + " → " + c(arg(r, 2)) + ")"
	case "set-path":
		return "Cambia la ruta a " + c(rs)
	case "replace-path":
		return "Reescribe la ruta (" + c(arg(r, 0)) + " → " + c(arg(r, 1)) + ")"
	case "set-uri":
		return "Cambia la URI a " + c(rs)
	case "set-query":
		return "Cambia la query string a " + c(rs)
	case "set-method":
		return "Cambia el método a " + c(rs)
	case "set-status":
		return "Cambia el código de estado a " + b(arg(r, 0))
	case "return":
		s := "Responde directamente"
		if st := kv(r, "status"); st != "" {
			s += " con estado " + b(st)
		}
		return s
	case "track-sc0", "track-sc1", "track-sc2":
		s := "Cuenta al cliente en la stick-table (contador " + name[len(name)-1:] + ") usando " + c(arg(r, 0))
		if tb := kv(r, "table"); tb != "" {
			s += " de " + c(tb)
		}
		return s
	case "set-var":
		return "Guarda en la variable " + c(inner) + " " + c(rs)
	case "unset-var":
		return "Borra la variable " + c(inner)
	case "cache-use":
		return "Sirve desde la caché " + c(arg(r, 0)) + " si la tiene"
	case "cache-store":
		return "Guarda la respuesta en la caché " + c(arg(r, 0))
	case "capture":
		return "Captura " + c(arg(r, 0)) + " para los logs"
	case "set-log-level":
		return "Cambia el nivel de log a " + c(arg(r, 0))
	case "use-service":
		return "Responde con el servicio " + c(arg(r, 0))
	case "wait-for-body":
		return "Espera a recibir el cuerpo de la petición"
	case "set-timeout":
		return "Ajusta el timeout " + c(arg(r, 0)) + " a " + dur(arg(r, 1), "ms")
	case "set-src":
		return "Usa como IP de origen " + c(rs)
	case "disable-l7-retry":
		return "Desactiva los reintentos de capa 7"
	case "expect-proxy":
		return "Espera el protocolo PROXY"
	}
	if strings.HasPrefix(name, "lua.") {
		return "Ejecuta la acción Lua " + c(name[4:])
	}
	if rs != "" {
		return "Acción " + c(act) + " " + c(rs)
	}
	return "Acción " + c(act)
}

func ruleDesc(a []string, label string) string {
	main, cd := SplitCond(a)
	return label + " " + actionPhrase(arg(main, 0), rest(main, 1)) + condSuffix(cd)
}

func tlsOpts(a []string) string {
	names := map[string]string{"no-sslv3": "sin SSLv3", "no-tlsv10": "sin TLS 1.0", "no-tlsv11": "sin TLS 1.1", "no-tlsv12": "sin TLS 1.2", "no-tlsv13": "sin TLS 1.3", "no-tls-tickets": "sin tickets TLS", "prefer-client-ciphers": "prefiere los cifrados del cliente", "force-tlsv12": "solo TLS 1.2", "force-tlsv13": "solo TLS 1.3"}
	var out []string
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "ssl-min-ver":
			out = append(out, "TLS mínimo "+c(arg(a, i+1)))
			i++
		case "ssl-max-ver":
			out = append(out, "TLS máximo "+c(arg(a, i+1)))
			i++
		default:
			if n, ok := names[a[i]]; ok {
				out = append(out, n)
			} else {
				out = append(out, c(a[i]))
			}
		}
	}
	return strings.Join(out, ", ")
}

type describer func(a []string, section string) (string, bool)

var describers map[string]describer

func init() {
	describers = map[string]describer{
		"mode": func(a []string, _ string) (string, bool) {
			switch arg(a, 0) {
			case "http":
				return b("Modo HTTP") + " (capa 7): HAProxy interpreta peticiones, cabeceras y rutas, y puede aplicar reglas sobre ellas.", true
			case "tcp":
				return b("Modo TCP") + " (capa 4): reenvía los bytes tal cual, sin inspeccionar HTTP.", true
			case "log":
				return "Modo log: reenvía mensajes syslog.", true
			}
			return "Modo " + c(arg(a, 0)), false
		},
		"bind": func(a []string, s string) (string, bool) {
			if s == "peers" {
				return "Esta instancia peer escucha en " + addrList(arg(a, 0)), true
			}
			return describeBind(a), true
		},
		"server": func(a []string, s string) (string, bool) {
			if s == "peers" {
				return "Peer " + b(arg(a, 0)) + " en " + c(arg(a, 1)), true
			}
			t := "Servidor " + b(arg(a, 0)) + " en " + c(arg(a, 1))
			if o := serverOpts(rest(a, 2)); len(o) > 0 {
				t += " · " + strings.Join(o, " · ")
			}
			return t, true
		},
		"server-template": func(a []string, _ string) (string, bool) {
			first, last := 1, 0
			if m := regexp.MustCompile(`^(\d+)-(\d+)$`).FindStringSubmatch(arg(a, 1)); m != nil {
				first, _ = strconv.Atoi(m[1])
				last, _ = strconv.Atoi(m[2])
			} else {
				last, _ = strconv.Atoi(arg(a, 1))
			}
			t := fmt.Sprintf("Genera %s servidores %s…%s a partir de %s", b(strconv.Itoa(last-first+1)), c(fmt.Sprint(arg(a, 0), first)), c(fmt.Sprint(arg(a, 0), last)), c(arg(a, 2)))
			if o := serverOpts(rest(a, 3)); len(o) > 0 {
				t += " · " + strings.Join(o, " · ")
			}
			return t, true
		},
		"default-server": func(a []string, _ string) (string, bool) {
			return "Opciones por defecto para todos los `server`: " + strings.Join(serverOpts(a), " · "), true
		},
		"balance": func(a []string, _ string) (string, bool) {
			k := arg(a, 0)
			if strings.HasPrefix(k, "hdr(") {
				return "Reparto: " + b("hash de la cabecera") + " " + c(strings.TrimSuffix(k[4:], ")")), true
			}
			if k == "url_param" {
				return "Reparto: hash del parámetro de URL " + c(arg(a, 1)), true
			}
			if d, ok := KB.Balance[k]; ok {
				return "Reparto de carga: " + d, true
			}
			return "Reparto de carga: " + c(strings.Join(a, " ")), false
		},
		"hash-type": func(a []string, _ string) (string, bool) {
			switch arg(a, 0) {
			case "consistent":
				return b("Hash consistente") + ": al quitar o añadir un servidor solo se reasignan sus clientes", true
			case "map-based":
				return "Hash basado en mapa (estático)", true
			}
			return "Tipo de hash " + c(strings.Join(a, " ")), true
		},
		"timeout": func(a []string, _ string) (string, bool) {
			what, ok := KB.Timeouts[arg(a, 0)]
			if !ok {
				what = c(arg(a, 0))
			}
			return "Tiempo máximo " + what + ": " + dur(arg(a, 1), "ms"), ok
		},
		"option": func(a []string, _ string) (string, bool) {
			o, extra := arg(a, 0), rest(a, 1)
			if o == "httpchk" {
				m, u := "OPTIONS", "/"
				if len(extra) == 1 {
					u = extra[0]
				} else if len(extra) >= 2 {
					m, u = extra[0], extra[1]
				}
				return "Health checks " + b("HTTP") + ": envía " + c(m+" "+u) + " a cada servidor en lugar de solo abrir TCP", true
			}
			if d, ok := KB.Options[o]; ok {
				if len(extra) > 0 {
					d += " " + c(strings.Join(extra, " "))
				}
				return d, true
			}
			return "Opción " + c(strings.Join(a, " ")), false
		},
		"no": func(a []string, _ string) (string, bool) {
			if arg(a, 0) == "option" {
				if d, ok := KB.Options[arg(a, 1)]; ok {
					return b("Desactiva:") + " " + d, true
				}
				return b("Desactiva:") + " " + c(arg(a, 1)), false
			}
			return "Desactiva " + c(strings.Join(a, " ")), false
		},
		"maxconn": func(a []string, s string) (string, bool) {
			switch s {
			case "global":
				return "Máximo de conexiones simultáneas de todo el proceso: " + b(arg(a, 0)), true
			case "defaults":
				return "Máximo de conexiones simultáneas por defecto en cada entrada: " + b(arg(a, 0)), true
			}
			return "Máximo de conexiones simultáneas en esta entrada: " + b(arg(a, 0)), true
		},
		"retries": func(a []string, _ string) (string, bool) {
			return "Reintentos de conexión a un servidor antes de fallar: " + b(arg(a, 0)), true
		},
		"default_backend": func(a []string, _ string) (string, bool) {
			return "Backend por defecto: " + b(arg(a, 0)) + " — recibe todo lo que no coincide con ninguna regla `use_backend`", true
		},
		"use_backend": func(a []string, _ string) (string, bool) {
			main, cd := SplitCond(a)
			if t := arg(main, 0); strings.Contains(t, "%[") {
				return "Envía al backend que resulte de evaluar " + c(t) + " (enrutado dinámico)" + condSuffix(cd), true
			}
			return "Envía al backend " + b(arg(main, 0)) + condSuffix(cd), true
		},
		"use-server": func(a []string, _ string) (string, bool) {
			main, cd := SplitCond(a)
			return "Fuerza el servidor " + b(arg(main, 0)) + condSuffix(cd), true
		},
		"acl": func(a []string, _ string) (string, bool) {
			return "Define la condición " + c(arg(a, 0)) + ": " + ACLPhrase(rest(a, 1)), true
		},
		"http-request":        func(a []string, _ string) (string, bool) { return ruleDesc(a, "Petición:"), true },
		"http-response":       func(a []string, _ string) (string, bool) { return ruleDesc(a, "Respuesta:"), true },
		"http-after-response": func(a []string, _ string) (string, bool) { return ruleDesc(a, "Respuesta final:"), true },
		"tcp-request": func(a []string, _ string) (string, bool) {
			if arg(a, 0) == "inspect-delay" {
				return "Espera hasta " + dur(arg(a, 1), "ms") + " para poder inspeccionar el contenido TCP", true
			}
			label := map[string]string{"connection": "Conexión TCP:", "content": "Contenido TCP:", "session": "Sesión TCP:"}[arg(a, 0)]
			if label == "" {
				label = "TCP:"
			}
			return ruleDesc(rest(a, 1), label), true
		},
		"redirect": func(a []string, _ string) (string, bool) {
			main, cd := SplitCond(a)
			return redirectPhrase(main) + condSuffix(cd), true
		},
		"log": func(a []string, _ string) (string, bool) {
			t := arg(a, 0)
			if t == "global" {
				return "Usa los destinos de log definidos en `global`", true
			}
			var tgt string
			switch {
			case t == "/dev/log":
				tgt = "al syslog local (`/dev/log`)"
			case t == "stdout":
				tgt = "a la salida estándar"
			case t == "stderr":
				tgt = "a la salida de error"
			case strings.HasPrefix(t, "ring@"):
				tgt = "al buffer " + c(t[5:])
			default:
				tgt = "al syslog " + c(t)
			}
			var r []string
			for i := 1; i < len(a); i++ {
				if a[i] == "len" || a[i] == "format" || a[i] == "sample" {
					i++
					continue
				}
				r = append(r, a[i])
			}
			s := "Envía logs " + tgt
			if len(r) > 0 {
				s += " · facility " + c(r[0])
			}
			if len(r) > 1 {
				s += " · solo nivel " + c(r[1]) + " o más grave"
			}
			return s, true
		},
		"stats": func(a []string, _ string) (string, bool) {
			r := rest(a, 1)
			switch arg(a, 0) {
			case "socket":
				s := "Socket de administración en " + addr(arg(r, 0))
				for _, o := range parseOpts(rest(r, 1)) {
					switch o.k {
					case "level":
						if o.v == "admin" {
							s += " con " + b("permisos de administrador")
						} else {
							s += " (nivel " + c(o.v) + ")"
						}
					case "mode":
						s += ", permisos de fichero " + c(o.v)
					case "expose-fd":
						s += ", permite recargas sin cortes"
					}
				}
				return s, true
			case "timeout":
				return "Timeout del socket de estadísticas: " + dur(arg(r, 0), "ms"), true
			case "enable":
				return "Activa la " + b("página web de estadísticas"), true
			case "uri":
				return "Página de estadísticas en la ruta " + c(arg(r, 0)), true
			case "auth":
				return "Protegida con el usuario " + b(strings.SplitN(arg(r, 0), ":", 2)[0]) + " y contraseña", true
			case "realm":
				return "Texto del diálogo de login: " + c(strings.Join(r, " ")), true
			case "refresh":
				return "La página se recarga cada " + dur(arg(r, 0), "s"), true
			case "admin":
				_, cd := SplitCond(r)
				return "Permite acciones de administración desde la web" + condSuffix(cd), true
			case "hide-version":
				return "Oculta la versión de HAProxy en la página", true
			}
			return "Estadísticas: " + c(strings.Join(a, " ")), false
		},
		"cookie": func(a []string, _ string) (string, bool) {
			f := toSet(rest(a, 1))
			p := []string{"Persistencia por cookie " + b(arg(a, 0))}
			for _, x := range []struct{ k, d string }{{"insert", "HAProxy la crea"}, {"rewrite", "reescribe la del servidor"}, {"prefix", "prefija la del servidor"}, {"indirect", "no se reenvía al servidor"}, {"nocache", "evita que se cachee"}, {"httponly", "HttpOnly"}, {"secure", "Secure"}} {
				if f[x.k] {
					p = append(p, x.d)
				}
			}
			return strings.Join(p, " · ") + " — el cliente vuelve siempre al mismo servidor", true
		},
		"stick-table": func(a []string, _ string) (string, bool) {
			s := "Tabla de seguimiento: clave " + c(kv(a, "type")) + ", " + kv(a, "size") + " entradas"
			if e := kv(a, "expire"); e != "" {
				s += ", caducan a los " + dur(e, "ms")
			}
			if st := kv(a, "store"); st != "" {
				var names []string
				for _, part := range strings.Split(st, ",") {
					m := regexp.MustCompile(`^(\w+)(?:\(([^)]*)\))?$`).FindStringSubmatch(part)
					if m == nil {
						names = append(names, part)
						continue
					}
					n := KB.Store[m[1]]
					if n == "" {
						n = m[1]
					}
					if m[2] != "" {
						n += " (" + m[2] + ")"
					}
					names = append(names, n)
				}
				s += " · guarda " + strings.Join(names, ", ")
			}
			if p := kv(a, "peers"); p != "" {
				s += " · replicada con " + c(p)
			}
			return s, true
		},
		"errorfile": func(a []string, _ string) (string, bool) {
			return "Página personalizada para el error " + b(arg(a, 0)) + " desde " + c(arg(a, 1)), true
		},
		"http-check": func(a []string, _ string) (string, bool) {
			r := rest(a, 1)
			switch arg(a, 0) {
			case "expect":
				k := arg(r, 0)
				neg := strings.HasPrefix(k, "!")
				k = strings.TrimPrefix(k, "!")
				v := strings.Join(rest(r, 1), " ")
				what := map[string]string{"status": "responde con estado " + b(v), "rstatus": "el estado cumple " + c(v), "string": "el cuerpo contiene " + c(v), "rstring": "el cuerpo cumple " + c(v)}[k]
				if what == "" {
					what = c(strings.Join(r, " "))
				}
				if neg {
					what = b("no") + " " + what
				}
				return "El check es correcto si " + what, true
			case "send":
				m, u := kv(r, "meth"), kv(r, "uri")
				if m == "" {
					m = "GET"
				}
				if u == "" {
					u = "/"
				}
				return "El check envía " + c(m+" "+u), true
			case "connect":
				return "El check abre una conexión", true
			case "disable-on-404":
				return "Si el check recibe 404, el servidor pasa a modo drenaje (no recibe nuevos clientes)", true
			}
			return "Check HTTP: " + c(strings.Join(a, " ")), true
		},
		"user": func(a []string, s string) (string, bool) {
			if s != "userlist" {
				return "Ejecuta HAProxy como el usuario " + b(arg(a, 0)), true
			}
			p := []string{"Usuario " + b(arg(a, 0))}
			f := toSet(a)
			if f["password"] {
				p = append(p, "contraseña cifrada")
			}
			if f["insecure-password"] {
				p = append(p, "⚠ contraseña en texto plano")
			}
			if g := kv(a, "groups"); g != "" {
				p = append(p, "grupos "+c(g))
			}
			return strings.Join(p, " · "), true
		},
		"group": func(a []string, s string) (string, bool) {
			if s != "userlist" {
				return "Ejecuta HAProxy con el grupo " + b(arg(a, 0)), true
			}
			t := "Grupo " + b(arg(a, 0))
			if u := kv(a, "users"); u != "" {
				t += " con los usuarios " + c(u)
			}
			return t, true
		},
		"nameserver": func(a []string, _ string) (string, bool) {
			return "Servidor DNS " + b(arg(a, 0)) + " en " + c(arg(a, 1)), true
		},
		"nbthread": func(a []string, _ string) (string, bool) { return "Usa " + b(arg(a, 0)) + " hilos de trabajo", true },
		"hard-stop-after": func(a []string, _ string) (string, bool) {
			return "Al recargar, corta las conexiones antiguas tras " + dur(arg(a, 0), "ms") + " como máximo", true
		},
		"ssl-default-bind-options": func(a []string, _ string) (string, bool) {
			return "Opciones TLS por defecto en los bind: " + tlsOpts(a), true
		},
		"ssl-default-server-options": func(a []string, _ string) (string, bool) {
			return "Opciones TLS por defecto hacia servidores: " + tlsOpts(a), true
		},
		"description": func(a []string, _ string) (string, bool) { return "Descripción: " + strings.Join(a, " "), true },
	}
}

// Describe explica una directiva en el contexto de su tipo de sección.
func Describe(l *Line, section string) Explanation {
	info, known := KB.Directives[l.Keyword]
	ex := Explanation{Category: "misc", Known: known}
	if known {
		ex.Category = info.Category
	}
	if f, ok := describers[l.Keyword]; ok {
		text, k := f(l.Args, section)
		ex.Text = text
		if !k {
			ex.Known = false
		}
	}
	if ex.Text == "" && known && info.Description != "" {
		ex.Text = info.Description
		if strings.HasSuffix(info.Description, ":") && len(l.Args) > 0 {
			ex.Text += " " + c(strings.Join(l.Args, " "))
		}
	}
	if ex.Text == "" && strings.HasPrefix(l.Keyword, "tune.") {
		ex = Explanation{Text: "Ajuste fino interno de rendimiento (" + c(l.Keyword[5:]) + " = " + c(strings.Join(l.Args, " ")) + ")", Category: "perf", Known: true}
	}
	if ex.Text == "" && regexp.MustCompile(`^(ssl-|quic|httpclient\.|h1-|h2-|ocsp)`).MatchString(l.Keyword) {
		cat := "misc"
		if strings.HasPrefix(l.Keyword, "ssl") {
			cat = "tls"
		}
		ex = Explanation{Text: "Ajuste global " + c(l.Keyword), Category: cat, Known: true}
	}
	if ex.Text == "" {
		ex = Explanation{Text: "Directiva que haro no reconoce (puede ser válida en tu versión; revisa la documentación).", Category: "misc", Known: false}
	}
	return ex
}

// DisplayArgs devuelve los argumentos tal como están escritos, ocultando
// contraseñas.
func DisplayArgs(l *Line) string {
	t := l.Text
	if l.Comment != "" {
		t = strings.TrimRight(t[:len(t)-len(l.Comment)], " \t")
	}
	if i := strings.IndexAny(t, " \t"); i >= 0 {
		t = strings.TrimSpace(t[i:])
	} else {
		t = ""
	}
	switch l.Keyword {
	case "stats":
		t = regexp.MustCompile(`^(auth\s+[^:\s]+:)\S+`).ReplaceAllString(t, "${1}••••••")
	case "user":
		t = regexp.MustCompile(`((?:insecure-)?password\s+)\S+`).ReplaceAllString(t, "${1}••••••")
	}
	return t
}

func arg(a []string, i int) string {
	if i >= 0 && i < len(a) {
		return a[i]
	}
	return ""
}

func rest(a []string, i int) []string {
	if i < len(a) {
		return a[i:]
	}
	return nil
}
