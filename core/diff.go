package core

import (
	"regexp"
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
