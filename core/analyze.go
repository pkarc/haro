package core

// LineExplanation asocia una directiva con su explicación.
type LineExplanation struct {
	Line    int         `json:"line"`
	Section int         `json:"section"`
	Keyword string      `json:"keyword"`
	Args    string      `json:"args"`
	Explain Explanation `json:"explanation"`
}

// Analysis es todo lo que la interfaz necesita en una sola llamada.
type Analysis struct {
	Doc          *Doc              `json:"doc"`
	Issues       []Issue           `json:"issues"`
	Explanations []LineExplanation `json:"explanations"`
	Effective    []Effective       `json:"effective"`
	Topology     Topology          `json:"topology"`
}

// Analyze parsea el texto y calcula problemas, explicaciones, herencia y
// topología. Es la función principal que exponen el API local y el WASM.
func Analyze(text string) Analysis {
	doc := Parse(text)
	var ex []LineExplanation
	for i, s := range doc.Sections {
		for _, l := range s.Directives() {
			ex = append(ex, LineExplanation{Line: l.Line, Section: i, Keyword: l.Keyword, Args: DisplayArgs(l), Explain: Describe(l, s.Type)})
		}
	}
	if ex == nil {
		ex = []LineExplanation{}
	}
	return Analysis{Doc: doc, Issues: Lint(doc), Explanations: ex, Effective: ComputeEffective(doc), Topology: BuildTopology(doc)}
}
