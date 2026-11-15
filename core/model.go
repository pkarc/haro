package core

import "strings"

// Effective es la configuración que realmente aplica a una sección tras
// heredar de su `defaults`.
type Effective struct {
	Mode     string          `json:"mode"`
	ModeFrom string          `json:"modeFrom"` // own | defaults | implicit
	Timeouts map[string]bool `json:"timeouts"`
	Defaults int             `json:"defaults"` // índice de la sección defaults heredada, -1 si ninguna
}

// ComputeEffective calcula la herencia para cada sección, en orden. Cada
// sección defaults reinicia lo heredado, como hace HAProxy.
func ComputeEffective(doc *Doc) []Effective {
	defMode, defTmo, defIdx := "", map[string]bool{}, -1
	out := make([]Effective, len(doc.Sections))
	for i, s := range doc.Sections {
		own, ownTmo := "", map[string]bool{}
		for _, l := range s.Directives() {
			switch l.Keyword {
			case "mode":
				own = arg(l.Args, 0)
			case "timeout":
				ownTmo[arg(l.Args, 0)] = true
			}
		}
		if s.Type == "defaults" {
			defMode, defTmo, defIdx = own, ownTmo, i
			out[i] = Effective{Mode: own, ModeFrom: "own", Timeouts: ownTmo, Defaults: -1}
			continue
		}
		e := Effective{Timeouts: map[string]bool{}, Defaults: defIdx}
		for k := range defTmo {
			e.Timeouts[k] = true
		}
		for k := range ownTmo {
			e.Timeouts[k] = true
		}
		switch {
		case own != "":
			e.Mode, e.ModeFrom = own, "own"
		case defMode != "":
			e.Mode, e.ModeFrom = defMode, "defaults"
		default:
			e.Mode, e.ModeFrom = "tcp", "implicit"
		}
		out[i] = e
	}
	return out
}

// Bind resume una línea bind para el mapa.
type Bind struct {
	Address string `json:"address"`
	TLS     bool   `json:"tls"`
}

// ServerNode es un servidor en el mapa.
type ServerNode struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Backup   bool   `json:"backup,omitempty"`
	Check    bool   `json:"check,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Template bool   `json:"template,omitempty"`
}

// EntryNode es un frontend o listen.
type EntryNode struct {
	Section int          `json:"section"`
	Type    string       `json:"type"`
	Name    string       `json:"name"`
	Binds   []Bind       `json:"binds"`
	Servers []ServerNode `json:"servers,omitempty"` // solo listen
}

// BackendNode es un backend.
type BackendNode struct {
	Section int          `json:"section"`
	Name    string       `json:"name"`
	Balance string       `json:"balance"`
	Servers []ServerNode `json:"servers"`
}

// Edge une una entrada con un backend.
type Edge struct {
	File      int    `json:"file"`    // archivo de la regla
	From      int    `json:"from"`    // índice de sección
	To        int    `json:"to"`      // índice de sección, -1 si no existe
	ToName    string `json:"toName"`  // nombre del backend
	Default   bool   `json:"default"` // default_backend
	Condition string `json:"condition,omitempty"`
	Line      int    `json:"line"`
}

// Topology es el grafo de tráfico: entradas → backends → servidores.
type Topology struct {
	Entries  []EntryNode   `json:"entries"`
	Backends []BackendNode `json:"backends"`
	Edges    []Edge        `json:"edges"`
	Missing  []string      `json:"missing"`
}

func serversOf(s *Section) []ServerNode {
	var out []ServerNode
	for _, l := range s.Directives() {
		set := toSet(l.Args)
		switch l.Keyword {
		case "server":
			out = append(out, ServerNode{Name: arg(l.Args, 0), Address: arg(l.Args, 1), Backup: set["backup"], Check: set["check"], Disabled: set["disabled"]})
		case "server-template":
			out = append(out, ServerNode{Name: arg(l.Args, 0) + "×" + arg(l.Args, 1), Address: arg(l.Args, 2), Check: set["check"], Template: true})
		}
	}
	return out
}

// BuildTopology construye el mapa de tráfico.
func BuildTopology(doc *Doc) Topology {
	t := Topology{Entries: []EntryNode{}, Backends: []BackendNode{}, Edges: []Edge{}, Missing: []string{}}
	byName := map[string]int{}
	for i, s := range doc.Sections {
		if (s.Type == "backend" || s.Type == "listen") && s.Name != "" {
			byName[s.Name] = i
		}
	}
	missing := map[string]bool{}
	for i, s := range doc.Sections {
		switch s.Type {
		case "frontend", "listen":
			e := EntryNode{Section: i, Type: s.Type, Name: s.Name, Binds: []Bind{}}
			for _, l := range s.Directives() {
				if l.Keyword == "bind" {
					e.Binds = append(e.Binds, Bind{Address: arg(l.Args, 0), TLS: toSet(l.Args)["ssl"]})
				}
			}
			if s.Type == "listen" {
				e.Servers = serversOf(s)
			}
			t.Entries = append(t.Entries, e)
			for _, l := range s.Directives() {
				var target, cond string
				def := false
				switch l.Keyword {
				case "default_backend":
					target, def = arg(l.Args, 0), true
				case "use_backend":
					main, cd := SplitCond(l.Args)
					target = arg(main, 0)
					if cd != nil {
						cond = strings.Join(cd.Tokens, " ")
						if cd.Negated {
							cond = "salvo " + cond
						}
					}
				default:
					continue
				}
				to, ok := byName[target]
				if !ok {
					to = -1
					if !missing[target] {
						missing[target] = true
						t.Missing = append(t.Missing, target)
					}
				}
				t.Edges = append(t.Edges, Edge{File: s.File, From: i, To: to, ToName: target, Default: def, Condition: cond, Line: l.Line})
			}
		case "backend":
			bal := "roundrobin"
			for _, l := range s.Directives() {
				if l.Keyword == "balance" {
					bal = arg(l.Args, 0)
				}
			}
			t.Backends = append(t.Backends, BackendNode{Section: i, Name: s.Name, Balance: bal, Servers: serversOf(s)})
		}
	}
	return t
}
