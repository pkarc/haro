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

// Plantillas por tecnología: configuraciones completas (frontends y backends)
// para publicar un servicio concreto (Nextcloud, PostgreSQL…) detrás de HAProxy.
// Son datos, no código: un JSON por tecnología en knowledge/templates/. Este
// archivo contiene el motor mínimo que los interpreta; no hay ningún motor de
// plantillas externo.
//
// Formato de un archivo (ver también CONTRIBUTING.md):
//
//	id, kind        «web» (HTTP con TLS; el motor genera los frontends) o «tcp»
//	name, description   {"es": …, "en": …}
//	params          parámetros: key, label, help, type (string|int|list|bool),
//	                default, required, dependsOn (otro parámetro bool anterior:
//	                solo se usa si es verdadero), example, validate
//	sections        secciones a generar: type, suffix (se añade al nombre base),
//	                if (condición) y lines
//
// Cada línea de una sección es un texto o un objeto con:
//
//	text      la directiva, con marcadores {{clave}}
//	if        condición: «clave», «!clave» o varias unidas con «&» (todas deben cumplirse);
//	          además de los parámetros vale «logging» (la configuración tiene un destino
//	          de logs, así que tiene sentido fijar el formato con option tcplog)
//	each      clave de un parámetro lista: se genera una línea por elemento
//	where     con each: «first» (solo el primer elemento) o «rest» (todos menos el primero)
//	comment   {"es": …, "en": …}: genera un comentario «# …» localizado
//
// En text valen los marcadores {{clave}} de cualquier parámetro, {{base}} (el
// nombre base ya sin colisiones) y, dentro de each, {{item}}, {{host}},
// {{port}} y {{n}} (posición desde 1). Para kind=web el motor añade los
// parámetros «domain» y «cert» y genera los frontends (puerto 80 con
// redirección a HTTPS y puerto 443 con TLS y ALPN) apuntando a {{base}}; el
// JSON solo describe el backend y lo extra.

//go:embed knowledge/templates/*.json
var techFS embed.FS

type i18n struct {
	ES string `json:"es"`
	EN string `json:"en"`
}

func (s i18n) get(l Lang) string {
	if l == LangEN && s.EN != "" {
		return s.EN
	}
	return s.ES
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

// UnmarshalJSON admite una cadena (solo texto) o un objeto.
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
		panic("haro: plantillas por tecnología inválidas: " + err.Error())
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
			return nil, fmt.Errorf("%s: el id «%s» debe coincidir con el nombre del archivo", e.Name(), d.ID)
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

// withCommonParams antepone a las plantillas web los parámetros comunes.
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

// validate comprueba que la plantilla está bien formada: es lo que hace
// fallar al arrancar (y en los tests) a una plantilla con un error de
// escritura, en vez de descubrirlo al usarla.
func (d *techDef) validate() error {
	if d.Kind != "web" && d.Kind != "tcp" {
		return fmt.Errorf("kind debe ser web o tcp, no «%s»", d.Kind)
	}
	if d.Name.ES == "" || d.Name.EN == "" || d.Description.ES == "" || d.Description.EN == "" {
		return fmt.Errorf("name y description necesitan es y en")
	}
	keys := map[string]*techParamDef{}
	for i := range d.Params {
		p := &d.Params[i]
		if _, dup := keys[p.Key]; dup || p.Key == "" {
			return fmt.Errorf("parámetro repetido o sin clave: «%s»", p.Key)
		}
		switch p.Type {
		case "string", "int", "list", "bool":
		default:
			return fmt.Errorf("parámetro %s: tipo «%s» desconocido", p.Key, p.Type)
		}
		if p.Label.ES == "" || p.Label.EN == "" {
			return fmt.Errorf("parámetro %s: label necesita es y en", p.Key)
		}
		if p.DependsOn != "" {
			dep, ok := keys[p.DependsOn]
			if !ok || dep.Type != "bool" {
				return fmt.Errorf("parámetro %s: dependsOn debe ser un bool declarado antes", p.Key)
			}
		}
		if p.Required && p.Default != nil {
			return fmt.Errorf("parámetro %s: un parámetro obligatorio no lleva valor por defecto", p.Key)
		}
		if p.Default != nil {
			if _, err := coerceParam(p, p.Default, LangES); err != nil {
				return fmt.Errorf("parámetro %s: valor por defecto inválido: %v", p.Key, err)
			}
		}
		keys[p.Key] = p
	}
	checkCond := func(c string) error {
		for _, k := range splitCond(c) {
			if _, ok := keys[k.key]; !ok && k.key != "logging" {
				return fmt.Errorf("la condición «%s» usa un parámetro que no existe", c)
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
			return fmt.Errorf("tipo de sección no permitido: «%s»", s.Type)
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
				return fmt.Errorf("línea vacía en la sección %s", s.Type)
			}
			if l.Each != "" {
				if p := keys[l.Each]; p == nil || p.Type != "list" {
					return fmt.Errorf("each «%s» no es un parámetro lista", l.Each)
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
					return fmt.Errorf("marcador {{%s}} desconocido en «%s»", k, l.Text)
				}
			}
		}
	}
	if !hasBackend {
		return fmt.Errorf("falta la sección backend sin suffix (la que recibe el tráfico)")
	}
	return nil
}

// ---------- API pública ----------

// TemplateParam describe un parámetro de una plantilla por tecnología, con los
// textos en el idioma pedido. Default es nil si no tiene valor por defecto;
// si no, un string, un número, un bool o una lista de strings.
type TemplateParam struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Help      string `json:"help,omitempty"`
	Type      string `json:"type"` // string | int | list | bool
	Default   any    `json:"defaultValue,omitempty"`
	Required  bool   `json:"required,omitempty"`
	DependsOn string `json:"dependsOn,omitempty"` // solo se usa si ese parámetro bool es verdadero
	Example   string `json:"example,omitempty"`
}

// TechTemplate son los metadatos de una plantilla por tecnología.
type TechTemplate struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"` // web | tcp
	Params      []TemplateParam `json:"params"`
}

// TechTemplates lista las plantillas por tecnología (ordenadas por id) con los
// textos en el idioma pedido.
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

// TemplateError es un error de los parámetros (o de la plantilla pedida), con
// el mensaje en el idioma de la petición. Param es la clave del parámetro
// afectado, si la hay.
type TemplateError struct {
	Param string `json:"param,omitempty"`
	Msg   string `json:"message"`
}

func (e *TemplateError) Error() string { return e.Msg }

func terr(param string, msg string) *TemplateError { return &TemplateError{Param: param, Msg: msg} }

// ---------- parámetros ----------

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

// checkString valida un valor de texto según el validador del parámetro.
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

// coerceParam convierte y valida el valor crudo de un parámetro. Admite los
// tipos que llegan de JSON (float64, []any) y de la línea de comandos (todo
// texto: las listas separadas por comas, espacios o saltos de línea).
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

// resolveParams valida los parámetros recibidos y devuelve el valor de cada
// uno (con los valores por defecto aplicados). Los parámetros cuyo dependsOn
// es falso se ignoran y valen su valor vacío.
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

// ---------- condiciones y sustitución ----------

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

// ---------- generación ----------

// genSection es una sección a crear: su nombre ya es definitivo.
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

// takenNames son los nombres de frontend, backend y listen existentes. Se
// tratan como un único espacio de nombres: HAProxy 3.3 ya no admite un
// frontend y un backend con el mismo nombre.
func takenNames(secs []*Section) map[string]bool {
	m := map[string]bool{}
	for _, s := range secs {
		if s.Name != "" && (s.Type == "frontend" || s.Type == "backend" || s.Type == "listen") {
			m[s.Name] = true
		}
	}
	return m
}

// pickBase busca el menor nombre base (id, id_2, id_3…) cuyos nombres derivados
// estén todos libres.
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

// findBind busca un frontend en modo http con un bind en ese puerto, con o sin
// TLS. Devuelve la sección y la línea del bind.
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

// redirectsToHTTPS indica si el frontend ya tiene una redirección a https.
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

// withCert devuelve el texto de un bind con ssl al que se le añade un
// certificado más. ok=false si no hace falta tocarlo (el certificado ya está o
// el bind carga un directorio de certificados).
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

// timeoutsMissing indica qué timeouts no están en el último defaults de la
// configuración (el que heredarán las secciones nuevas, que van al final).
func timeoutsMissing(secs []*Section) map[string]bool {
	miss := map[string]bool{"connect": true, "client": true, "server": true}
	var last *Section
	for _, s := range secs {
		if s.Type == "defaults" {
			last = s
		}
	}
	if last == nil {
		return map[string]bool{} // se añade un defaults con los tres
	}
	for _, l := range last.Lines {
		if l.Kind == KindDirective && l.Keyword == "timeout" {
			delete(miss, arg(l.Args, 0))
		}
	}
	return miss
}

// loggingConfigured indica si las secciones nuevas tendrán un destino de logs:
// fijar option httplog o tcplog sin él hace que HAProxy avise de que el
// formato se ignora. Si no hay defaults se añade el de la plantilla (con
// log global), así que depende de que el global tenga alguna línea log.
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

// ensureTimeouts añade a la sección los timeouts que le faltan, justo detrás del mode.
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

// RenderTechTemplate genera las operaciones de edición que añaden la plantilla
// con esos parámetros a la configuración existente (nil = vacía). Aplícalas
// con Apply. Los parámetros llevan valores tipados (string, número, bool,
// lista); desde la línea de comandos pueden llegar como texto.
//
// Hace lo siguiente:
//   - valida los parámetros y devuelve un *TemplateError en el idioma pedido;
//   - elige nombres que no colisionen con las secciones existentes (sufijos _2…);
//   - reutiliza el global y el defaults que ya existan y solo añade los que
//     falten; si el último defaults no tiene los timeouts, los pone en las
//     secciones nuevas;
//   - en las plantillas web, si ya hay un frontend http con TLS en el puerto 443
//     añade ahí la ACL por dominio y el use_backend (y el certificado al bind), en
//     vez de crear un segundo bind en el mismo puerto; lo mismo con la
//     redirección en el puerto 80.
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

	// Frontends de las plantillas web: crearlos o unirse a los que ya hay.
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
			if at == len(s.Lines) { // sin default_backend: al final del contenido, antes de los blancos
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

	// Las ops de unión llevan «{{base}}»: se sustituye cuando se conoce el nombre.
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
	// Comentario de procedencia al principio de la primera sección.
	gens[0].lines = append([]string{"# " + lang.t(
		fmt.Sprintf("%s: generado con la plantilla de haro", d.Name.get(lang)),
		fmt.Sprintf("%s: generated with the haro template", d.Name.get(lang)))}, gens[0].lines...)

	// global y defaults: solo los que falten, y detrás de las ops de unión
	// (que usan los índices originales).
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
		if gi < 0 { // el global nuevo está en la posición 0 del archivo 0
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
