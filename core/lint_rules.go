package core

import (
	"fmt"
	"regexp"
	"strings"
)

// Security, best-practice and version detector rules. They are
// run after LintLang's structural rules.

var reWeakUserPass = regexp.MustCompile(`(?i)^(test|admin|password|haproxy|changeme|cambiame|cambiar|123456|1234|12345678|secret|root|pass|qwerty|letmein)$`)

// internalPorts are ports of services that should almost never be open
// to the whole Internet.
var internalPorts = map[string]string{
	"1433": "SQL Server", "3306": "MySQL/MariaDB", "5432": "PostgreSQL", "6379": "Redis",
	"9200": "Elasticsearch", "9300": "Elasticsearch", "11211": "Memcached", "27017": "MongoDB",
}

var legacyTLS = map[string]bool{"sslv3": true, "tlsv1.0": true, "tlsv1.1": true, "force-sslv3": true, "force-tlsv10": true, "force-tlsv11": true}

// bindAddrs returns the host and port of each address on a bind line.
// Unix sockets and descriptors have no host or port.
func bindAddrs(arg0 string) (out []struct{ Host, Port string }) {
	for _, a := range strings.Split(arg0, ",") {
		if i := strings.Index(a, "@"); i > 0 {
			switch strings.ToLower(a[:i]) {
			case "unix", "abns", "abnsz", "fd", "sockpair":
				continue
			}
			a = a[i+1:]
		}
		if strings.HasPrefix(a, "/") {
			continue
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
		out = append(out, struct{ Host, Port string }{host, port})
	}
	return
}

func isPublicHost(h string) bool {
	switch h {
	case "", "*", "0.0.0.0", "::", ":::":
		return true
	}
	return false
}

// publicBind reports whether any address of the bind line listens on all
// interfaces. If ports is not nil, the port must also be in the map.
func publicBind(l *Line, ports map[string]string) (port string, ok bool) {
	for _, a := range bindAddrs(arg(l.Args, 0)) {
		if !isPublicHost(a.Host) {
			continue
		}
		if ports == nil {
			return a.Port, true
		}
		if _, hit := ports[a.Port]; hit {
			return a.Port, true
		}
	}
	return "", false
}

// restrictsBySource reports whether the section limits access by source IP or
// with authentication or deny rules.
func restrictsBySource(dirs []*Line) bool {
	for _, l := range dirs {
		switch l.Keyword {
		case "acl":
			if f := arg(l.Args, 1); f == "src" || strings.HasPrefix(f, "src_") || strings.HasPrefix(f, "src,") {
				return true
			}
		case "http-request":
			switch arg(l.Args, 0) {
			case "auth", "deny", "reject", "tarpit", "silent-drop", "allow":
				return true
			}
		case "tcp-request":
			if a := arg(l.Args, 1); a == "reject" || a == "silent-drop" || a == "accept" {
				return true
			}
		case "stats":
			switch arg(l.Args, 0) {
			case "auth", "admin", "http-request":
				return true
			}
		}
	}
	return false
}

// forwardforState says whether the section explicitly enables (on) or disables (off)
// option forwardfor, counting what is inherited from defaults.
func forwardforState(doc *Doc, eff []Effective, i int) (on, off bool) {
	scan := func(s *Section) {
		for _, l := range s.Directives() {
			if l.Keyword == "option" && arg(l.Args, 0) == "forwardfor" {
				on, off = true, false
			}
			if l.Keyword == "no" && arg(l.Args, 0) == "option" && arg(l.Args, 1) == "forwardfor" {
				on, off = false, true
			}
			if l.Keyword == "http-request" && strings.Contains(strings.ToLower(strings.Join(l.Args, " ")), "x-forwarded-for") {
				on = true
			}
		}
	}
	if d := eff[i].Defaults; d >= 0 && doc.Sections[i].Type != "defaults" {
		scan(doc.Sections[d])
	}
	scan(doc.Sections[i])
	return
}

func hasCheck(dirs []*Line) (servers int, checked bool) {
	for _, l := range dirs {
		switch l.Keyword {
		case "server", "server-template":
			servers++
			set := toSet(l.Args)
			if set["check"] || set["track"] || set["agent-check"] {
				checked = true
			}
		case "default-server":
			if toSet(l.Args)["check"] {
				checked = true
			}
		}
	}
	return
}

// lintExtra returns the problems of the security, best-practice
// and target-version rules.
func lintExtra(doc *Doc, eff []Effective, lang Lang, opts LintOptions) []Issue {
	var out []Issue
	add := func(rule string, sev Severity, s *Section, si, line int, es, en string, a ...any) {
		out = append(out, Issue{Rule: rule, File: s.File, Severity: sev, Line: line, Section: si, Message: fmt.Sprintf(lang.t(es, en), a...)})
	}
	target, hasTarget := ParseVersion(opts.HAProxyVersion)

	// frontends that point to each backend (for option forwardfor).
	feOf := map[string][]int{}
	for i, s := range doc.Sections {
		if s.Type != "frontend" && s.Type != "listen" {
			continue
		}
		for _, l := range s.Directives() {
			switch l.Keyword {
			case "default_backend":
				feOf[arg(l.Args, 0)] = append(feOf[arg(l.Args, 0)], i)
			case "use_backend":
				main, _ := SplitCond(l.Args)
				feOf[arg(main, 0)] = append(feOf[arg(main, 0)], i)
			}
		}
	}

	backendOf := map[string]*Section{}
	for _, s := range doc.Sections {
		if (s.Type == "backend" || s.Type == "listen") && s.Name != "" {
			backendOf[s.Name] = s
		}
	}

	for i, s := range doc.Sections {
		dirs := s.Directives()
		label := fmt.Sprintf(lang.t("%s «%s»", "%s “%s”"), s.Type, s.Name)

		if hasTarget {
			if info, ok := KB.Sections[s.Type]; ok && info.Since != "" {
				if v, ok := ParseVersion(info.Since); ok && target.Less(v) {
					add("newer-than-target", SevWarn, s, i, s.Line, "La sección «%s» requiere HAProxy ≥ %s (versión objetivo %s).", "The “%s” section requires HAProxy ≥ %s (target version %s).", s.Type, v, target)
				}
			}
			for _, l := range dirs {
				name, since := l.Keyword, KB.Directives[l.Keyword].Since
				if l.Keyword == "option" || (l.Keyword == "no" && arg(l.Args, 0) == "option") {
					o := arg(l.Args, 0)
					if l.Keyword == "no" {
						o = arg(l.Args, 1)
					}
					if v := KB.OptionSince[o]; v != "" {
						name, since = "option "+o, v
					}
				}
				if since == "" {
					continue
				}
				if v, ok := ParseVersion(since); ok && target.Less(v) {
					add("newer-than-target", SevWarn, s, i, l.Line, "«%s» requiere HAProxy ≥ %s (versión objetivo %s).", "“%s” requires HAProxy ≥ %s (target version %s).", name, v, target)
				}
			}
		}

		for _, l := range dirs {
			switch l.Keyword {
			case "bind", "server", "default-server", "ssl-default-bind-options", "ssl-default-server-options":
				for k := 0; k < len(l.Args); k++ {
					t := strings.ToLower(l.Args[k])
					if (t == "ssl-min-ver" && k+1 < len(l.Args) && legacyTLS[strings.ToLower(l.Args[k+1])]) || (strings.HasPrefix(t, "force-") && legacyTLS[t]) {
						v := t
						if t == "ssl-min-ver" {
							v = l.Args[k+1]
						}
						add("tls-legacy-protocol", SevWarn, s, i, l.Line, "«%s» permite un protocolo TLS obsoleto (%s). Usa TLSv1.2 o superior.", "“%s” allows an obsolete TLS protocol (%s). Use TLSv1.2 or later.", l.Keyword, v)
						break
					}
				}
			case "user":
				if s.Type == "userlist" {
					for k, t := range l.Args {
						if t == "insecure-password" && k+1 < len(l.Args) && reWeakUserPass.MatchString(l.Args[k+1]) {
							add("weak-userlist-password", SevWarn, s, i, l.Line, "El usuario «%s» tiene una contraseña de ejemplo o débil. Cámbiala.", "The user “%s” has a placeholder or weak password. Change it.", arg(l.Args, 0))
						}
					}
				}
			}
		}

		switch s.Type {
		case "frontend", "listen":
			// The restriction may be in the section itself or in a backend it sends to.
			protected := restrictsBySource(dirs)
			for _, l := range dirs {
				name := ""
				switch l.Keyword {
				case "default_backend":
					name = arg(l.Args, 0)
				case "use_backend":
					main, _ := SplitCond(l.Args)
					name = arg(main, 0)
				}
				if be := backendOf[name]; be != nil && restrictsBySource(be.Directives()) {
					protected = true
				}
			}
			statsOn := false
			for _, l := range dirs {
				if l.Keyword == "stats" && (arg(l.Args, 0) == "enable" || arg(l.Args, 0) == "uri") {
					statsOn = true
				}
			}
			var https, sslBind, acme bool
			var port80 *Line
			for _, l := range dirs {
				if strings.Contains(l.Text, ".well-known") {
					acme = true
				}
				if (l.Keyword == "redirect" || (l.Keyword == "http-request" && arg(l.Args, 0) == "redirect")) && strings.Contains(strings.ToLower(l.Text), "https") {
					https = true
				}
				if l.Keyword != "bind" {
					continue
				}
				set := toSet(l.Args)
				if set["ssl"] {
					sslBind = true
				}
				for _, a := range bindAddrs(arg(l.Args, 0)) {
					if a.Port == "80" && !set["ssl"] && port80 == nil {
						port80 = l
					}
				}
				if _, ok := publicBind(l, nil); ok && statsOn && !protected {
					add("stats-exposed", SevWarn, s, i, l.Line, "%s publica la página de estadísticas en una dirección pública sin autenticación ni restricción por IP.", "%s publishes the statistics page on a public address with no authentication or IP restriction.", label)
					statsOn = false // one warning per section
				}
				if p, ok := publicBind(l, internalPorts); ok && !protected && !(set["ssl"] && kv(l.Args, "verify") == "required") {
					add("internal-port-public", SevWarn, s, i, l.Line, "El puerto %s (%s) está abierto a todas las interfaces sin ACL por IP de origen.", "Port %s (%s) is open on all interfaces with no source-IP ACL.", p, internalPorts[p])
				}
			}
			if port80 != nil && sslBind && !https && !acme && eff[i].Mode == "http" {
				add("no-https-redirect", SevInfo, s, i, port80.Line, "%s escucha en el puerto 80 y en TLS, pero no redirige a HTTPS.", "%s listens on port 80 and on TLS, but does not redirect to HTTPS.", label)
			}
		}

		if (s.Type == "backend" || s.Type == "listen") && eff[i].Mode == "http" {
			if n, _ := hasCheck(dirs); n > 0 {
				// Only when some entry that arrives here terminates TLS: that is the case in
				// which the client IP is lost for good and the suggestion is beyond doubt.
				tls := s.Type == "listen" && terminatesTLS(s)
				on, off := forwardforState(doc, eff, i)
				if s.Type == "backend" {
					for _, fe := range feOf[s.Name] {
						if terminatesTLS(doc.Sections[fe]) {
							tls = true
						}
						if fon, _ := forwardforState(doc, eff, fe); fon {
							on = true
						}
					}
				}
				if tls && !on && !off {
					add("no-forwardfor", SevInfo, s, i, s.Line, "%s no añade X-Forwarded-For: los servidores verán la IP de HAProxy en vez de la del cliente.", "%s does not add X-Forwarded-For: servers will see HAProxy's IP instead of the client's.", label)
				}
			}
		}
		if s.Type == "backend" || s.Type == "listen" {
			n, checked := hasCheck(dirs)
			if d := eff[i].Defaults; d >= 0 && !checked {
				_, checked = hasCheck(doc.Sections[d].Directives())
			}
			if n >= 2 && !checked {
				add("no-health-check", SevInfo, s, i, s.Line, "%s tiene %d servidores y ninguno usa «check»: seguirá enviando tráfico a los que estén caídos.", "%s has %d servers and none uses “check”: it will keep sending traffic to the ones that are down.", label, n)
			}
		}
	}
	return out
}

// terminatesTLS reports whether the section has any bind with ssl.
func terminatesTLS(s *Section) bool {
	for _, l := range s.Directives() {
		if l.Keyword == "bind" && toSet(l.Args)["ssl"] {
			return true
		}
	}
	return false
}
