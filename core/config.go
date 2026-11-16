package core

// HAProxy can load several files (`-f a.cfg -f conf.d/`). They are processed
// in load order as if they were one, with two rules:
//   - each file starts outside any section: a section cannot be
//     split across files;
//   - everything else does cross files: `defaults` inheritance, references to
//     backends, duplicate names...
//
// Config represents that set. Directory expansion (only non-hidden
// `.cfg` files, in lexicographic order) is done by whoever reads from disk,
// because the core does not access the file system: that way it works the same in the
// CLI, in `haro serve` and in WASM.

// Source is a configuration file already read.
type Source struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// File is a parsed file within a Config.
type File struct {
	Path string `json:"path"`
	Doc  *Doc   `json:"doc"`
}

// Config is a configuration made up of one or more files, in load order.
type Config struct {
	Files []*File `json:"files"`
}

// NewConfig parses each file and marks its lines and sections with their index.
func NewConfig(sources []Source) *Config {
	cfg := &Config{Files: make([]*File, len(sources))}
	for i, src := range sources {
		doc := Parse(src.Text)
		for _, l := range doc.Pre {
			l.File = i
		}
		for _, s := range doc.Sections {
			s.File = i
			for _, l := range s.Lines {
				l.File = i
			}
		}
		cfg.Files[i] = &File{Path: src.Path, Doc: doc}
	}
	return cfg
}

// Merge returns a unified view in load order. The sections and
// lines are the same pointers as in each file, so editing through
// the unified view modifies the corresponding file.
func (c *Config) Merge() *Doc {
	m := &Doc{Pre: []*Line{}, Sections: []*Section{}, Files: make([]string, len(c.Files))}
	for i, f := range c.Files {
		m.Files[i] = f.Path
		m.Pre = append(m.Pre, f.Doc.Pre...)
		m.Sections = append(m.Sections, f.Doc.Sections...)
	}
	return m
}

// Serialize returns the text of file i.
func (c *Config) Serialize(i int) string {
	return Serialize(c.Files[i].Doc)
}

// AnalyzeConfig analyzes a multi-file configuration. The lines of
// problems and explanations are relative to their file (File field).
// The texts come out in English; for another language, use AnalyzeConfigLang.
func AnalyzeConfig(sources []Source) Analysis { return AnalyzeConfigLang(sources, LangDefault) }

// AnalyzeConfigLang is AnalyzeConfig with the texts in the requested language.
func AnalyzeConfigLang(sources []Source, lang Lang) Analysis {
	return analyzeDoc(NewConfig(sources).Merge(), lang)
}

// AnalyzeConfigWith is AnalyzeConfigLang with detector options (language and
// target HAProxy version).
func AnalyzeConfigWith(sources []Source, opts LintOptions) Analysis {
	return analyzeDocWith(NewConfig(sources).Merge(), opts)
}
