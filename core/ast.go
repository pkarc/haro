// Package core contiene todo lo que haro sabe de HAProxy: parser sin pérdidas,
// serializador, base de conocimiento, explicaciones, detector de problemas,
// modelo semántico (herencia de defaults, topología) y diff.
//
// No depende de ninguna interfaz: la usan por igual el CLI, `haro serve`
// y, en el futuro, el módulo WASM de la web pública.
package core

// LineKind distingue los tipos de línea dentro de una sección.
type LineKind string

const (
	KindBlank     LineKind = "blank"
	KindComment   LineKind = "comment"
	KindDirective LineKind = "directive"
)

// Line es una línea dentro de una sección (o antes de la primera sección).
// Text guarda el contenido exacto sin la indentación, para poder volver a
// escribir el archivo sin cambios.
type Line struct {
	Kind    LineKind `json:"kind"`
	Line    int      `json:"line"`
	Indent  string   `json:"indent,omitempty"`
	Text    string   `json:"text,omitempty"`
	Keyword string   `json:"keyword,omitempty"`
	Args    []string `json:"args,omitempty"`
	Comment string   `json:"comment,omitempty"`
	// File es el índice del archivo de origen dentro de un Config.
	File int `json:"file"`
	// IndentSet indica si Indent viene del archivo original. Las líneas nuevas
	// creadas por operaciones de edición usan la indentación por defecto.
	IndentSet bool `json:"-"`
}

// Section es una sección de nivel superior: global, defaults, frontend…
type Section struct {
	File    int      `json:"file"` // índice del archivo de origen dentro de un Config
	Type    string   `json:"type"`
	Name    string   `json:"name,omitempty"`
	Line    int      `json:"line"`
	Indent  string   `json:"indent,omitempty"`
	Text    string   `json:"text"`
	Tokens  []string `json:"tokens"`
	Comment string   `json:"comment,omitempty"`
	Lines   []*Line  `json:"lines"`
}

// Directives devuelve solo las líneas que son directivas.
func (s *Section) Directives() []*Line {
	out := make([]*Line, 0, len(s.Lines))
	for _, l := range s.Lines {
		if l.Kind == KindDirective {
			out = append(out, l)
		}
	}
	return out
}

// Doc es el árbol completo de un haproxy.cfg.
type Doc struct {
	Pre      []*Line    `json:"pre"`
	Sections []*Section `json:"sections"`
	// Files lista las rutas de origen cuando el Doc une varios archivos
	// (ver Config.Merge). Vacío para un único texto.
	Files []string `json:"files,omitempty"`
}
