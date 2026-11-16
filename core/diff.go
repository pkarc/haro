package core

import (
	"regexp"
	"strconv"
	"strings"
)

// DiffOp is a diff line: "=" unchanged, "-" deleted, "+" added.
type DiffOp struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

var reTrailingWS = regexp.MustCompile(`(?m)[ \t]+$`)

// Normalize unifies line breaks, strips trailing spaces and leaves a single
// line break at the end. Used before comparing configurations.
func Normalize(t string) string {
	t = strings.ReplaceAll(strings.ReplaceAll(t, "\r\n", "\n"), "\r", "\n")
	t = reTrailingWS.ReplaceAllString(t, "")
	return strings.TrimRight(t, "\n") + "\n"
}

// DiffLines compares two texts line by line (LCS) after trimming the common
// prefix and suffix. For huge inputs it falls back to "all deleted, all added".
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

// UnifiedDiff returns a unified-format diff (with ctx lines of
// context) between two texts, or "" if they are equal after Normalize. name is used
// in the --- and +++ headers.
func UnifiedDiff(name, before, after string, ctx int) string {
	ops := DiffLines(Normalize(before), Normalize(after))
	// The trailing empty line that Split leaves after the last newline is discarded.
	if n := len(ops); n > 0 && ops[n-1].Op == "=" && ops[n-1].Text == "" {
		ops = ops[:n-1]
	}
	// An empty text has no lines (Normalize leaves it one blank line).
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
	// Position (1-based) of each op in the original and the new text.
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
		// The hunk extends while there are changes less than 2*ctx lines apart.
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
