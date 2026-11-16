package core

// LineExplanation pairs a directive with its explanation.
type LineExplanation struct {
	File    int         `json:"file"`
	Line    int         `json:"line"`
	Section int         `json:"section"`
	Keyword string      `json:"keyword"`
	Args    string      `json:"args"`
	Explain Explanation `json:"explanation"`
}

// Analysis is everything the interface needs in a single call.
// With several files, Doc is the unified view and Doc.Files lists the paths.
type Analysis struct {
	Doc          *Doc              `json:"doc"`
	Issues       []Issue           `json:"issues"`
	Explanations []LineExplanation `json:"explanations"`
	Effective    []Effective       `json:"effective"`
	Topology     Topology          `json:"topology"`
}

// Analyze parses a single text and computes problems, explanations, inheritance
// and topology, with English texts. For several files use
// AnalyzeConfig; for another language, AnalyzeLang.
func Analyze(text string) Analysis { return AnalyzeLang(text, LangDefault) }

// AnalyzeLang is Analyze with the texts in the requested language.
func AnalyzeLang(text string, lang Lang) Analysis {
	return analyzeDoc(Parse(text), lang)
}

func analyzeDoc(doc *Doc, lang Lang) Analysis { return analyzeDocWith(doc, LintOptions{Lang: lang}) }

func analyzeDocWith(doc *Doc, opts LintOptions) Analysis {
	lang := opts.Lang
	ex := []LineExplanation{}
	for i, s := range doc.Sections {
		for _, l := range s.Directives() {
			ex = append(ex, LineExplanation{File: s.File, Line: l.Line, Section: i, Keyword: l.Keyword, Args: DisplayArgs(l), Explain: DescribeLang(l, s.Type, lang)})
		}
	}
	return Analysis{Doc: doc, Issues: LintWith(doc, opts), Explanations: ex, Effective: ComputeEffective(doc), Topology: BuildTopologyLang(doc, lang)}
}
