package core

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Per-technology templates: complete configurations (frontends and backends)
// to publish a specific service (Nextcloud, PostgreSQL...) behind HAProxy.
// They are data, not code: one JSON per technology in knowledge/templates/. This
// file holds the minimal engine that interprets them; there is no external
// template engine.
//
// Format of a file (see also CONTRIBUTING.md):
//
//	id, kind        "web" (HTTP with TLS; the engine generates the frontends) or "tcp"
//	name, description   {"es": ..., "en": ...}
//	params          parameters: key, label, help, type (string|int|list|bool),
//	                default, required, dependsOn (another, earlier bool parameter:
//	                only used if true), example, validate
//	sections        sections to generate: type, suffix (appended to the base name),
//	                if (condition) and lines
//
// Each line of a section is a text or an object with:
//
//	text      the directive, with {{key}} placeholders
//	if        condition: "key", "!key" or several joined with "&" (all must hold);
//	          besides the parameters, "logging" is valid (the configuration has a log
//	          destination, so it makes sense to set the format with option tcplog)
//	each      key of a list parameter: one line is generated per element
//	where     with each: "first" (only the first element) or "rest" (all but the first)
//	comment   {"es": ..., "en": ...}: generates a localized "# ..." comment
//
// In text, the {{key}} placeholders of any parameter are valid, plus {{base}} (the
// base name already free of collisions) and, inside each, {{item}}, {{host}},
// {{port}} and {{n}} (position from 1). For kind=web the engine adds the
// parameters "domain" and "cert" and generates the frontends (port 80 with
// redirect to HTTPS and port 443 with TLS and ALPN) pointing at {{base}}; the
// JSON only describes the backend and the extras.

//go:embed knowledge/templates/*.json
var techFS embed.FS

type i18n struct {
	ES string `json:"es"`
	EN string `json:"en"`
}

func (s i18n) get(l Lang) string {
	if l == LangES && s.ES != "" {
		return s.ES
	}
	return s.EN
}

type techParamDef struct {
	Key       string `json:"key"`
	Label     i18n   `json:"label"`
	Help      i18n   `json:"help"`
	Type      string `json:"type"`
	Default   any    `json:"default"`
	Required  bool   `json:"required"`
	DependsOn string `json:"dependsOn"`
	Example   string `json:"example"`
	Validate  string `json:"validate"`
}

type techLine struct {
	Text    string
	If      string
	Each    string
	Where   string
	Comment *i18n
}

// UnmarshalJSON accepts a string (text only) or an object.
func (l *techLine) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = techLine{Text: s}
		return nil
	}
	var o struct {
		Text    string `json:"text"`
		If      string `json:"if"`
		Each    string `json:"each"`
		Where   string `json:"where"`
		Comment *i18n  `json:"comment"`
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return err
	}
	*l = techLine{Text: o.Text, If: o.If, Each: o.Each, Where: o.Where, Comment: o.Comment}
	return nil
}

type techSection struct {
	Type   string     `json:"type"`
	Suffix string     `json:"suffix"`
	If     string     `json:"if"`
	Lines  []techLine `json:"lines"`
}

type techDef struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Name        i18n           `json:"name"`
	Description i18n           `json:"description"`
	Params      []techParamDef `json:"params"`
	Sections    []techSection  `json:"sections"`
}

var techDefs = mustLoadTechDefs()

func mustLoadTechDefs() []*techDef {
	defs, err := loadTechDefs(techFS, "knowledge/templates")
	if err != nil {
		panic("haro: invalid technology templates: " + err.Error())
	}
	return defs
}

func loadTechDefs(fsys fs.FS, dir string) ([]*techDef, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var defs []*techDef
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		var d techDef
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if d.ID+".json" != e.Name() {
			return nil, fmt.Errorf("%s: the id “%s” must match the file name", e.Name(), d.ID)
		}
		d.Params = withCommonParams(&d)
		if err := d.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		defs = append(defs, &d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs, nil
}

// withCommonParams prepends the common parameters to the web templates.
func withCommonParams(d *techDef) []techParamDef {
	if d.Kind != "web" {
		return d.Params
	}
	common := []techParamDef{
		{Key: "domain", Type: "string", Required: true, Validate: "domain", Example: "app.example.com",
			Label: i18n{"Dominio público", "Public domain"},
			Help:  i18n{"Nombre con el que los usuarios acceden (sin https://).", "Name users connect with (without https://)."}},
		{Key: "cert", Type: "string", Required: true, Example: "/etc/haproxy/certs/app.example.com.pem",
			Label: i18n{"Certificado TLS (.pem)", "TLS certificate (.pem)"},
			Help:  i18n{"Ruta en el servidor de HAProxy del archivo .pem con certificado y clave privada.", "Path on the HAProxy host of the .pem file with the certificate and private key."}},
	}
	return append(common, d.Params...)
}

var placeholderRE = regexp.MustCompile(`\{\{([a-z_0-9]+)\}\}`)

// validate checks that the template is well formed: it is what makes
// a template with a typo fail at startup (and in tests),
// instead of discovering it on use.
func (d *techDef) validate() error {
	if d.Kind != "web" && d.Kind != "tcp" {
		return fmt.Errorf("kind must be web or tcp, not “%s”", d.Kind)
	}
	if d.Name.ES == "" || d.Name.EN == "" || d.Description.ES == "" || d.Description.EN == "" {
		return fmt.Errorf("name y description necesitan es y en")
	}
	keys := map[string]*techParamDef{}
	for i := range d.Params {
		p := &d.Params[i]
		if _, dup := keys[p.Key]; dup || p.Key == "" {
			return fmt.Errorf("duplicate or keyless parameter: “%s”", p.Key)
		}
		switch p.Type {
		case "string", "int", "list", "bool":
		default:
			return fmt.Errorf("parameter %s: unknown type “%s”", p.Key, p.Type)
		}
		if p.Label.ES == "" || p.Label.EN == "" {
			return fmt.Errorf("parameter %s: label needs es and en", p.Key)
		}
		if p.DependsOn != "" {
			dep, ok := keys[p.DependsOn]
			if !ok || dep.Type != "bool" {
				return fmt.Errorf("parameter %s: dependsOn must be a bool declared earlier", p.Key)
			}
		}
		if p.Required && p.Default != nil {
			return fmt.Errorf("parameter %s: a required parameter takes no default value", p.Key)
		}
		if p.Default != nil {
			if _, err := coerceParam(p, p.Default, LangDefault); err != nil {
				return fmt.Errorf("parameter %s: invalid default value: %v", p.Key, err)
			}
		}
		keys[p.Key] = p
	}
	checkCond := func(c string) error {
		for _, k := range splitCond(c) {
			if _, ok := keys[k.key]; !ok && k.key != "logging" {
				return fmt.Errorf("the condition “%s” uses a parameter that does not exist", c)
			}
		}
		return nil
	}
	if len(d.Sections) == 0 {
		return fmt.Errorf("no hay secciones")
	}
	hasBackend := false
	for _, s := range d.Sections {
		if s.Type != "frontend" && s.Type != "backend" && s.Type != "listen" {
			return fmt.Errorf("section type not allowed: “%s”", s.Type)
		}
		if s.Type == "backend" && s.Suffix == "" {
			hasBackend = true
		}
		if err := checkCond(s.If); err != nil {
			return err
		}
		for _, l := range s.Lines {
			if err := checkCond(l.If); err != nil {
				return err
			}
			if l.Comment != nil {
				if l.Comment.ES == "" || l.Comment.EN == "" || l.Text != "" {
					return fmt.Errorf("un comentario necesita es y en, y no lleva text")
				}
				continue
			}
			if strings.TrimSpace(l.Text) == "" {
				return fmt.Errorf("empty line in section %s", s.Type)
			}
			if l.Each != "" {
				if p := keys[l.Each]; p == nil || p.Type != "list" {
					return fmt.Errorf("each “%s” is not a list parameter", l.Each)
				}
				if l.Where != "" && l.Where != "first" && l.Where != "rest" {
					return fmt.Errorf("where debe ser first o rest")
				}
			} else if l.Where != "" {
				return fmt.Errorf("where solo se usa con each")
			}
			for _, m := range placeholderRE.FindAllStringSubmatch(l.Text, -1) {
				k := m[1]
				switch {
				case keys[k] != nil, k == "base":
				case l.Each != "" && (k == "item" || k == "host" || k == "port" || k == "n"):
				default:
					return fmt.Errorf("unknown marker {{%s}} in “%s”", k, l.Text)
				}
			}
		}
	}
	if !hasBackend {
		return fmt.Errorf("missing backend section without suffix (the one that receives traffic)")
	}
	return nil
}

// ---------- public API ----------

// TemplateParam describes a parameter of a per-technology template, with the
// texts in the requested language. Default is nil if it has no default value;
// otherwise a string, a number, a bool or a list of strings.
type TemplateParam struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Help      string `json:"help,omitempty"`
	Type      string `json:"type"` // string | int | list | bool
	Default   any    `json:"defaultValue,omitempty"`
	Required  bool   `json:"required,omitempty"`
	DependsOn string `json:"dependsOn,omitempty"` // only used if that bool parameter is true
	Example   string `json:"example,omitempty"`
}

// TechTemplate is the metadata of a per-technology template.
type TechTemplate struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"` // web | tcp
	Params      []TemplateParam `json:"params"`
}

// TechTemplates lists the per-technology templates (sorted by id) with the
// texts in the requested language.
func TechTemplates(lang Lang) []TechTemplate {
	out := make([]TechTemplate, len(techDefs))
	for i, d := range techDefs {
		t := TechTemplate{ID: d.ID, Name: d.Name.get(lang), Description: d.Description.get(lang), Kind: d.Kind, Params: make([]TemplateParam, len(d.Params))}
		for k := range d.Params {
			p := &d.Params[k]
			tp := TemplateParam{Key: p.Key, Label: p.Label.get(lang), Help: p.Help.get(lang), Type: p.Type, Required: p.Required, DependsOn: p.DependsOn, Example: p.Example}
			if p.Default != nil {
				tp.Default, _ = coerceParam(p, p.Default, lang)
			}
			t.Params[k] = tp
		}
		out[i] = t
	}
	return out
}

// TemplateError is an error in the parameters (or in the requested template), with
// the message in the request's language. Param is the key of the affected
// parameter, if any.
type TemplateError struct {
	Param string `json:"param,omitempty"`
	Msg   string `json:"message"`
}

func (e *TemplateError) Error() string { return e.Msg }

func terr(param string, msg string) *TemplateError { return &TemplateError{Param: param, Msg: msg} }

// ---------- parameters ----------

type paramVals map[string]any

var (
	tokenRE    = regexp.MustCompile(`^[^\s"'\\#{}]+$`)
	domainRE   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)
	timeRE     = regexp.MustCompile(`^[0-9]+(us|ms|s|m|h|d)?$`)
	hostRE     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	bindHostRE = regexp.MustCompile(`^(\*|[A-Za-z0-9._-]+)$`)
)

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535 && s == strconv.Itoa(n)
}

// checkString validates a text value according to the parameter's validator.
func checkString(p *techParamDef, v string, lang Lang) error {
	label := p.Label.get(lang)
	switch p.Validate {
	case "domain":
		if !domainRE.MatchString(v) {
			return terr(p.Key, lang.t(
				fmt.Sprintf("«%s» (%s) no es un dominio válido: usa letras, números, guiones y puntos, sin espacios ni «https://».", v, label),
				fmt.Sprintf("“%s” (%s) is not a valid domain: use letters, digits, hyphens and dots, with no spaces or “https://”.", v, label)))
		}
	case "time":
		if !timeRE.MatchString(v) {
			return terr(p.Key, lang.t(
				fmt.Sprintf("«%s» (%s) no es un tiempo válido: un número con unidad opcional (us, ms, s, m, h, d), como 30s o 10m.", v, label),
				fmt.Sprintf("“%s” (%s) is not a valid time: a number with an optional unit (us, ms, s, m, h, d), like 30s or 10m.", v, label)))
		}
	case "bind":
		host, port, ok := strings.Cut(v, ":")
		if !ok || !bindHostRE.MatchString(host) || !validPort(port) {
			return terr(p.Key, lang.t(
				fmt.Sprintf("«%s» (%s) no es una dirección de escucha válida: usa dirección:puerto, como 10.0.0.5:5432 o *:6443.", v, label),
				fmt.Sprintf("“%s” (%s) is not a valid listen address: use address:port, like 10.0.0.5:5432 or *:6443.", v, label)))
		}
	default: // token
		if !tokenRE.MatchString(v) {
			return terr(p.Key, lang.t(
				fmt.Sprintf("«%s» (%s) no puede estar vacío ni contener espacios, comillas, # ni llaves.", v, label),
				fmt.Sprintf("“%s” (%s) cannot be empty or contain spaces, quotes, # or braces.", v, label)))
		}
	}
	return nil
}

func checkHostPort(p *techParamDef, v string, lang Lang) error {
	label := p.Label.get(lang)
	if strings.Count(v, ":") > 1 || strings.ContainsAny(v, "[]") {
		return terr(p.Key, lang.t(
			fmt.Sprintf("«%s» (%s): las direcciones IPv6 no se admiten en las plantillas; usa un nombre de host o edita el resultado.", v, label),
			fmt.Sprintf("“%s” (%s): IPv6 addresses are not supported in templates; use a host name or edit the result.", v, label)))
	}
	host, port, ok := strings.Cut(v, ":")
	if !ok || !hostRE.MatchString(host) || !validPort(port) {
		return terr(p.Key, lang.t(
			fmt.Sprintf("«%s» (%s) no es un servidor válido: usa host:puerto, como 10.0.0.10:8080.", v, label),
			fmt.Sprintf("“%s” (%s) is not a valid server: use host:port, like 10.0.0.10:8080.", v, label)))
	}
	return nil
}

func zeroValue(p *techParamDef) any {
	switch p.Type {
	case "bool":
		return false
	case "int":
		return 0
	case "list":
		return []string{}
	}
	return ""
}

// coerceParam converts and validates a parameter's raw value. It accepts the
// types that arrive from JSON (float64, []any) and from the command line (all
// text: lists separated by commas, spaces or line breaks).
func coerceParam(p *techParamDef, raw any, lang Lang) (any, error) {
	label := p.Label.get(lang)
	bad := func(es, en string) error {
		return terr(p.Key, lang.t(fmt.Sprintf("%s (%s): %s", p.Key, label, es), fmt.Sprintf("%s (%s): %s", p.Key, label, en)))
	}
	switch p.Type {
	case "string":
		s, ok := raw.(string)
		if !ok {
			return nil, bad("debe ser texto.", "must be text.")
		}
		s = strings.TrimSpace(s)
		if err := checkString(p, s, lang); err != nil {
			return nil, err
		}
		return s, nil
	case "int":
		var n int
		switch v := raw.(type) {
		case int:
			n = v
		case int64:
			n = int(v)
		case float64:
			if v != float64(int(v)) {
				return nil, bad("debe ser un número entero.", "must be an integer.")
			}
			n = int(v)
		case string:
			x, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, bad("debe ser un número entero.", "must be an integer.")
			}
			n = x
		default:
			return nil, bad("debe ser un número entero.", "must be an integer.")
		}
		if n < 1 || n > 65535 {
			return nil, bad("debe estar entre 1 y 65535.", "must be between 1 and 65535.")
		}
		return n, nil
	case "bool":
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "yes", "y", "1", "on", "si", "sí":
				return true, nil
			case "false", "no", "n", "0", "off":
				return false, nil
			}
		}
		return nil, bad("debe ser verdadero o falso.", "must be true or false.")
	case "list":
		var items []string
		switch v := raw.(type) {
		case []string:
			items = v
		case []any:
			for _, x := range v {
				s, ok := x.(string)
				if !ok {
					return nil, bad("todos los elementos deben ser texto.", "all items must be text.")
				}
				items = append(items, s)
			}
		case string:
			items = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';' })
		default:
			return nil, bad("debe ser una lista.", "must be a list.")
		}
		out := make([]string, 0, len(items))
		for _, it := range items {
			it = strings.TrimSpace(it)
			if it == "" {
				continue
			}
			if err := checkHostPort(p, it, lang); err != nil {
				return nil, err
			}
			out = append(out, it)
		}
		return out, nil
	}
	return nil, bad("tipo desconocido.", "unknown type.")
}

func isEmptyRaw(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	}
	return false
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case []string:
		return len(x) > 0
	}
	return false
}

// resolveParams validates the received parameters and returns the value of each
// one (with defaults applied). Parameters whose dependsOn
// is false are ignored and take their empty value.
func resolveParams(d *techDef, in map[string]any, lang Lang) (paramVals, error) {
	known := map[string]bool{}
	for _, p := range d.Params {
		known[p.Key] = true
	}
	var unknown []string
	for k := range in {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		var valid []string
		for _, p := range d.Params {
			valid = append(valid, p.Key)
		}
		return nil, terr(unknown[0], lang.t(
			fmt.Sprintf("parámetro desconocido «%s» para la plantilla %s (válidos: %s).", unknown[0], d.ID, strings.Join(valid, ", ")),
			fmt.Sprintf("unknown parameter “%s” for template %s (valid: %s).", unknown[0], d.ID, strings.Join(valid, ", "))))
	}
	vals := paramVals{}
	for i := range d.Params {
		p := &d.Params[i]
		if p.DependsOn != "" && !truthy(vals[p.DependsOn]) {
			vals[p.Key] = zeroValue(p)
			continue
		}
		raw, given := in[p.Key]
		if !given || isEmptyRaw(raw) {
			switch {
			case p.Default != nil:
				raw = p.Default
			case p.Required:
				return nil, terr(p.Key, lang.t(
					fmt.Sprintf("falta el parámetro «%s» (%s).", p.Key, p.Label.get(lang)),
					fmt.Sprintf("missing parameter “%s” (%s).", p.Key, p.Label.get(lang))))
			default:
				vals[p.Key] = zeroValue(p)
				continue
			}
		}
		v, err := coerceParam(p, raw, lang)
		if err != nil {
			return nil, err
		}
		if p.Required && p.Type == "list" && len(v.([]string)) == 0 {
			return nil, terr(p.Key, lang.t(
				fmt.Sprintf("falta el parámetro «%s» (%s): indica al menos un elemento.", p.Key, p.Label.get(lang)),
				fmt.Sprintf("missing parameter “%s” (%s): give at least one item.", p.Key, p.Label.get(lang))))
		}
		vals[p.Key] = v
	}
	return vals, nil
}

// ---------- conditions and substitution ----------

type condTerm struct {
	key string
	neg bool
}

func splitCond(c string) []condTerm {
	if c == "" {
		return nil
	}
	var out []condTerm
	for _, t := range strings.Split(c, "&") {
		t = strings.TrimSpace(t)
		neg := strings.HasPrefix(t, "!")
		out = append(out, condTerm{strings.TrimPrefix(t, "!"), neg})
	}
	return out
}

func (v paramVals) holds(c string) bool {
	for _, t := range splitCond(c) {
		if truthy(v[t.key]) == t.neg {
			return false
		}
	}
	return true
}

func substitute(text string, vals paramVals, extra map[string]string) string {
	return placeholderRE.ReplaceAllStringFunc(text, func(m string) string {
		k := m[2 : len(m)-2]
		if s, ok := extra[k]; ok {
			return s
		}
		switch x := vals[k].(type) {
		case string:
			return x
		case int:
			return strconv.Itoa(x)
		case bool:
			return strconv.FormatBool(x)
		case []string:
			return strings.Join(x, ",")
		}
		return ""
	})
}

// ---------- generation ----------

// genSection is a section to create: its name is already final.
type genSection struct {
	typ, name string
	lines     []string
}

func (d *techDef) lineTexts(s techSection, vals paramVals, base string, lang Lang) []string {
	var out []string
	for _, l := range s.Lines {
		if !vals.holds(l.If) {
			continue
		}
		switch {
		case l.Comment != nil:
			out = append(out, "# "+l.Comment.get(lang))
		case l.Each != "":
			items := vals[l.Each].([]string)
			for i, it := range items {
				if (l.Where == "first" && i != 0) || (l.Where == "rest" && i == 0) {
					continue
				}
				host, port, _ := strings.Cut(it, ":")
				out = append(out, substitute(l.Text, vals, map[string]string{
					"base": base, "item": it, "host": host, "port": port, "n": strconv.Itoa(i + 1)}))
			}
		default:
			out = append(out, substitute(l.Text, vals, map[string]string{"base": base}))
		}
	}
	return out
}

// takenNames are the existing frontend, backend and listen names. They are
// treated as a single namespace: HAProxy 3.3 no longer allows a
// frontend and a backend with the same name.
func takenNames(secs []*Section) map[string]bool {
	m := map[string]bool{}
	for _, s := range secs {
		if s.Name != "" && (s.Type == "frontend" || s.Type == "backend" || s.Type == "listen") {
			m[s.Name] = true
		}
	}
	return m
}

// pickBase finds the smallest base name (id, id_2, id_3...) whose derived names
// are all free.
func pickBase(id string, suffixes []string, taken map[string]bool) string {
	for k := 1; ; k++ {
		base := id
		if k > 1 {
			base = fmt.Sprintf("%s_%d", id, k)
		}
		free := true
		for _, s := range suffixes {
			if taken[base+s] {
				free = false
				break
			}
		}
		if free {
			return base
		}
	}
}

func hasBindPort(l *Line, port string) bool {
	for _, a := range bindAddrs(arg(l.Args, 0)) {
		if a.Port == port {
			return true
		}
	}
	return false
}

// findBind finds an http-mode frontend with a bind on that port, with or without
// TLS. It returns the section and the bind's line.
func findBind(doc *Doc, eff []Effective, port string, ssl bool) (si, li int, ok bool) {
	for si, s := range doc.Sections {
		if s.Type != "frontend" || eff[si].Mode != "http" {
			continue
		}
		for li, l := range s.Lines {
			if l.Kind == KindDirective && l.Keyword == "bind" && hasBindPort(l, port) && toSet(l.Args)["ssl"] == ssl {
				return si, li, true
			}
		}
	}
	return 0, 0, false
}

// redirectsToHTTPS reports whether the frontend already has a redirect to https.
func redirectsToHTTPS(s *Section) bool {
	for _, l := range s.Lines {
		if l.Kind != KindDirective {
			continue
		}
		args := l.Args
		if l.Keyword == "http-request" && arg(args, 0) == "redirect" {
			args = args[1:]
		} else if l.Keyword != "redirect" {
			continue
		}
		for i, a := range args {
			if a == "scheme" && arg(args, i+1) == "https" {
				return true
			}
		}
	}
	return false
}

// withCert returns the text of an ssl bind with one more certificate
// added. ok=false if it does not need touching (the certificate is already there or
// the bind loads a certificate directory).
func withCert(l *Line, cert string) (text string, ok bool) {
	args := l.Args
	last := -1
	for i, a := range args {
		if a == "crt" && i+1 < len(args) {
			if args[i+1] == cert || strings.HasSuffix(args[i+1], "/") {
				return "", false
			}
			last = i + 1
		}
	}
	text = strings.TrimSpace(strings.TrimSuffix(l.Text, l.Comment))
	if last >= 0 {
		if at := strings.Index(text, args[last]); at >= 0 {
			text = text[:at+len(args[last])] + " crt " + QuoteToken(cert) + text[at+len(args[last]):]
		} else {
			text += " crt " + QuoteToken(cert)
		}
	} else {
		text += " crt " + QuoteToken(cert)
	}
	if l.Comment != "" {
		text += "  " + l.Comment
	}
	return text, true
}

// timeoutsMissing reports which timeouts are not in the last defaults of the
// configuration (the one the new sections will inherit, which go at the end).
func timeoutsMissing(secs []*Section) map[string]bool {
	miss := map[string]bool{"connect": true, "client": true, "server": true}
	var last *Section
	for _, s := range secs {
		if s.Type == "defaults" {
			last = s
		}
	}
	if last == nil {
		return map[string]bool{} // a defaults with all three is added
	}
	for _, l := range last.Lines {
		if l.Kind == KindDirective && l.Keyword == "timeout" {
			delete(miss, arg(l.Args, 0))
		}
	}
	return miss
}

// loggingConfigured reports whether the new sections will have a log destination:
// setting option httplog or tcplog without one makes HAProxy warn that the
// format is ignored. If there are no defaults, the template's is added (with
// log global), so it depends on the global having some log line.
func loggingConfigured(secs []*Section) bool {
	var global, last *Section
	for _, s := range secs {
		switch s.Type {
		case "global":
			if global == nil {
				global = s
			}
		case "defaults":
			last = s
		}
	}
	has := func(s *Section, global bool) bool {
		for _, l := range s.Lines {
			if l.Kind == KindDirective && l.Keyword == "log" && (!global || arg(l.Args, 0) != "global") {
				return true
			}
		}
		return false
	}
	globalLogs := global == nil || has(global, false)
	if last == nil {
		return globalLogs
	}
	for _, l := range last.Lines {
		if l.Kind == KindDirective && l.Keyword == "log" {
			return arg(l.Args, 0) != "global" || globalLogs
		}
	}
	return false
}

// ensureTimeouts adds to the section the timeouts it lacks, right after the mode.
func ensureTimeouts(g *genSection, miss map[string]bool) {
	has := func(name string) bool {
		for _, l := range g.lines {
			if strings.HasPrefix(l, "timeout "+name+" ") {
				return true
			}
		}
		return false
	}
	var add []string
	switch g.typ {
	case "frontend":
		if miss["client"] && !has("client") {
			add = append(add, "timeout client 30s")
		}
	case "backend":
		if miss["connect"] && !has("connect") {
			add = append(add, "timeout connect 5s")
		}
		if miss["server"] && !has("server") {
			add = append(add, "timeout server 30s")
		}
	}
	if len(add) == 0 {
		return
	}
	at := 0
	for i, l := range g.lines {
		if strings.HasPrefix(l, "mode ") {
			at = i + 1
			break
		}
	}
	g.lines = append(g.lines[:at:at], append(add, g.lines[at:]...)...)
}

func ptr(i int) *int { return &i }

// RenderTechTemplate generates the edit operations that add the template
// with those parameters to the existing configuration (nil = empty). Apply them
// with Apply. Parameters carry typed values (string, number, bool,
// list); from the command line they may arrive as text.
//
// It does the following:
//   - validates the parameters and returns a *TemplateError in the requested language;
//   - picks names that do not collide with the existing sections (suffixes _2...);
//   - reuses the global and defaults that already exist and only adds those
//     that are missing; if the last defaults lacks the timeouts, it puts them in the
//     new sections;
//   - in web templates, if there is already an http frontend with TLS on port 443
//     it adds the domain ACL and the use_backend there (and the certificate to the bind),
//     instead of creating a second bind on the same port; the same for the
//     redirect on port 80.
func RenderTechTemplate(id string, params map[string]any, existing *Config, lang Lang) ([]Op, error) {
	var d *techDef
	for _, x := range techDefs {
		if x.ID == id {
			d = x
		}
	}
	if d == nil {
		var ids []string
		for _, x := range techDefs {
			ids = append(ids, x.ID)
		}
		return nil, terr("", lang.t(
			fmt.Sprintf("plantilla desconocida «%s» (disponibles: %s).", id, strings.Join(ids, ", ")),
			fmt.Sprintf("unknown template “%s” (available: %s).", id, strings.Join(ids, ", "))))
	}
	vals, err := resolveParams(d, params, lang)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		existing = NewConfig([]Source{{Path: "haproxy.cfg"}})
	}
	doc := existing.Merge()
	secs := doc.Sections
	eff := ComputeEffective(doc)
	logging := loggingConfigured(secs)
	vals["logging"] = logging

	var ops []Op

	// Frontends of the web templates: create them or join the existing ones.
	var newFrontends []techSection
	if d.Kind == "web" {
		domain := vals["domain"].(string)
		cert := vals["cert"].(string)
		base := "{{base}}"
		if si, li, ok := findBind(doc, eff, "443", true); ok {
			s := secs[si]
			if text, ok := withCert(s.Lines[li], cert); ok {
				ops = append(ops, Op{Op: OpUpdateDirective, Section: si, LineIndex: li, Text: text})
			}
			at := len(s.Lines)
			for i, l := range s.Lines {
				if l.Kind == KindDirective && l.Keyword == "default_backend" {
					at = i
					break
				}
			}
			if at == len(s.Lines) { // no default_backend: at the end of the content, before the blanks
				for at > 0 && s.Lines[at-1].Kind == KindBlank {
					at--
				}
			}
			ops = append(ops,
				Op{Op: OpAddDirective, Section: si, At: ptr(at), Text: "acl host_{{base}} hdr(host),field(1,:) -i " + domain},
				Op{Op: OpAddDirective, Section: si, At: ptr(at + 1), Text: "use_backend {{base}} if host_{{base}}"})
		} else {
			newFrontends = append(newFrontends, techSection{Type: "frontend", Suffix: "_https", Lines: []techLine{
				{Text: "bind *:443 ssl crt " + QuoteToken(cert) + " alpn h2,http/1.1"},
				{Text: "mode http"}, {Text: "option httplog", If: "logging"}, {Text: "default_backend " + base}}})
		}
		if si, _, ok := findBind(doc, eff, "80", false); ok {
			if !redirectsToHTTPS(secs[si]) {
				ops = append(ops, Op{Op: OpAddDirective, Section: si, Text: "http-request redirect scheme https code 301 if { hdr(host),field(1,:) -i " + domain + " }"})
			}
		} else {
			newFrontends = append([]techSection{{Type: "frontend", Suffix: "_http", Lines: []techLine{
				{Text: "bind *:80"}, {Text: "mode http"}, {Text: "option httplog", If: "logging"},
				{Text: "http-request redirect scheme https code 301 unless { ssl_fc }"}}}}, newFrontends...)
		}
	}

	// The join ops carry "{{base}}": it is substituted once the name is known.
	var plan []techSection
	plan = append(plan, newFrontends...)
	for _, s := range d.Sections {
		if vals.holds(s.If) {
			plan = append(plan, s)
		}
	}
	var suffixes []string
	for _, s := range plan {
		suffixes = append(suffixes, s.Suffix)
	}
	base := pickBase(d.ID, suffixes, takenNames(secs))
	for i := range ops {
		ops[i].Text = strings.ReplaceAll(ops[i].Text, "{{base}}", base)
	}

	var gens []genSection
	for _, s := range plan {
		g := genSection{typ: s.Type, name: base + s.Suffix}
		g.lines = d.lineTexts(s, vals, base, lang)
		gens = append(gens, g)
	}
	miss := timeoutsMissing(secs)
	for i := range gens {
		ensureTimeouts(&gens[i], miss)
	}
	// Provenance comment at the start of the first section.
	gens[0].lines = append([]string{"# " + lang.t(
		fmt.Sprintf("%s: generado con la plantilla de haro", d.Name.get(lang)),
		fmt.Sprintf("%s: generated with the haro template", d.Name.get(lang)))}, gens[0].lines...)

	// global and defaults: only those that are missing, and after the join ops
	// (which use the original indices).
	gi, di := -1, -1
	for i, s := range secs {
		if s.Type == "global" && gi < 0 {
			gi = i
		}
		if s.Type == "defaults" && di < 0 {
			di = i
		}
	}
	added := 0
	if gi < 0 {
		ops = append(ops, Op{Op: OpAddSection, Type: "global", Template: true})
		added++
	}
	if di < 0 {
		op := Op{Op: OpAddSection, Type: "defaults", Template: true}
		if gi < 0 { // the new global is at position 0 of file 0
			op.File, op.At = ptr(0), ptr(1)
		} else {
			op.File, op.At = ptr(secs[gi].File), ptr(gi+1)
		}
		ops = append(ops, op)
		added++
	}

	cursor := len(secs) + added
	for _, g := range gens {
		ops = append(ops, Op{Op: OpAddSection, Type: g.typ, Name: g.name, At: ptr(cursor)})
		for _, l := range g.lines {
			ops = append(ops, Op{Op: OpAddDirective, Section: cursor, Text: l})
		}
		cursor++
	}
	return ops, nil
}
