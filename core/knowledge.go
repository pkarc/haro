package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed knowledge/knowledge.json
var knowledgeJSON []byte

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

// Knowledge es la base de conocimiento completa. Vive en
// core/knowledge/knowledge.json para poder corregir y ampliar explicaciones
// sin tocar código.
type Knowledge struct {
	Categories     map[string]string        `json:"categories"`
	Sections       map[string]SectionInfo   `json:"sections"`
	Directives     map[string]DirectiveInfo `json:"directives"`
	Options        map[string]string        `json:"options"`
	Timeouts       map[string]string        `json:"timeouts"`
	Balance        map[string]string        `json:"balance"`
	Fetches        map[string]string        `json:"fetches"`
	Matchers       map[string]string        `json:"matchers"`
	Operators      map[string]string        `json:"operators"`
	Store          map[string]string        `json:"store"`
	RedirectCodes  map[string]string        `json:"redirectCodes"`
	BooleanFetches []string                 `json:"booleanFetches"`
	BuiltinACLs    []string                 `json:"builtinAcls"`

	booleanFetches map[string]bool
	builtinACLs    map[string]bool
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
	return &k
}

func toSet(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, v := range list {
		m[v] = true
	}
	return m
}
