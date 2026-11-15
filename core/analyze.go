package core

// LineExplanation asocia una directiva con su explicación.
type LineExplanation struct {
	File    int         `json:"file"`
	Line    int         `json:"line"`
	Section int         `json:"section"`
	Keyword string      `json:"keyword"`
	Args    string      `json:"args"`
	Explain Explanation `json:"explanation"`
}

// Analysis es todo lo que la interfaz necesita en una sola llamada.
// Con varios archivos, Doc es la vista unificada y Doc.Files lista las rutas.
type Analysis struct {
	Doc          *Doc              `json:"doc"`
	Issues       []Issue           `json:"issues"`
	Explanations []LineExplanation `json:"explanations"`
	Effective    []Effective       `json:"effective"`
	Topology     Topology          `json:"topology"`
}

// Analyze parsea un único texto y calcula problemas, explicaciones, herencia
// y topología. Para varios archivos, usa AnalyzeConfig.
func Analyze(text string) Analysis {
	return analyzeDoc(Parse(text))
}

func analyzeDoc(doc *Doc) Analysis {
	ex := []LineExplanation{}
	for i, s := range doc.Sections {
		for _, l := range s.Directives() {
			ex = append(ex, LineExplanation{File: s.File, Line: l.Line, Section: i, Keyword: l.Keyword, Args: DisplayArgs(l), Explain: Describe(l, s.Type)})
		}
	}
	return Analysis{Doc: doc, Issues: Lint(doc), Explanations: ex, Effective: ComputeEffective(doc), Topology: BuildTopology(doc)}
}
