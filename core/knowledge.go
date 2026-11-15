package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed knowledge/knowledge.json
var knowledgeJSON []byte

// Los textos visibles viven aparte, uno por idioma y con la misma forma:
// knowledge.json solo tiene estructura (colores, secciones válidas…).
//
//go:embed knowledge/text.es.json
var textESJSON []byte

//go:embed knowledge/text.en.json
var textENJSON []byte

// SectionInfo describe un tipo de sección.
type SectionInfo struct {
	Color       string `json:"color"`
	Named       string `json:"named"` // required | optional | none
	Title       string `json:"title"`
	Description string `json:"description"`
}

// DirectiveInfo describe una directiva de la base de conocimiento.
type DirectiveInfo struct {
	Sections    []string `json:"sections"`
	Category    string   `json:"category"`
	Description string   `json:"description,omitempty"`
}

// AllowedIn indica si la directiva es válida en el tipo de sección dado.
func (d DirectiveInfo) AllowedIn(section string) bool {
	for _, s := range d.Sections {
		if s == section {
			return true
		}
	}
	return false
}

// SectionText es el texto visible de un tipo de sección.
type SectionText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// KnowledgeText son todos los textos de la base de conocimiento en un idioma.
// Los archivos core/knowledge/text.<idioma>.json tienen exactamente esta forma
// y las mismas claves.
type KnowledgeText struct {
	Categories    map[string]string      `json:"categories"`
	Sections      map[string]SectionText `json:"sections"`
	Directives    map[string]string      `json:"directives"`
	Options       map[string]string      `json:"options"`
	Timeouts      map[string]string      `json:"timeouts"`
	Balance       map[string]string      `json:"balance"`
	Fetches       map[string]string      `json:"fetches"`
	Matchers      map[string]string      `json:"matchers"`
	Operators     map[string]string      `json:"operators"`
	Store         map[string]string      `json:"store"`
	RedirectCodes map[string]string      `json:"redirectCodes"`
}

// Knowledge es la base de conocimiento completa. Vive en
// core/knowledge/knowledge.json (estructura) y text.<idioma>.json (textos)
// para poder corregir y ampliar explicaciones sin tocar código.
//
// Los campos de texto (Categories, Options, Balance… y Title/Description de
// Sections y Directives) contienen el español, para que el código anterior
// siga funcionando. Para otro idioma, usa Text(lang).
type Knowledge struct {
	Categories     map[string]string        `json:"-"`
	Sections       map[string]SectionInfo   `json:"sections"`
	Directives     map[string]DirectiveInfo `json:"directives"`
	Options        map[string]string        `json:"-"`
	Timeouts       map[string]string        `json:"-"`
	Balance        map[string]string        `json:"-"`
	Fetches        map[string]string        `json:"-"`
	Matchers       map[string]string        `json:"-"`
	Operators      map[string]string        `json:"-"`
	Store          map[string]string        `json:"-"`
	RedirectCodes  map[string]string        `json:"-"`
	BooleanFetches []string                 `json:"booleanFetches"`
	BuiltinACLs    []string                 `json:"builtinAcls"`

	booleanFetches map[string]bool
	builtinACLs    map[string]bool
	texts          map[Lang]*KnowledgeText
}

// KB es la base de conocimiento cargada al iniciar.
var KB = mustLoadKnowledge()

// Sections es un atajo a KB.Sections.
var Sections = KB.Sections

// proxySections son las cabeceras que se reconocen aunque estén indentadas.
var proxySections = map[string]bool{"global": true, "defaults": true, "frontend": true, "backend": true, "listen": true}

func mustLoadKnowledge() *Knowledge {
	var k Knowledge
	if err := json.Unmarshal(knowledgeJSON, &k); err != nil {
		panic(fmt.Sprintf("haro: base de conocimiento inválida: %v", err))
	}
	k.booleanFetches = toSet(k.BooleanFetches)
	k.builtinACLs = toSet(k.BuiltinACLs)
	k.texts = map[Lang]*KnowledgeText{}
	for lang, raw := range map[Lang][]byte{LangES: textESJSON, LangEN: textENJSON} {
		var t KnowledgeText
		if err := json.Unmarshal(raw, &t); err != nil {
			panic(fmt.Sprintf("haro: textos %s inválidos: %v", lang, err))
		}
		k.texts[lang] = &t
	}
	// Compatibilidad: los accesos anteriores (KB.Options…) devuelven español.
	es := k.texts[LangES]
	k.Categories, k.Options, k.Timeouts, k.Balance, k.Fetches = es.Categories, es.Options, es.Timeouts, es.Balance, es.Fetches
	k.Matchers, k.Operators, k.Store, k.RedirectCodes = es.Matchers, es.Operators, es.Store, es.RedirectCodes
	k.Sections = k.sectionsFor(LangES)
	k.Directives = k.directivesFor(LangES)
	return &k
}

// Text devuelve los textos del idioma pedido (español si no existe).
func (k *Knowledge) Text(lang Lang) *KnowledgeText {
	if t, ok := k.texts[lang]; ok {
		return t
	}
	return k.texts[LangES]
}

func (k *Knowledge) sectionsFor(lang Lang) map[string]SectionInfo {
	t := k.Text(lang)
	out := make(map[string]SectionInfo, len(k.Sections))
	for name, info := range k.Sections {
		info.Title, info.Description = t.Sections[name].Title, t.Sections[name].Description
		out[name] = info
	}
	return out
}

func (k *Knowledge) directivesFor(lang Lang) map[string]DirectiveInfo {
	t := k.Text(lang)
	out := make(map[string]DirectiveInfo, len(k.Directives))
	for name, info := range k.Directives {
		info.Description = t.Directives[name]
		out[name] = info
	}
	return out
}

func toSet(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, v := range list {
		m[v] = true
	}
	return m
}

// PublicKnowledge es lo que las interfaces necesitan de la base de
// conocimiento: tipos de sección, categorías, directivas por sección (para el
// autocompletado), reglas del detector y plantillas de sección.
type PublicKnowledge struct {
	Categories map[string]string          `json:"categories"`
	Sections   map[string]SectionInfo     `json:"sections"`
	Directives map[string]DirectiveInfo   `json:"directives"`
	Rules      map[string]string          `json:"rules"`
	Templates  map[string]SectionTemplate `json:"templates"`
}

// Public devuelve la parte de la base de conocimiento que se publica por la
// API, en español. Para otro idioma, usa PublicFor.
func (k *Knowledge) Public() PublicKnowledge { return k.PublicFor(LangES) }

// PublicFor devuelve la base de conocimiento pública con los textos del idioma pedido.
func (k *Knowledge) PublicFor(lang Lang) PublicKnowledge {
	return PublicKnowledge{Categories: k.Text(lang).Categories, Sections: k.sectionsFor(lang), Directives: k.directivesFor(lang), Rules: RulesFor(lang), Templates: Templates}
}
