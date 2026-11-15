package core

import (
	"regexp"
	"strconv"
	"strings"
)

// DiffOp es una línea del diff: "=" sin cambios, "-" borrada, "+" añadida.
type DiffOp struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

var reTrailingWS = regexp.MustCompile(`(?m)[ \t]+$`)

// Normalize unifica saltos de línea, quita espacios finales y deja un único
// salto de línea al final. Se usa antes de comparar configuraciones.
func Normalize(t string) string {
	t = strings.ReplaceAll(strings.ReplaceAll(t, "\r\n", "\n"), "\r", "\n")
	t = reTrailingWS.ReplaceAllString(t, "")
	return strings.TrimRight(t, "\n") + "\n"
}

// DiffLines compara dos textos línea a línea (LCS) tras recortar el prefijo y
// el sufijo comunes. Para entradas enormes cae a "todo borrado, todo añadido".
func DiffLines(a, b string) []DiffOp {
	A, B := strings.Split(a, "\n"), strings.Split(b, "\n")
	s := 0
	for s < len(A) && s < len(B) && A[s] == B[s] {
		s++
	}
	ea, eb := len(A), len(B)
	for ea > s && eb > s && A[ea-1] == B[eb-1] {
		ea--
		eb--
	}
	ops := make([]DiffOp, 0, len(A)+len(B))
	for i := 0; i < s; i++ {
		ops = append(ops, DiffOp{"=", A[i]})
	}
	a2, b2 := A[s:ea], B[s:eb]
	n, m := len(a2), len(b2)
	if n*m > 4_000_000 {
		for _, v := range a2 {
			ops = append(ops, DiffOp{"-", v})
		}
		for _, v := range b2 {
			ops = append(ops, DiffOp{"+", v})
		}
	} else {
		w := m + 1
		dp := make([]uint32, (n+1)*w)
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if a2[i] == b2[j] {
					dp[i*w+j] = dp[(i+1)*w+j+1] + 1
				} else if dp[(i+1)*w+j] >= dp[i*w+j+1] {
					dp[i*w+j] = dp[(i+1)*w+j]
				} else {
					dp[i*w+j] = dp[i*w+j+1]
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case a2[i] == b2[j]:
				ops = append(ops, DiffOp{"=", a2[i]})
				i++
				j++
			case dp[(i+1)*w+j] >= dp[i*w+j+1]:
				ops = append(ops, DiffOp{"-", a2[i]})
				i++
			default:
				ops = append(ops, DiffOp{"+", b2[j]})
				j++
			}
		}
		for ; i < n; i++ {
			ops = append(ops, DiffOp{"-", a2[i]})
		}
		for ; j < m; j++ {
			ops = append(ops, DiffOp{"+", b2[j]})
		}
	}
	for i := ea; i < len(A); i++ {
		ops = append(ops, DiffOp{"=", A[i]})
	}
	return ops
}

// UnifiedDiff devuelve un diff en formato unificado (con ctx líneas de
// contexto) entre dos textos, o "" si son iguales tras Normalize. name se usa
// en las cabeceras --- y +++.
func UnifiedDiff(name, before, after string, ctx int) string {
	ops := DiffLines(Normalize(before), Normalize(after))
	// Se descarta la línea vacía final que deja Split tras el último salto.
	if n := len(ops); n > 0 && ops[n-1].Op == "=" && ops[n-1].Text == "" {
		ops = ops[:n-1]
	}
	// Un texto vacío no tiene líneas (Normalize le deja una en blanco).
	if strings.TrimSpace(before) == "" {
		for i, o := range ops {
			if o.Op == "-" && o.Text == "" {
				ops = append(ops[:i], ops[i+1:]...)
				break
			}
		}
	}
	changed := false
	for _, o := range ops {
		if o.Op != "=" {
			changed = true
		}
	}
	if !changed {
		return ""
	}
	// Posición (1-based) de cada op en el texto original y en el nuevo.
	type pos struct{ a, b int }
	at := make([]pos, len(ops)+1)
	a, b := 1, 1
	for i, o := range ops {
		at[i] = pos{a, b}
		if o.Op != "+" {
			a++
		}
		if o.Op != "-" {
			b++
		}
	}
	at[len(ops)] = pos{a, b}

	var sb strings.Builder
	sb.WriteString("--- " + name + "\n+++ " + name + "\n")
	i := 0
	for i < len(ops) {
		for i < len(ops) && ops[i].Op == "=" {
			i++
		}
		if i >= len(ops) {
			break
		}
		start := max(0, i-ctx)
		// El hunk se extiende mientras haya cambios a menos de 2*ctx líneas.
		end, lastChange := i, i
		for j := i; j < len(ops); j++ {
			if ops[j].Op != "=" {
				lastChange = j
				end = j
			} else if j-lastChange > 2*ctx {
				break
			}
		}
		end = min(len(ops), end+ctx+1)
		var la, lb int
		for _, o := range ops[start:end] {
			if o.Op != "+" {
				la++
			}
			if o.Op != "-" {
				lb++
			}
		}
		sb.WriteString("@@ -" + hunkRange(at[start].a, la) + " +" + hunkRange(at[start].b, lb) + " @@\n")
		for _, o := range ops[start:end] {
			p := " "
			if o.Op != "=" {
				p = o.Op
			}
			sb.WriteString(p + o.Text + "\n")
		}
		i = end
	}
	return sb.String()
}

func hunkRange(start, n int) string {
	if n == 0 {
		start--
	}
	if n == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(n)
}
