package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Explanations use minimal markdown: **bold** and `code`.
// The web converts it to HTML and the TUI to terminal styles.
//
// The language travels with the call (type tr), never in a global variable: the
// server handles concurrent requests in different languages. Each phrase
// is written in full in both languages; when the word order
// changes, two templates (fmt.Sprintf) are used instead of concatenating.

// Explanation is the human-readable explanation of a directive.
type Explanation struct {
	Text     string `json:"text"`
	Category string `json:"category"`
	Known    bool   `json:"known"`
}

func b(s string) string { return "**" + s + "**" }
func c(s string) string { return "`" + s + "`" }

// tr is the language context of an explanation.
type tr struct{ lang Lang }

// t picks the text according to the language.
func (x tr) t(es, en string) string { return x.lang.t(es, en) }

// f is t followed by fmt.Sprintf: two complete templates, one per language.
func (x tr) f(es, en string, a ...any) string { return fmt.Sprintf(x.t(es, en), a...) }

// kb returns the knowledge base texts for the language.
func (x tr) kb() *KnowledgeText { return KB.Text(x.lang) }

var reDuration = regexp.MustCompile(`^(\d+(?:\.\d+)?)(us|ms|s|m|h|d)?$`)

func (x tr) dur(v, defUnit string) string {
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
	day := x.t("días", "days")
	if m[1] == "1" {
		day = x.t("día", "day")
	}
	names := map[string]string{"us": "µs", "ms": "ms", "s": "s", "m": "min", "h": "h", "d": day}
	return b(m[1] + " " + names[unit])
}

func (x tr) addr(s string) string {
	if s == "" {
		return x.t("(sin dirección)", "(no address)")
	}
	a, pre := s, ""
	if i := strings.Index(a, "@"); i > 0 && regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(a[:i]) {
		pre, a = strings.ToLower(a[:i]), a[i+1:]
		switch pre {
		case "unix":
			return x.t("socket Unix ", "Unix socket ") + c(a)
		case "abns", "abnsz":
			return x.t("socket abstracto ", "abstract socket ") + c(a)
		case "fd":
			return x.t("descriptor ", "file descriptor ") + c(a)
		case "sockpair":
			return "socketpair " + c(a)
		}
	}
	if strings.HasPrefix(a, "/") {
		return x.t("socket Unix ", "Unix socket ") + c(a)
	}
	proto := ""
	if strings.HasPrefix(pre, "quic") {
		proto = " (QUIC / HTTP/3)"
	} else if strings.HasPrefix(pre, "udp") {
		proto = " (UDP)"
	}
	host, port := a, ""
	if strings.HasPrefix(a, "[") {
		if e := strings.Index(a, "]"); e > 0 {
			host = a[1:e]
			if len(a) > e+2 {
				port = a[e+2:]
			}
		}
	} else if i := strings.LastIndex(a, ":"); i >= 0 {
		host, port = a[:i], a[i+1:]
	}
	var h string
	switch host {
	case "", "*", "0.0.0.0":
		h = x.t("todas las interfaces", "all interfaces")
	case "::":
		h = x.t("todas las interfaces (IPv4 + IPv6)", "all interfaces (IPv4 + IPv6)")
	default:
		h = c(host)
	}
	if port != "" {
		return h + x.t(", puerto ", ", port ") + c(port) + proto
	}
	return h + proto
}

func (x tr) addrList(s string) string {
	parts := strings.Split(s, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = x.addr(p)
	}
	return strings.Join(out, x.t(" y ", " and "))
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

func (x tr) serverOpts(args []string) []string {
	var p []string
	for _, o := range parseOpts(args) {
		switch o.k {
		case "check":
			p = append(p, x.t("health checks activos", "active health checks"))
		case "no-check":
			p = append(p, x.t("sin health checks", "no health checks"))
		case "inter":
			p = append(p, x.t("check cada ", "check every ")+x.dur(o.v, "ms"))
		case "fastinter":
			p = append(p, x.t("check cada "+x.dur(o.v, "ms")+" mientras cambia de estado", "check every "+x.dur(o.v, "ms")+" while changing state"))
		case "downinter":
			p = append(p, x.t("check cada "+x.dur(o.v, "ms")+" si está caído", "check every "+x.dur(o.v, "ms")+" when down"))
		case "rise":
			p = append(p, x.t("vuelve a estar sano tras "+b(o.v)+" checks OK", "healthy again after "+b(o.v)+" successful checks"))
		case "fall":
			p = append(p, x.t("se marca caído tras "+b(o.v)+" fallos", "marked down after "+b(o.v)+" failures"))
		case "weight":
			p = append(p, x.t("peso ", "weight ")+b(o.v))
		case "backup":
			p = append(p, x.t(b("respaldo")+": solo recibe tráfico si caen los demás", b("backup")+": only receives traffic if the others go down"))
		case "maxconn":
			p = append(p, x.t("máx. "+b(o.v)+" conexiones", "max "+b(o.v)+" connections"))
		case "maxqueue":
			p = append(p, x.t("cola máx. ", "max queue ")+b(o.v))
		case "ssl":
			p = append(p, x.t("conexión "+b("TLS")+" hacia el servidor", b("TLS")+" connection to the server"))
		case "verify":
			if o.v == "none" {
				p = append(p, x.t("⚠ sin verificar su certificado", "⚠ certificate not verified"))
			} else {
				p = append(p, x.t("verificación TLS ", "TLS verification ")+c(o.v))
			}
		case "ca-file":
			p = append(p, "CA "+c(o.v))
		case "sni":
			p = append(p, "SNI "+c(o.v))
		case "cookie":
			p = append(p, x.t("valor de cookie ", "cookie value ")+c(o.v))
		case "send-proxy", "send-proxy-v2", "send-proxy-v2-ssl":
			p = append(p, x.t("envía protocolo PROXY (IP real del cliente)", "sends the PROXY protocol (real client IP)"))
		case "disabled":
			p = append(p, b(x.t("deshabilitado", "disabled")))
		case "resolvers":
			p = append(p, x.t("resuelve su nombre por DNS con ", "resolves its name via DNS using ")+c(o.v))
		case "init-addr":
			p = append(p, x.t("resolución al arrancar: ", "resolution at startup: ")+c(o.v))
		case "slowstart":
			p = append(p, x.t("arranque progresivo durante ", "gradual ramp-up over ")+x.dur(o.v, "ms"))
		case "port":
			p = append(p, x.t("checks al puerto ", "checks sent to port ")+c(o.v))
		case "addr":
			p = append(p, x.t("checks a ", "checks sent to ")+c(o.v))
		case "check-ssl":
			p = append(p, x.t("checks por TLS", "checks over TLS"))
		case "agent-check":
			p = append(p, x.t("agent-check activo", "agent-check enabled"))
		case "on-marked-down":
			p = append(p, x.t("al caer: ", "when marked down: ")+c(o.v))
		case "proto":
			p = append(p, x.t("protocolo ", "protocol ")+c(o.v))
		case "alpn":
			p = append(p, "ALPN "+c(o.v))
		case "track":
			p = append(p, x.t("copia el estado de ", "mirrors the state of ")+c(o.v))
		case "non-stick":
			p = append(p, x.t("excluido de la persistencia", "excluded from persistence"))
		default:
			p = append(p, rawOpt(o))
		}
	}
	return p
}

func (x tr) describeBind(a []string) string {
	p := []string{x.t("Escucha en ", "Listens on ") + x.addrList(arg(a, 0))}
	for _, o := range parseOpts(rest(a, 1)) {
		switch o.k {
		case "ssl":
			p = append(p, b("TLS / HTTPS"))
		case "crt":
			p = append(p, x.t("certificado ", "certificate ")+c(o.v))
		case "crt-list":
			p = append(p, x.t("lista de certificados ", "certificate list ")+c(o.v))
		case "alpn":
			s := "ALPN " + c(o.v)
			if regexp.MustCompile(`\bh2\b`).MatchString(o.v) {
				s += x.t(" (habilita HTTP/2)", " (enables HTTP/2)")
			}
			p = append(p, s)
		case "accept-proxy":
			p = append(p, x.t("acepta protocolo PROXY", "accepts the PROXY protocol"))
		case "v4v6":
			p = append(p, x.t("IPv4 e IPv6", "IPv4 and IPv6"))
		case "v6only":
			p = append(p, x.t("solo IPv6", "IPv6 only"))
		case "transparent":
			p = append(p, x.t("modo transparente", "transparent mode"))
		case "ssl-min-ver":
			p = append(p, x.t("TLS mínimo ", "minimum TLS ")+c(o.v))
		case "strict-sni":
			p = append(p, x.t("rechaza SNI sin certificado", "rejects SNI without a certificate"))
		case "verify":
			switch o.v {
			case "required":
				p = append(p, x.t(b("exige certificado de cliente")+" (mTLS)", b("requires a client certificate")+" (mTLS)"))
			case "optional":
				p = append(p, x.t("certificado de cliente opcional", "optional client certificate"))
			default:
				p = append(p, "verify "+c(o.v))
			}
		case "ca-file":
			p = append(p, x.t("CA de clientes ", "client CA ")+c(o.v))
		case "name":
			p = append(p, x.t("nombre ", "name ")+c(o.v))
		case "interface":
			p = append(p, x.t("solo en la interfaz ", "only on interface ")+c(o.v))
		case "mode":
			p = append(p, x.t("permisos ", "permissions ")+c(o.v))
		case "level":
			p = append(p, x.t("nivel ", "level ")+c(o.v))
		default:
			p = append(p, rawOpt(o))
		}
	}
	return strings.Join(p, " · ")
}

var reFetch = regexp.MustCompile(`^([\w.\-]+)\(([^)]*)\)(.*)$`)
var reMatchSuffix = regexp.MustCompile(`^(.*?)_(beg|end|sub|reg|dom|dir|len|found|str|ip|int|bool|cnt)$`)
var reScFetch = regexp.MustCompile(`^sc_(http_req_rate|conn_cur|conn_rate|http_err_rate)$`)

func (x tr) fetchSubject(fn, a string) (string, bool) {
	host := strings.EqualFold(a, "host")
	switch fn {
	case "hdr", "req.hdr":
		if host {
			return x.t("el Host", "the Host header"), true
		}
		return x.t("la cabecera ", "the header ") + c(a), true
	case "req.fhdr":
		return x.t("la cabecera ", "the header ") + c(a), true
	case "res.hdr", "shdr":
		return x.t("la cabecera de respuesta ", "the response header ") + c(a), true
	case "urlp", "url_param":
		return x.t("el parámetro "+c(a)+" de la URL", "the URL parameter "+c(a)), true
	case "cook", "req.cook":
		return x.t("la cookie ", "the cookie ") + c(a), true
	case "nbsrv":
		return x.t("el nº de servidores activos en ", "the number of active servers in ") + c(a), true
	case "http_auth":
		return x.t("hay credenciales válidas de ", "there are valid credentials for ") + c(a), true
	case "be_conn":
		return x.t("las conexiones del backend ", "the number of connections of backend ") + c(a), true
	case "var":
		return x.t("la variable ", "the variable ") + c(a), true
	case "env":
		return x.t("la variable de entorno ", "the environment variable ") + c(a), true
	}
	if m := reScFetch.FindStringSubmatch(fn); m != nil {
		if a == "" {
			a = "0"
		}
		es := map[string]string{"http_req_rate": "el ritmo de peticiones", "conn_cur": "las conexiones abiertas", "conn_rate": "el ritmo de conexiones", "http_err_rate": "el ritmo de errores"}
		en := map[string]string{"http_req_rate": "request rate", "conn_cur": "number of open connections", "conn_rate": "connection rate", "http_err_rate": "error rate"}
		return x.t(es[m[1]]+" del cliente (contador "+a+")", "the client's "+en[m[1]]+" (counter "+a+")"), true
	}
	if s, ok := x.kb().Fetches[fn]; ok {
		return s, true
	}
	return "", false
}

// ACLPhrase explains an ACL's criterion, in the default language: `path_beg -i /api /v2`.
func ACLPhrase(t []string) string { return ACLPhraseLang(t, LangDefault) }

// ACLPhraseLang is ACLPhrase in the requested language.
func ACLPhraseLang(t []string, lang Lang) string { return tr{lang}.aclPhrase(t) }

func (x tr) aclPhrase(t []string) string {
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
	if _, known := x.fetchSubject(fn, a); !known {
		if m := reMatchSuffix.FindStringSubmatch(fn); m != nil {
			if _, ok := x.fetchSubject(m[1], a); ok {
				fn = m[1]
				if match == "" {
					match = m[2]
				}
			}
		}
	}
	subject, known := x.fetchSubject(fn, a)
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
	text := x.kb()
	ipish := fn == "src" || fn == "dst"
	var out string
	switch {
	case flags["-f"] != "":
		verb := text.Matchers[match]
		if match == "" {
			verb = x.t("coincide con", "matches")
			if ipish {
				verb = x.t("está en", "is in")
			}
		}
		out = subject + " " + verb + x.t(" la lista del fichero ", " the list in file ") + c(flags["-f"])
	case flags["-M"] != "":
		out = subject + x.t(" está en el mapa ", " is in the map ") + c(flags["-M"])
	case len(vals) > 0 && text.Operators[vals[0]] != "":
		// With a comparison operator, what is compared is the number: the
		// length (path_len) or the count (hdr_cnt), not the subject itself.
		subj := subject
		switch match {
		case "len":
			subj = x.t("la longitud de "+subject, "the length of "+subject)
		case "cnt":
			subj = x.t("el nº de apariciones de "+subject, "the number of occurrences of "+subject)
		}
		subj = strings.NewReplacer("de el ", "del ").Replace(subj)
		out = subj + x.t(" es ", " is ") + text.Operators[vals[0]] + " " + b(arg(vals, 1))
	case len(vals) == 0:
		if match == "found" {
			out = subject + x.t(" existe", " exists")
		} else if KB.booleanFetches[fn] || !known {
			out = subject
		} else {
			out = subject + x.t(" existe", " exists")
		}
	default:
		verb, ok := text.Matchers[match]
		if !ok {
			verb = x.t("coincide ("+match+") con", "matches ("+match+")")
		}
		if match == "" && ipish {
			verb = x.t("está en", "is in")
		}
		cv := make([]string, len(vals))
		for k, v := range vals {
			cv[k] = c(v)
		}
		out = subject + " " + verb + " " + strings.Join(cv, x.t(" o ", " or "))
	}
	if flags["-i"] != "" {
		out += x.t(" (sin distinguir mayúsculas)", " (case-insensitive)")
	}
	return out
}

// CondPhrase explains a complete condition, in the default language.
func CondPhrase(cd *Cond) string { return CondPhraseLang(cd, LangDefault) }

// CondPhraseLang is CondPhrase in the requested language.
func CondPhraseLang(cd *Cond, lang Lang) string { return tr{lang}.condPhrase(cd) }

func (x tr) condPhrase(cd *Cond) string {
	var out []string
	prevTerm := false
	t := cd.Tokens
	for i := 0; i < len(t); i++ {
		tok := t[i]
		if tok == "||" || strings.EqualFold(tok, "or") {
			out = append(out, x.t("o", "or"))
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
			term = x.t("«"+x.aclPhrase(inner)+"»", "“"+x.aclPhrase(inner)+"”")
		} else {
			term = c(tok)
		}
		if prevTerm {
			out = append(out, x.t("y", "and"))
		}
		if neg {
			term = b(x.t("no", "not")) + " " + term
		}
		out = append(out, term)
		prevTerm = true
	}
	return strings.Join(out, " ")
}

func (x tr) condSuffix(cd *Cond) string {
	if cd == nil {
		return ""
	}
	if cd.Negated {
		return x.t(" — salvo si ", " — unless ") + x.condPhrase(cd)
	}
	return x.t(" — si ", " — if ") + x.condPhrase(cd)
}

func kv(a []string, k string) string {
	for i, v := range a {
		if v == k {
			return arg(a, i+1)
		}
	}
	return ""
}

func (x tr) redirectPhrase(r []string) string {
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
		s = x.t("Redirige a ", "Redirects to ") + b(strings.ToUpper(o["scheme"]))
	case o["location"] != "":
		s = x.t("Redirige a ", "Redirects to ") + c(o["location"])
	case o["prefix"] != "":
		s = x.t("Redirige anteponiendo ", "Redirects by prepending ") + c(o["prefix"])
	default:
		s = x.t("Redirige", "Redirects")
	}
	if code := o["code"]; code != "" {
		if n := x.kb().RedirectCodes[code]; n != "" {
			s += " (" + code + " " + n + ")"
		} else {
			s += " (" + code + ")"
		}
	}
	if o["drop-query"] != "" {
		s += x.t(", sin la query string", ", dropping the query string")
	}
	return s
}

var reParen = regexp.MustCompile(`\((.*)\)`)

func (x tr) actionPhrase(act string, r []string) string {
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
		return x.redirectPhrase(r)
	case "deny":
		st := kv(r, "deny_status")
		if st == "" {
			st = kv(r, "status")
		}
		if st == "" {
			st = "403"
		}
		return x.t(b("Rechaza")+" la petición ("+st+")", b("Denies")+" the request ("+st+")")
	case "allow":
		return x.t("Permite y deja de evaluar reglas", "Allows and stops evaluating rules")
	case "accept":
		return x.t("Acepta", "Accepts")
	case "auth":
		s := x.t("Pide usuario y contraseña (HTTP Basic)", "Asks for username and password (HTTP Basic)")
		if rl := kv(r, "realm"); rl != "" {
			s += " · realm " + c(rl)
		}
		return s
	case "tarpit":
		return x.t("Retiene y luego rechaza (tarpit, frena abusos)", "Holds and then rejects (tarpit, slows down abuse)")
	case "reject":
		return x.t(b("Corta")+" la conexión sin responder", b("Closes")+" the connection without answering")
	case "silent-drop":
		return x.t("Descarta la conexión en silencio", "Silently drops the connection")
	case "set-header":
		return x.t("Fija la cabecera ", "Sets the header ") + c(arg(r, 0)) + " = " + c(strings.Join(rest(r, 1), " "))
	case "add-header":
		return x.t("Añade la cabecera ", "Adds the header ") + c(arg(r, 0)) + ": " + c(strings.Join(rest(r, 1), " "))
	case "del-header":
		return x.t("Elimina la cabecera ", "Removes the header ") + c(arg(r, 0))
	case "replace-header":
		return x.t("Reescribe la cabecera ", "Rewrites the header ") + c(arg(r, 0)) + " (" + c(arg(r, 1)) + " → " + c(arg(r, 2)) + ")"
	case "set-path":
		return x.t("Cambia la ruta a ", "Sets the path to ") + c(rs)
	case "replace-path":
		return x.t("Reescribe la ruta (", "Rewrites the path (") + c(arg(r, 0)) + " → " + c(arg(r, 1)) + ")"
	case "set-uri":
		return x.t("Cambia la URI a ", "Sets the URI to ") + c(rs)
	case "set-query":
		return x.t("Cambia la query string a ", "Sets the query string to ") + c(rs)
	case "set-method":
		return x.t("Cambia el método a ", "Sets the method to ") + c(rs)
	case "set-status":
		return x.t("Cambia el código de estado a ", "Sets the status code to ") + b(arg(r, 0))
	case "return":
		s := x.t("Responde directamente", "Answers directly")
		if st := kv(r, "status"); st != "" {
			s += x.t(" con estado ", " with status ") + b(st)
		}
		return s
	case "track-sc0", "track-sc1", "track-sc2":
		n := name[len(name)-1:]
		s := x.t("Cuenta al cliente en la stick-table (contador "+n+") usando "+c(arg(r, 0)), "Tracks the client in the stick table (counter "+n+") using "+c(arg(r, 0)))
		if tb := kv(r, "table"); tb != "" {
			s += x.t(" de ", ", table ") + c(tb)
		}
		return s
	case "set-var":
		return x.f("Guarda en la variable %s %s", "Stores %[2]s in the variable %[1]s", c(inner), c(rs))
	case "unset-var":
		return x.t("Borra la variable ", "Deletes the variable ") + c(inner)
	case "cache-use":
		return x.f("Sirve desde la caché %s si la tiene", "Serves from the cache %s if it has the object", c(arg(r, 0)))
	case "cache-store":
		return x.t("Guarda la respuesta en la caché ", "Stores the response in the cache ") + c(arg(r, 0))
	case "capture":
		return x.f("Captura %s para los logs", "Captures %s for the logs", c(arg(r, 0)))
	case "set-log-level":
		return x.t("Cambia el nivel de log a ", "Sets the log level to ") + c(arg(r, 0))
	case "use-service":
		return x.t("Responde con el servicio ", "Answers with the service ") + c(arg(r, 0))
	case "wait-for-body":
		return x.t("Espera a recibir el cuerpo de la petición", "Waits to receive the request body")
	case "set-timeout":
		return x.f("Ajusta el timeout %s a %s", "Sets the timeout %s to %s", c(arg(r, 0)), x.dur(arg(r, 1), "ms"))
	case "set-src":
		return x.f("Usa como IP de origen %s", "Uses %s as the source IP", c(rs))
	case "disable-l7-retry":
		return x.t("Desactiva los reintentos de capa 7", "Disables layer 7 retries")
	case "expect-proxy":
		return x.t("Espera el protocolo PROXY", "Expects the PROXY protocol")
	}
	if strings.HasPrefix(name, "lua.") {
		return x.t("Ejecuta la acción Lua ", "Runs the Lua action ") + c(name[4:])
	}
	if act == "" {
		return x.t("Acción incompleta", "Incomplete action")
	}
	if rs != "" {
		return x.t("Acción ", "Action ") + c(act) + " " + c(rs)
	}
	return x.t("Acción ", "Action ") + c(act)
}

func (x tr) ruleDesc(a []string, label string) string {
	main, cd := SplitCond(a)
	return label + " " + x.actionPhrase(arg(main, 0), rest(main, 1)) + x.condSuffix(cd)
}

func (x tr) tlsOpts(a []string) string {
	names := map[string]string{
		"no-sslv3":              x.t("sin SSLv3", "no SSLv3"),
		"no-tlsv10":             x.t("sin TLS 1.0", "no TLS 1.0"),
		"no-tlsv11":             x.t("sin TLS 1.1", "no TLS 1.1"),
		"no-tlsv12":             x.t("sin TLS 1.2", "no TLS 1.2"),
		"no-tlsv13":             x.t("sin TLS 1.3", "no TLS 1.3"),
		"no-tls-tickets":        x.t("sin tickets TLS", "no TLS tickets"),
		"prefer-client-ciphers": x.t("prefiere los cifrados del cliente", "prefers the client's ciphers"),
		"force-tlsv12":          x.t("solo TLS 1.2", "TLS 1.2 only"),
		"force-tlsv13":          x.t("solo TLS 1.3", "TLS 1.3 only"),
	}
	var out []string
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "ssl-min-ver":
			out = append(out, x.t("TLS mínimo ", "minimum TLS ")+c(arg(a, i+1)))
			i++
		case "ssl-max-ver":
			out = append(out, x.t("TLS máximo ", "maximum TLS ")+c(arg(a, i+1)))
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

type describer func(x tr, a []string, section string) (string, bool)

var describers map[string]describer

func init() {
	describers = map[string]describer{
		"mode": func(x tr, a []string, _ string) (string, bool) {
			switch arg(a, 0) {
			case "http":
				return x.t(b("Modo HTTP")+" (capa 7): HAProxy interpreta peticiones, cabeceras y rutas, y puede aplicar reglas sobre ellas.", b("HTTP mode")+" (layer 7): HAProxy parses requests, headers and paths, and can apply rules to them."), true
			case "tcp":
				return x.t(b("Modo TCP")+" (capa 4): reenvía los bytes tal cual, sin inspeccionar HTTP.", b("TCP mode")+" (layer 4): forwards the bytes as they are, without inspecting HTTP."), true
			case "log":
				return x.t("Modo log: reenvía mensajes syslog.", "Log mode: forwards syslog messages."), true
			}
			return x.t("Modo ", "Mode ") + c(arg(a, 0)), false
		},
		"bind": func(x tr, a []string, s string) (string, bool) {
			if s == "peers" {
				return x.t("Esta instancia peer escucha en ", "This peer instance listens on ") + x.addrList(arg(a, 0)), true
			}
			return x.describeBind(a), true
		},
		"server": func(x tr, a []string, s string) (string, bool) {
			if s == "peers" {
				return x.f("Peer %s en %s", "Peer %s at %s", b(arg(a, 0)), c(arg(a, 1))), true
			}
			t := x.f("Servidor %s en %s", "Server %s at %s", b(arg(a, 0)), c(arg(a, 1)))
			if o := x.serverOpts(rest(a, 2)); len(o) > 0 {
				t += " · " + strings.Join(o, " · ")
			}
			return t, true
		},
		"server-template": func(x tr, a []string, _ string) (string, bool) {
			first, last := 1, 0
			if m := regexp.MustCompile(`^(\d+)-(\d+)$`).FindStringSubmatch(arg(a, 1)); m != nil {
				first, _ = strconv.Atoi(m[1])
				last, _ = strconv.Atoi(m[2])
			} else {
				last, _ = strconv.Atoi(arg(a, 1))
			}
			t := x.f("Genera %s servidores %s…%s a partir de %s", "Generates %s servers %s…%s from %s", b(strconv.Itoa(last-first+1)), c(fmt.Sprint(arg(a, 0), first)), c(fmt.Sprint(arg(a, 0), last)), c(arg(a, 2)))
			if o := x.serverOpts(rest(a, 3)); len(o) > 0 {
				t += " · " + strings.Join(o, " · ")
			}
			return t, true
		},
		"default-server": func(x tr, a []string, _ string) (string, bool) {
			return x.t("Opciones por defecto para todos los `server`: ", "Default options for all `server` lines: ") + strings.Join(x.serverOpts(a), " · "), true
		},
		"balance": func(x tr, a []string, _ string) (string, bool) {
			k := arg(a, 0)
			if strings.HasPrefix(k, "hdr(") {
				return x.t("Reparto: "+b("hash de la cabecera")+" ", "Balancing: "+b("hash of the header")+" ") + c(strings.TrimSuffix(k[4:], ")")), true
			}
			if k == "url_param" {
				return x.t("Reparto: hash del parámetro de URL ", "Balancing: hash of the URL parameter ") + c(arg(a, 1)), true
			}
			base := k
			if i := strings.Index(base, "("); i > 0 {
				base = base[:i]
			}
			if d, ok := x.kb().Balance[base]; ok {
				return x.t("Reparto de carga: ", "Load balancing: ") + d, true
			}
			return x.t("Reparto de carga: ", "Load balancing: ") + c(strings.Join(a, " ")), false
		},
		"hash-type": func(x tr, a []string, _ string) (string, bool) {
			switch arg(a, 0) {
			case "consistent":
				return x.t(b("Hash consistente")+": al quitar o añadir un servidor solo se reasignan sus clientes", b("Consistent hash")+": when a server is added or removed, only its clients are reassigned"), true
			case "map-based":
				return x.t("Hash basado en mapa (estático)", "Map-based (static) hash"), true
			}
			return x.t("Tipo de hash ", "Hash type ") + c(strings.Join(a, " ")), true
		},
		"timeout": func(x tr, a []string, _ string) (string, bool) {
			what, ok := x.kb().Timeouts[arg(a, 0)]
			if !ok {
				what = c(arg(a, 0))
			}
			return x.t("Tiempo máximo ", "Maximum time ") + what + ": " + x.dur(arg(a, 1), "ms"), ok
		},
		"option": func(x tr, a []string, _ string) (string, bool) {
			o, extra := arg(a, 0), rest(a, 1)
			if o == "httpchk" {
				m, u := "OPTIONS", "/"
				if len(extra) == 1 {
					u = extra[0]
				} else if len(extra) >= 2 {
					m, u = extra[0], extra[1]
				}
				return x.f("Health checks %s: envía %s a cada servidor en lugar de solo abrir TCP", "%s health checks: sends %s to each server instead of just opening a TCP connection", b("HTTP"), c(m+" "+u)), true
			}
			if d, ok := x.kb().Options[o]; ok {
				if len(extra) > 0 {
					d += " " + c(strings.Join(extra, " "))
				}
				return d, true
			}
			return x.t("Opción ", "Option ") + c(strings.Join(a, " ")), false
		},
		"no": func(x tr, a []string, _ string) (string, bool) {
			if arg(a, 0) == "option" {
				if d, ok := x.kb().Options[arg(a, 1)]; ok {
					return b(x.t("Desactiva:", "Disables:")) + " " + d, true
				}
				return b(x.t("Desactiva:", "Disables:")) + " " + c(arg(a, 1)), false
			}
			return x.t("Desactiva ", "Disables ") + c(strings.Join(a, " ")), false
		},
		"maxconn": func(x tr, a []string, s string) (string, bool) {
			switch s {
			case "global":
				return x.t("Máximo de conexiones simultáneas de todo el proceso: ", "Maximum simultaneous connections for the whole process: ") + b(arg(a, 0)), true
			case "defaults":
				return x.t("Máximo de conexiones simultáneas por defecto en cada entrada: ", "Default maximum simultaneous connections on each entry point: ") + b(arg(a, 0)), true
			}
			return x.t("Máximo de conexiones simultáneas en esta entrada: ", "Maximum simultaneous connections on this entry point: ") + b(arg(a, 0)), true
		},
		"retries": func(x tr, a []string, _ string) (string, bool) {
			return x.t("Reintentos de conexión a un servidor antes de fallar: ", "Connection retries to a server before failing: ") + b(arg(a, 0)), true
		},
		"default_backend": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Backend por defecto: %s — recibe todo lo que no coincide con ninguna regla `use_backend`", "Default backend: %s — receives everything that matches no `use_backend` rule", b(arg(a, 0))), true
		},
		"use_backend": func(x tr, a []string, _ string) (string, bool) {
			main, cd := SplitCond(a)
			if t := arg(main, 0); strings.Contains(t, "%[") {
				return x.f("Envía al backend que resulte de evaluar %s (enrutado dinámico)", "Sends to the backend that results from evaluating %s (dynamic routing)", c(t)) + x.condSuffix(cd), true
			}
			return x.t("Envía al backend ", "Sends to the backend ") + b(arg(main, 0)) + x.condSuffix(cd), true
		},
		"use-server": func(x tr, a []string, _ string) (string, bool) {
			main, cd := SplitCond(a)
			return x.t("Fuerza el servidor ", "Forces the server ") + b(arg(main, 0)) + x.condSuffix(cd), true
		},
		"acl": func(x tr, a []string, _ string) (string, bool) {
			return x.t("Define la condición ", "Defines the condition ") + c(arg(a, 0)) + ": " + x.aclPhrase(rest(a, 1)), true
		},
		"http-request": func(x tr, a []string, _ string) (string, bool) {
			return x.ruleDesc(a, x.t("Petición:", "Request:")), true
		},
		"http-response": func(x tr, a []string, _ string) (string, bool) {
			return x.ruleDesc(a, x.t("Respuesta:", "Response:")), true
		},
		"http-after-response": func(x tr, a []string, _ string) (string, bool) {
			return x.ruleDesc(a, x.t("Respuesta final:", "Final response:")), true
		},
		"tcp-request": func(x tr, a []string, _ string) (string, bool) {
			if arg(a, 0) == "inspect-delay" {
				return x.f("Espera hasta %s para poder inspeccionar el contenido TCP", "Waits up to %s to be able to inspect the TCP content", x.dur(arg(a, 1), "ms")), true
			}
			label := map[string]string{
				"connection": x.t("Conexión TCP:", "TCP connection:"),
				"content":    x.t("Contenido TCP:", "TCP content:"),
				"session":    x.t("Sesión TCP:", "TCP session:"),
			}[arg(a, 0)]
			if label == "" {
				// With no known phase, everything that follows is the action.
				return x.ruleDesc(a, "TCP:"), true
			}
			return x.ruleDesc(rest(a, 1), label), true
		},
		"redirect": func(x tr, a []string, _ string) (string, bool) {
			main, cd := SplitCond(a)
			return x.redirectPhrase(main) + x.condSuffix(cd), true
		},
		"log": func(x tr, a []string, _ string) (string, bool) {
			t := arg(a, 0)
			if t == "global" {
				return x.t("Usa los destinos de log definidos en `global`", "Uses the log destinations defined in `global`"), true
			}
			var tgt string
			switch {
			case t == "/dev/log":
				tgt = x.t("al syslog local (`/dev/log`)", "to the local syslog (`/dev/log`)")
			case t == "stdout":
				tgt = x.t("a la salida estándar", "to standard output")
			case t == "stderr":
				tgt = x.t("a la salida de error", "to standard error")
			case strings.HasPrefix(t, "ring@"):
				tgt = x.t("al buffer ", "to the buffer ") + c(t[5:])
			default:
				tgt = x.t("al syslog ", "to the syslog ") + c(t)
			}
			var r []string
			for i := 1; i < len(a); i++ {
				if a[i] == "len" || a[i] == "format" || a[i] == "sample" {
					i++
					continue
				}
				r = append(r, a[i])
			}
			s := x.t("Envía logs ", "Sends logs ") + tgt
			if len(r) > 0 {
				s += " · facility " + c(r[0])
			}
			if len(r) > 1 {
				s += x.f(" · solo nivel %s o más grave", " · only level %s or more severe", c(r[1]))
			}
			return s, true
		},
		"stats": func(x tr, a []string, _ string) (string, bool) {
			r := rest(a, 1)
			switch arg(a, 0) {
			case "socket":
				s := x.t("Socket de administración en ", "Admin socket: ") + x.addr(arg(r, 0))
				for _, o := range parseOpts(rest(r, 1)) {
					switch o.k {
					case "level":
						if o.v == "admin" {
							s += x.t(" con "+b("permisos de administrador"), " with "+b("administrator permissions"))
						} else {
							s += x.t(" (nivel ", " (level ") + c(o.v) + ")"
						}
					case "mode":
						s += x.t(", permisos de fichero ", ", file permissions ") + c(o.v)
					case "expose-fd":
						s += x.t(", permite recargas sin cortes", ", allows zero-downtime reloads")
					}
				}
				return s, true
			case "timeout":
				return x.t("Timeout del socket de estadísticas: ", "Statistics socket timeout: ") + x.dur(arg(r, 0), "ms"), true
			case "enable":
				return x.t("Activa la ", "Enables the ") + b(x.t("página web de estadísticas", "statistics web page")), true
			case "uri":
				return x.t("Página de estadísticas en la ruta ", "Statistics page at the path ") + c(arg(r, 0)), true
			case "auth":
				return x.f("Protegida con el usuario %s y contraseña", "Protected with the user %s and a password", b(strings.SplitN(arg(r, 0), ":", 2)[0])), true
			case "realm":
				return x.t("Texto del diálogo de login: ", "Login dialog text: ") + c(strings.Join(r, " ")), true
			case "refresh":
				return x.t("La página se recarga cada ", "The page reloads every ") + x.dur(arg(r, 0), "s"), true
			case "admin":
				_, cd := SplitCond(r)
				return x.t("Permite acciones de administración desde la web", "Allows admin actions from the web page") + x.condSuffix(cd), true
			case "hide-version":
				return x.t("Oculta la versión de HAProxy en la página", "Hides the HAProxy version on the page"), true
			case "show-node":
				return x.t("Muestra el nombre del nodo en la página", "Shows the node name on the page"), true
			case "show-desc":
				return x.t("Muestra la descripción del nodo en la página", "Shows the node description on the page"), true
			case "show-legends":
				return x.t("Muestra datos extra de cada servidor (nombre, dirección…) al pasar el ratón", "Shows extra details of each server (name, address…) on hover"), true
			case "show-modules":
				return x.t("Muestra los módulos cargados en la página", "Shows the loaded modules on the page"), true
			case "scope":
				return x.t("Limita la página a la sección ", "Limits the page to the section ") + c(arg(r, 0)), true
			case "http-request":
				return x.ruleDesc(r, x.t("En la página de estadísticas:", "On the statistics page:")), true
			case "maxconn":
				return x.t("Máximo de conexiones simultáneas al socket de estadísticas: ", "Maximum simultaneous connections to the statistics socket: ") + b(arg(r, 0)), true
			case "bind-process":
				return x.t("Procesos que sirven las estadísticas: ", "Processes serving the statistics: ") + c(strings.Join(r, " ")), true
			}
			return x.t("Estadísticas: ", "Statistics: ") + c(strings.Join(a, " ")), false
		},
		"cookie": func(x tr, a []string, _ string) (string, bool) {
			f := toSet(rest(a, 1))
			p := []string{x.t("Persistencia por cookie ", "Cookie-based persistence ") + b(arg(a, 0))}
			for _, o := range []struct{ k, es, en string }{
				{"insert", "HAProxy la crea", "HAProxy creates it"},
				{"rewrite", "reescribe la del servidor", "rewrites the server's"},
				{"prefix", "prefija la del servidor", "prefixes the server's"},
				{"indirect", "no se reenvía al servidor", "not forwarded to the server"},
				{"nocache", "evita que se cachee", "prevents caching"},
				{"httponly", "HttpOnly", "HttpOnly"},
				{"secure", "Secure", "Secure"},
			} {
				if f[o.k] {
					p = append(p, x.t(o.es, o.en))
				}
			}
			return strings.Join(p, " · ") + x.t(" — el cliente vuelve siempre al mismo servidor", " — the client always goes back to the same server"), true
		},
		"stick-table": func(x tr, a []string, _ string) (string, bool) {
			s := x.f("Tabla de seguimiento: clave %s, %s entradas", "Stick table: key %s, %s entries", c(kv(a, "type")), kv(a, "size"))
			if e := kv(a, "expire"); e != "" {
				s += x.t(", caducan tras ", ", entries expire after ") + x.dur(e, "ms")
			}
			if st := kv(a, "store"); st != "" {
				var names []string
				for _, part := range strings.Split(st, ",") {
					m := regexp.MustCompile(`^(\w+)(?:\(([^)]*)\))?$`).FindStringSubmatch(part)
					if m == nil {
						names = append(names, part)
						continue
					}
					n := x.kb().Store[m[1]]
					if n == "" {
						n = m[1]
					}
					if m[2] != "" {
						n += " (" + m[2] + ")"
					}
					names = append(names, n)
				}
				s += x.t(" · guarda ", " · stores ") + strings.Join(names, ", ")
			}
			if p := kv(a, "peers"); p != "" {
				s += x.t(" · replicada con ", " · replicated with ") + c(p)
			}
			return s, true
		},
		"errorfile": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Página personalizada para el error %s desde %s", "Custom page for the %s error, loaded from %s", b(arg(a, 0)), c(arg(a, 1))), true
		},
		"http-check": func(x tr, a []string, _ string) (string, bool) {
			r := rest(a, 1)
			switch arg(a, 0) {
			case "expect":
				k := arg(r, 0)
				neg := strings.HasPrefix(k, "!")
				k = strings.TrimPrefix(k, "!")
				v := strings.Join(rest(r, 1), " ")
				what := map[string]string{
					"status":  x.t("responde con estado ", "the response status is ") + b(v),
					"rstatus": x.t("el estado cumple ", "the status matches ") + c(v),
					"string":  x.t("el cuerpo contiene ", "the body contains ") + c(v),
					"rstring": x.t("el cuerpo cumple ", "the body matches ") + c(v),
				}[k]
				if what == "" {
					what = c(strings.Join(r, " "))
				}
				if neg {
					return x.t("El check es correcto si "+b("no")+" "+what, "The check passes unless "+what), true
				}
				return x.t("El check es correcto si ", "The check passes if ") + what, true
			case "send":
				m, u := kv(r, "meth"), kv(r, "uri")
				if m == "" {
					m = "GET"
				}
				if u == "" {
					u = "/"
				}
				return x.t("El check envía ", "The check sends ") + c(m+" "+u), true
			case "connect":
				return x.t("El check abre una conexión", "The check opens a connection"), true
			case "disable-on-404":
				return x.t("Si el check recibe 404, el servidor pasa a modo drenaje (no recibe nuevos clientes)", "If the check receives a 404, the server enters drain mode (it receives no new clients)"), true
			}
			return x.t("Check HTTP: ", "HTTP check: ") + c(strings.Join(a, " ")), true
		},
		"user": func(x tr, a []string, s string) (string, bool) {
			if s != "userlist" {
				return x.t("Ejecuta HAProxy como el usuario ", "Runs HAProxy as the user ") + b(arg(a, 0)), true
			}
			p := []string{x.t("Usuario ", "User ") + b(arg(a, 0))}
			f := toSet(a)
			if f["password"] {
				p = append(p, x.t("contraseña cifrada", "hashed password"))
			}
			if f["insecure-password"] {
				p = append(p, x.t("⚠ contraseña en texto plano", "⚠ plaintext password"))
			}
			if g := kv(a, "groups"); g != "" {
				p = append(p, x.t("grupos ", "groups ")+c(g))
			}
			return strings.Join(p, " · "), true
		},
		"group": func(x tr, a []string, s string) (string, bool) {
			if s != "userlist" {
				return x.t("Ejecuta HAProxy con el grupo ", "Runs HAProxy with the group ") + b(arg(a, 0)), true
			}
			t := x.t("Grupo ", "Group ") + b(arg(a, 0))
			if u := kv(a, "users"); u != "" {
				t += x.t(" con los usuarios ", " with the users ") + c(u)
			}
			return t, true
		},
		"tcp-check": func(x tr, a []string, _ string) (string, bool) {
			r := rest(a, 1)
			switch arg(a, 0) {
			case "connect":
				t := x.t("El check abre una conexión", "The check opens a connection")
				if p := kv(r, "port"); p != "" {
					t += x.t(" al puerto ", " to port ") + c(p)
				}
				if toSet(r)["ssl"] {
					t += " (TLS)"
				}
				return t, true
			case "send", "send-lf":
				return x.t("El check envía ", "The check sends ") + c(strings.Join(r, " ")), true
			case "send-binary", "send-binary-lf":
				return x.t("El check envía los bytes ", "The check sends the bytes ") + c(strings.Join(r, " ")), true
			case "expect":
				return x.t("El check es correcto si la respuesta cumple ", "The check passes if the response matches ") + c(strings.Join(r, " ")), true
			case "comment":
				return x.t("Comentario del check: ", "Check comment: ") + c(strings.Join(r, " ")), true
			case "set-var", "set-var-fmt", "unset-var":
				return x.t("El check manipula la variable ", "The check manipulates the variable ") + c(arg(r, 0)), true
			}
			return x.t("Paso de check TCP: ", "TCP check step: ") + c(strings.Join(a, " ")), true
		},
		"tcp-response": func(x tr, a []string, _ string) (string, bool) {
			if arg(a, 0) == "inspect-delay" {
				return x.f("Espera hasta %s para poder inspeccionar la respuesta TCP", "Waits up to %s to be able to inspect the TCP response", x.dur(arg(a, 1), "ms")), true
			}
			if arg(a, 0) == "content" {
				return x.ruleDesc(rest(a, 1), x.t("Respuesta TCP:", "TCP response:")), true
			}
			return x.ruleDesc(a, x.t("Respuesta TCP:", "TCP response:")), true
		},
		"stick": func(x tr, a []string, _ string) (string, bool) {
			main, cd := SplitCond(rest(a, 1))
			what := map[string]string{
				"on":             x.t("Persistencia: asocia y busca por ", "Persistence: stores and looks up by "),
				"match":          x.t("Persistencia: busca al servidor por ", "Persistence: looks up the server by "),
				"store-request":  x.t("Persistencia: guarda en la petición ", "Persistence: stores from the request "),
				"store-response": x.t("Persistencia: guarda de la respuesta ", "Persistence: stores from the response "),
			}[arg(a, 0)]
			if what == "" {
				return x.t("Regla de persistencia: ", "Persistence rule: ") + c(strings.Join(a, " ")), false
			}
			return what + c(arg(main, 0)) + x.condSuffix(cd), true
		},
		"hold": func(x tr, a []string, _ string) (string, bool) {
			what := map[string]string{
				"valid":    x.t("respuestas válidas", "valid responses"),
				"nx":       x.t("respuestas NXDOMAIN (no existe)", "NXDOMAIN responses (no such name)"),
				"obsolete": x.t("direcciones obsoletas", "obsolete addresses"),
				"other":    x.t("otras respuestas", "other responses"),
				"refused":  x.t("respuestas REFUSED", "REFUSED responses"),
				"timeout":  x.t("consultas sin respuesta", "queries without an answer"),
			}[arg(a, 0)]
			if what == "" {
				return x.t("Retención DNS: ", "DNS hold: ") + c(strings.Join(a, " ")), false
			}
			return x.t("Conserva las ", "Keeps ") + what + x.t(" durante ", " for ") + x.dur(arg(a, 1), "ms"), true
		},
		"capture": func(x tr, a []string, _ string) (string, bool) {
			switch {
			case arg(a, 0) == "cookie":
				return x.f("Captura la cookie %s en el log (hasta %s caracteres)", "Captures the cookie %s in the log (up to %s characters)", c(arg(a, 1)), b(arg(a, 3))), true
			case (arg(a, 0) == "request" || arg(a, 0) == "response") && arg(a, 1) == "header":
				return x.f("Captura la cabecera %[1]s de la %[3]s en el log (hasta %[2]s caracteres)", "Captures the %[3]s header %[1]s in the log (up to %[2]s characters)", c(arg(a, 2)), b(arg(a, 4)), map[string]string{"request": x.t("petición", "request"), "response": x.t("respuesta", "response")}[arg(a, 0)]), true
			}
			return x.t("Captura para el log: ", "Log capture: ") + c(strings.Join(a, " ")), false
		},
		"compression": func(x tr, a []string, _ string) (string, bool) {
			r := strings.Join(rest(a, 1), " ")
			switch arg(a, 0) {
			case "algo":
				return x.t("Algoritmos de compresión: ", "Compression algorithms: ") + c(r), true
			case "type":
				return x.t("Comprime solo estos tipos de contenido: ", "Compresses only these content types: ") + c(r), true
			case "offload":
				return x.t("HAProxy comprime y quita la cabecera Accept-Encoding hacia el servidor", "HAProxy compresses and strips Accept-Encoding towards the server"), true
			case "direction":
				return x.t("Dirección de la compresión: ", "Compression direction: ") + c(r), true
			}
			return x.t("Compresión: ", "Compression: ") + c(strings.Join(a, " ")), false
		},
		"filter": func(x tr, a []string, _ string) (string, bool) {
			what := map[string]string{
				"compression": x.t("compresión de respuestas", "response compression"),
				"trace":       x.t("trazas de depuración del flujo", "stream debug traces"),
				"spoe":        x.t("agente externo SPOE", "SPOE external agent"),
				"cache":       x.t("caché HTTP", "HTTP cache"),
				"fcgi-app":    "FastCGI",
				"bwlim-in":    x.t("límite de ancho de banda de entrada", "inbound bandwidth limit"),
				"bwlim-out":   x.t("límite de ancho de banda de salida", "outbound bandwidth limit"),
			}[arg(a, 0)]
			if what == "" {
				return x.t("Filtro: ", "Filter: ") + c(strings.Join(a, " ")), false
			}
			return x.t("Filtro de ", "Filter for ") + what, true
		},
		"http-reuse": func(x tr, a []string, _ string) (string, bool) {
			what := map[string]string{
				"never":      x.t("nunca reutiliza conexiones entre clientes", "never reuses connections between clients"),
				"safe":       x.t("reutiliza solo tras la primera petición (seguro)", "reuses only after the first request (safe)"),
				"aggressive": x.t("reutiliza siempre que hay una conexión libre", "reuses whenever an idle connection exists"),
				"always":     x.t("reutiliza incluso la primera petición", "reuses even for the first request"),
			}[arg(a, 0)]
			if what == "" {
				return x.t("Reutilización de conexiones: ", "Connection reuse: ") + c(strings.Join(a, " ")), false
			}
			return x.t("Reutilización de conexiones: ", "Connection reuse: ") + what, true
		},
		"rate-limit": func(x tr, a []string, _ string) (string, bool) {
			if arg(a, 0) == "sessions" {
				return x.f("Limita las sesiones nuevas a %s por segundo", "Limits new sessions to %s per second", b(arg(a, 1))), true
			}
			return x.t("Límite de tasa: ", "Rate limit: ") + c(strings.Join(a, " ")), false
		},
		"http-error": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Respuesta personalizada para el estado %s", "Custom response for status %s", b(kv(a, "status"))), true
		},
		"peer": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Réplica %s en %s", "Replica %s at %s", b(arg(a, 0)), c(arg(a, 1))), true
		},
		"mailer": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Servidor de correo %s en %s", "Mail server %s at %s", b(arg(a, 0)), c(arg(a, 1))), true
		},
		"external-check": func(x tr, a []string, s string) (string, bool) {
			switch arg(a, 0) {
			case "command":
				return x.t("Comando que se ejecuta como health check: ", "Command run as a health check: ") + c(arg(a, 1)), true
			case "path":
				return x.t("PATH que ven los checks externos: ", "PATH seen by external checks: ") + c(arg(a, 1)), true
			}
			if s == "global" {
				return x.t("Permite ejecutar checks externos (comandos del sistema)", "Allows running external checks (system commands)"), true
			}
			return x.t("Checks externos: ", "External checks: ") + c(strings.Join(a, " ")), false
		},
		"nameserver": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Servidor DNS %s en %s", "DNS server %s at %s", b(arg(a, 0)), c(arg(a, 1))), true
		},
		"nbthread": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Usa %s hilos de trabajo", "Uses %s worker threads", b(arg(a, 0))), true
		},
		"hard-stop-after": func(x tr, a []string, _ string) (string, bool) {
			return x.f("Al recargar, corta las conexiones antiguas tras %s como máximo", "On reload, cuts the old connections after at most %s", x.dur(arg(a, 0), "ms")), true
		},
		"ssl-default-bind-options": func(x tr, a []string, _ string) (string, bool) {
			return x.t("Opciones TLS por defecto en los bind: ", "Default TLS options on binds: ") + x.tlsOpts(a), true
		},
		"ssl-default-server-options": func(x tr, a []string, _ string) (string, bool) {
			return x.t("Opciones TLS por defecto hacia servidores: ", "Default TLS options towards servers: ") + x.tlsOpts(a), true
		},
		"description": func(x tr, a []string, _ string) (string, bool) {
			return x.t("Descripción: ", "Description: ") + strings.Join(a, " "), true
		},
	}
}

var reGlobalSetting = regexp.MustCompile(`^(ssl-|quic|httpclient\.|h1-|h2-|ocsp)`)

// Describe explains a directive in the context of its section type, in the
// default language. For another language, use DescribeLang.
func Describe(l *Line, section string) Explanation { return DescribeLang(l, section, LangDefault) }

// DescribeLang is Describe in the requested language.
func DescribeLang(l *Line, section string, lang Lang) Explanation {
	x := tr{lang}
	info, known := KB.Directives[l.Keyword]
	ex := Explanation{Category: "misc", Known: known}
	if known {
		ex.Category = info.Category
	}
	if f, ok := describers[l.Keyword]; ok {
		text, k := f(x, l.Args, section)
		ex.Text = text
		if !k {
			ex.Known = false
		}
	}
	if desc := x.kb().Directives[l.Keyword]; ex.Text == "" && known && desc != "" {
		ex.Text = desc
		if strings.HasSuffix(desc, ":") && len(l.Args) > 0 {
			ex.Text += " " + c(strings.Join(l.Args, " "))
		}
	}
	if ex.Text == "" && strings.HasPrefix(l.Keyword, "tune.") {
		ex = Explanation{Text: x.t("Ajuste fino interno de rendimiento (", "Internal performance tuning (") + c(l.Keyword[5:]) + " = " + c(strings.Join(l.Args, " ")) + ")", Category: "perf", Known: true}
	}
	if ex.Text == "" && reGlobalSetting.MatchString(l.Keyword) {
		cat := "misc"
		if strings.HasPrefix(l.Keyword, "ssl") {
			cat = "tls"
		}
		ex = Explanation{Text: x.t("Ajuste global ", "Global setting ") + c(l.Keyword), Category: cat, Known: true}
	}
	if ex.Text == "" {
		ex = Explanation{Text: x.t("Directiva que haro no reconoce (puede ser válida en tu versión; revisa la documentación).", "Directive that haro does not recognize (it may be valid in your version; check the documentation)."), Category: "misc", Known: false}
	}
	return ex
}

// DisplayArgs returns the arguments as written, hiding
// passwords.
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
