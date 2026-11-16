package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed knowledge/knowledge.json
var knowledgeJSON []byte

// The visible texts live apart, one per language and with the same shape:
// knowledge.json only has structure (colors, valid sections...).
//
//go:embed knowledge/text.es.json
var textESJSON []byte

//go:embed knowledge/text.en.json
var textENJSON []byte

// SectionInfo describes a section type.
type SectionInfo struct {
	Color       string `json:"color"`
	Named       string `json:"named"` // required | optional | none
	Title       string `json:"title"`
	Description string `json:"description"`
	// Since: HAProxy version in which the section type appeared, if known.
	Since string `json:"since,omitempty"`
}

// DirectiveInfo describes a directive of the knowledge base.
type DirectiveInfo struct {
	Sections    []string `json:"sections"`
	Category    string   `json:"category"`
	Description string   `json:"description,omitempty"`
	// Since is the HAProxy version in which it appeared ("2.4"). Empty if it
	// is not known for sure.
	Since string `json:"since,omitempty"`
}

// AllowedIn reports whether the directive is valid in the given section type.
func (d DirectiveInfo) AllowedIn(section string) bool {
	for _, s := range d.Sections {
		if s == section {
			return true
		}
	}
	return false
}

// SectionText is the visible text of a section type.
type SectionText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// KnowledgeText is all the knowledge base texts in one language.
// The core/knowledge/text.<language>.json files have exactly this shape
// and the same keys.
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

// Knowledge is the complete knowledge base. It lives in
// core/knowledge/knowledge.json (structure) and text.<language>.json (texts)
// so explanations can be fixed and extended without touching code.
//
// The text fields (Categories, Options, Balance... and Title/Description of
// Sections and Directives) hold English, so earlier code
// keeps working. For another language, use Text(lang).
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
	// OptionSince gives the HAProxy version in which each "option" appeared.
	OptionSince map[string]string `json:"optionSince"`

	booleanFetches map[string]bool
	builtinACLs    map[string]bool
	texts          map[Lang]*KnowledgeText
}

// KB is the knowledge base loaded at startup.
var KB = mustLoadKnowledge()

// Sections is a shortcut to KB.Sections.
var Sections = KB.Sections

// proxySections are the headers recognized even when indented.
var proxySections = map[string]bool{"global": true, "defaults": true, "frontend": true, "backend": true, "listen": true}

func mustLoadKnowledge() *Knowledge {
	var k Knowledge
	if err := json.Unmarshal(knowledgeJSON, &k); err != nil {
		panic(fmt.Sprintf("haro: invalid knowledge base: %v", err))
	}
	k.booleanFetches = toSet(k.BooleanFetches)
	k.builtinACLs = toSet(k.BuiltinACLs)
	k.texts = map[Lang]*KnowledgeText{}
	for lang, raw := range map[Lang][]byte{LangES: textESJSON, LangEN: textENJSON} {
		var t KnowledgeText
		if err := json.Unmarshal(raw, &t); err != nil {
			panic(fmt.Sprintf("haro: invalid %s texts: %v", lang, err))
		}
		k.texts[lang] = &t
	}
	// Compatibility: the earlier accessors (KB.Options...) return English.
	es := k.texts[LangDefault]
	k.Categories, k.Options, k.Timeouts, k.Balance, k.Fetches = es.Categories, es.Options, es.Timeouts, es.Balance, es.Fetches
	k.Matchers, k.Operators, k.Store, k.RedirectCodes = es.Matchers, es.Operators, es.Store, es.RedirectCodes
	k.Sections = k.sectionsFor(LangDefault)
	k.Directives = k.directivesFor(LangDefault)
	return &k
}

// Text returns the texts of the requested language (English if it does not exist).
func (k *Knowledge) Text(lang Lang) *KnowledgeText {
	if t, ok := k.texts[lang]; ok {
		return t
	}
	return k.texts[LangDefault]
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

// PublicKnowledge is what the interfaces need from the knowledge
// base: section types, categories, directives per section (for
// autocompletion), detector rules and section templates.
type PublicKnowledge struct {
	Categories map[string]string          `json:"categories"`
	Sections   map[string]SectionInfo     `json:"sections"`
	Directives map[string]DirectiveInfo   `json:"directives"`
	Rules      map[string]string          `json:"rules"`
	Templates  map[string]SectionTemplate `json:"templates"`
}

// Public returns the part of the knowledge base published through the
// API, in English. For another language, use PublicFor.
func (k *Knowledge) Public() PublicKnowledge { return k.PublicFor(LangDefault) }

// PublicFor returns the public knowledge base with the texts of the requested language.
func (k *Knowledge) PublicFor(lang Lang) PublicKnowledge {
	return PublicKnowledge{Categories: k.Text(lang).Categories, Sections: k.sectionsFor(lang), Directives: k.directivesFor(lang), Rules: RulesFor(lang), Templates: Templates}
}
