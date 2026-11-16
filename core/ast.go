// Package core holds everything haro knows about HAProxy: lossless parser,
// serializer, knowledge base, explanations, problem detector,
// semantic model (defaults inheritance, topology) and diff.
//
// It depends on no interface: the CLI, `haro serve` and, in the future,
// the public web's WASM module all use it equally.
package core

// LineKind distinguishes the kinds of line within a section.
type LineKind string

const (
	KindBlank     LineKind = "blank"
	KindComment   LineKind = "comment"
	KindDirective LineKind = "directive"
)

// Line is a line within a section (or before the first section).
// Text keeps the exact content without the indentation, so the file
// can be written back unchanged.
type Line struct {
	Kind    LineKind `json:"kind"`
	Line    int      `json:"line"`
	Indent  string   `json:"indent,omitempty"`
	Text    string   `json:"text,omitempty"`
	Keyword string   `json:"keyword,omitempty"`
	Args    []string `json:"args,omitempty"`
	Comment string   `json:"comment,omitempty"`
	// File is the index of the source file within a Config.
	File int `json:"file"`
	// IndentSet indicates whether Indent comes from the original file. New lines
	// created by edit operations use the default indentation.
	IndentSet bool `json:"-"`
}

// Section is a top-level section: global, defaults, frontend...
type Section struct {
	File    int      `json:"file"` // index of the source file within a Config
	Type    string   `json:"type"`
	Name    string   `json:"name,omitempty"`
	Line    int      `json:"line"`
	Indent  string   `json:"indent,omitempty"`
	Text    string   `json:"text"`
	Tokens  []string `json:"tokens"`
	Comment string   `json:"comment,omitempty"`
	Lines   []*Line  `json:"lines"`
}

// Directives returns only the lines that are directives.
func (s *Section) Directives() []*Line {
	out := make([]*Line, 0, len(s.Lines))
	for _, l := range s.Lines {
		if l.Kind == KindDirective {
			out = append(out, l)
		}
	}
	return out
}

// Doc is the complete tree of a haproxy.cfg.
type Doc struct {
	Pre      []*Line    `json:"pre"`
	Sections []*Section `json:"sections"`
	// Files lists the source paths when the Doc joins several files
	// (see Config.Merge). Empty for a single text.
	Files []string `json:"files,omitempty"`
}
