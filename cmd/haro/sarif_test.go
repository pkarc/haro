package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkarc/haro/core"
)

func TestSARIF(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)
	os.WriteFile("a.cfg", []byte("frontend f\n    bind *:80\n    default_backend nope\n"), 0o644)
	srcs := []core.Source{{Path: "a.cfg", Text: "frontend f\n    bind *:80\n    default_backend nope\n"}, {Path: filepath.Join(os.TempDir(), "fuera.cfg"), Text: "global\n"}}
	issues := core.Lint(core.NewConfig(srcs).Merge())
	var buf bytes.Buffer
	if err := writeSARIF(&buf, srcs, issues); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string
					Rules []struct{ ID string }
				}
			}
			Results []struct {
				RuleID    string
				RuleIndex int
				Level     string
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI, URIBaseId string }
						Region           struct{ StartLine int }
					}
				}
			}
		}
	}
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 || log.Runs[0].Tool.Driver.Name != "haro" {
		t.Fatalf("estructura SARIF: %s", buf.String())
	}
	run := log.Runs[0]
	if len(run.Tool.Driver.Rules) != len(core.Rules) {
		t.Errorf("deben listarse todas las reglas: %d", len(run.Tool.Driver.Rules))
	}
	found := false
	for _, r := range run.Results {
		if run.Tool.Driver.Rules[r.RuleIndex].ID != r.RuleID {
			t.Errorf("ruleIndex no corresponde a ruleId %s", r.RuleID)
		}
		loc := r.Locations[0].PhysicalLocation
		if r.RuleID == "backend-missing" {
			found = true
			if r.Level != "error" || loc.ArtifactLocation.URI != "a.cfg" || loc.ArtifactLocation.URIBaseId != "%SRCROOT%" || loc.Region.StartLine != 3 {
				t.Errorf("resultado: %+v", r)
			}
		}
		if loc.Region.StartLine < 1 {
			t.Errorf("startLine debe ser >= 1: %+v", r)
		}
	}
	if !found {
		t.Error("falta backend-missing")
	}
	// Sin problemas, results es [] y no null.
	buf.Reset()
	writeSARIF(&buf, srcs[:1], nil)
	if !bytes.Contains(buf.Bytes(), []byte(`"results": []`)) {
		t.Errorf("results vacío debe ser []: %s", buf.String())
	}
}

func TestMarkdownSummary(t *testing.T) {
	srcs := []core.Source{{Path: "a.cfg", Text: "frontend f\n    bind *:80\n    default_backend nope\n"}}
	issues := core.Lint(core.NewConfig(srcs).Merge())
	var buf bytes.Buffer
	writeMarkdown(&buf, srcs, issues)
	out := buf.String()
	for _, want := range []string{"### haro lint", "1 error", "| ❌ | `a.cfg:3` |", "`backend-missing`"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("falta %q:\n%s", want, out)
		}
	}
	buf.Reset()
	writeMarkdown(&buf, srcs, nil)
	if !bytes.Contains(buf.Bytes(), []byte("Sin problemas")) {
		t.Errorf("sin problemas: %s", buf.String())
	}
}
