package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCorpus recorre configuraciones reales de terceros, si se han descargado
// con scripts/fetch-corpus.sh. Comprueba que:
//   - el parser no pierde nada al reescribir (Serialize(Parse(x)) == x);
//   - Analyze no falla con ninguna;
//
// y muestra qué directivas no conoce todavía la base de conocimiento, para
// saber qué explicaciones añadir.
func TestCorpus(t *testing.T) {
	root := filepath.Join("testdata", "corpus")
	if _, err := os.Stat(root); err != nil {
		t.Skip("sin corpus; ejecuta scripts/fetch-corpus.sh")
	}
	var files []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".cfg") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		t.Skip("el corpus no contiene archivos .cfg")
	}
	unknown := map[string]int{}
	directives := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Error(err)
			continue
		}
		src := strings.ReplaceAll(string(b), "\r\n", "\n")
		doc := Parse(src)
		if out := Serialize(doc); out != trimTrailing(src) {
			t.Errorf("%s: la reescritura no es idéntica al original", f)
		}
		a := Analyze(src)
		for _, e := range a.Explanations {
			directives++
			if !e.Explain.Known {
				unknown[e.Keyword]++
			}
		}
	}
	type kv struct {
		k string
		n int
	}
	var list []kv
	total := 0
	for k, n := range unknown {
		list = append(list, kv{k, n})
		total += n
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	t.Logf("%d archivos, %d directivas, %d sin explicación conocida (%.1f%% de cobertura)", len(files), directives, total, 100*float64(directives-total)/float64(max(directives, 1)))
	for i, e := range list {
		if i == 25 {
			break
		}
		t.Logf("  %-32s %d", e.k, e.n)
	}
}

// trimTrailing quita los espacios al final de cada línea, que el parser no
// conserva a propósito.
func trimTrailing(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}
