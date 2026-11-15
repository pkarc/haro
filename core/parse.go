package core

import (
	"strings"
)

// DefaultIndent es la indentación que reciben las líneas nuevas.
const DefaultIndent = "    "

// Tokenize divide una línea (sin indentación) en tokens respetando comillas
// simples y dobles, escapes con barra invertida y comentarios con '#'.
func Tokenize(s string) (tokens []string, comment string) {
	var cur strings.Builder
	has := false
	var quote rune
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		if quote != 0 {
			if ch == '\\' && quote == '"' && i+1 < len(rs) {
				cur.WriteRune(rs[i+1])
				i++
				continue
			}
			if ch == quote {
				quote = 0
				continue
			}
			cur.WriteRune(ch)
			continue
		}
		switch {
		case ch == '"' || ch == '\'':
			quote = ch
			has = true
		case ch == '\\' && i+1 < len(rs):
			cur.WriteRune(rs[i+1])
			has = true
			i++
		case ch == '#' && !has:
			return tokens, string(rs[i:])
		case ch == ' ' || ch == '\t':
			if has {
				tokens = append(tokens, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(ch)
			has = true
		}
	}
	if has {
		tokens = append(tokens, cur.String())
	}
	return tokens, ""
}

// ParseLine interpreta una sola línea. Si es una cabecera de sección devuelve
// la sección (sin líneas) y nil como línea.
func ParseLine(raw string, lineNo int) (*Line, *Section) {
	raw = strings.TrimRight(raw, " \t\r")
	trimmed := strings.TrimLeft(raw, " \t")
	indent := raw[:len(raw)-len(trimmed)]
	if trimmed == "" {
		return &Line{Kind: KindBlank, Line: lineNo}, nil
	}
	if strings.HasPrefix(trimmed, "#") {
		return &Line{Kind: KindComment, Line: lineNo, Indent: indent, IndentSet: true, Text: trimmed}, nil
	}
	tokens, comment := Tokenize(trimmed)
	kw := ""
	if len(tokens) > 0 {
		kw = strings.ToLower(tokens[0])
	}
	if _, ok := Sections[kw]; ok && (indent == "" || proxySections[kw]) {
		name := ""
		if len(tokens) > 1 {
			name = tokens[1]
		}
		return nil, &Section{Type: kw, Name: name, Line: lineNo, Indent: indent, Text: trimmed, Tokens: tokens, Comment: comment}
	}
	args := []string{}
	if len(tokens) > 1 {
		args = tokens[1:]
	}
	return &Line{Kind: KindDirective, Line: lineNo, Indent: indent, IndentSet: true, Text: trimmed, Keyword: kw, Args: args, Comment: comment}, nil
}

// Parse convierte el texto en un Doc sin perder comentarios, líneas en blanco
// ni indentación.
func Parse(text string) *Doc {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	doc := &Doc{Pre: []*Line{}, Sections: []*Section{}}
	var cur *Section
	for i, raw := range strings.Split(text, "\n") {
		l, s := ParseLine(raw, i+1)
		if s != nil {
			s.Lines = []*Line{}
			doc.Sections = append(doc.Sections, s)
			cur = s
			continue
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, l)
		} else {
			doc.Pre = append(doc.Pre, l)
		}
	}
	return doc
}

// Serialize vuelve a escribir el Doc. Para un Doc recién parseado,
// Serialize(Parse(t)) == t salvo espacios al final de línea y \r.
func Serialize(doc *Doc) string {
	var b strings.Builder
	first := true
	write := func(s string) {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(s)
	}
	line := func(l *Line, def string) string {
		if l.Kind == KindBlank {
			return ""
		}
		ind := def
		if l.IndentSet {
			ind = l.Indent
		}
		return ind + l.Text
	}
	for _, l := range doc.Pre {
		write(line(l, ""))
	}
	for _, s := range doc.Sections {
		write(s.Indent + s.Text)
		for _, l := range s.Lines {
			write(line(l, DefaultIndent))
		}
	}
	return b.String()
}

// EnsureSeparation garantiza una línea en blanco al final de cada sección,
// útil tras mover, añadir o borrar secciones.
func EnsureSeparation(doc *Doc) {
	blank := func() *Line { return &Line{Kind: KindBlank} }
	if len(doc.Sections) > 0 && len(doc.Pre) > 0 && doc.Pre[len(doc.Pre)-1].Kind != KindBlank {
		for _, l := range doc.Pre {
			if l.Kind != KindBlank {
				doc.Pre = append(doc.Pre, blank())
				break
			}
		}
	}
	for _, s := range doc.Sections {
		if len(s.Lines) == 0 || s.Lines[len(s.Lines)-1].Kind != KindBlank {
			s.Lines = append(s.Lines, blank())
		}
	}
}

// QuoteToken entrecomilla un token si contiene espacios o caracteres especiales.
func QuoteToken(t string) string {
	if t == "" || strings.ContainsAny(t, " \t#\"'\\") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(t) + `"`
	}
	return t
}

// SplitCond separa los argumentos de una regla en la parte principal y la
// condición (tras `if` o `unless`).
func SplitCond(args []string) (main []string, cond *Cond) {
	for i, t := range args {
		if t == "if" || t == "unless" {
			return args[:i], &Cond{Negated: t == "unless", Tokens: args[i+1:]}
		}
	}
	return args, nil
}

// Cond es la condición de una regla: `if a !b || { src 10.0.0.0/8 }`.
type Cond struct {
	Negated bool     `json:"negated"`
	Tokens  []string `json:"tokens"`
}

// ACLNames devuelve los nombres de ACL usados en la condición, ignorando las
// ACL anónimas entre llaves y los operadores.
func (c *Cond) ACLNames() []string {
	var names []string
	depth := 0
	for _, tok := range c.Tokens {
		if tok == "||" || strings.EqualFold(tok, "or") {
			continue
		}
		tok = strings.TrimPrefix(tok, "!")
		switch {
		case tok == "":
			continue
		case strings.HasPrefix(tok, "{"):
			depth++
			continue
		case tok == "}":
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			names = append(names, tok)
		}
	}
	return names
}
