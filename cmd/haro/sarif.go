package main

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pkarc/haro/core"
)

// SARIF 2.1.0 output of haro lint, for GitHub code scanning and other
// tools. The core.Rules entries are the rule descriptions.

const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool       `json:"tool"`
	Results     []sarifResult   `json:"results"`
	Artifacts   []sarifArtifact `json:"artifacts,omitempty"`
	ColumnKind  string          `json:"columnKind,omitempty"`
	OriginalURI map[string]any  `json:"originalUriBaseIds,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string         `json:"id"`
	ShortDescription sarifText      `json:"shortDescription"`
	DefaultConfig    map[string]any `json:"defaultConfiguration,omitempty"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	RuleIndex int             `json:"ruleIndex"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	Physical sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	Artifact sarifArtifactLocation `json:"artifactLocation"`
	Region   sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type sarifArtifact struct {
	Location sarifArtifactLocation `json:"location"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

func sarifLevel(s core.Severity) string {
	switch s {
	case core.SevError:
		return "error"
	case core.SevWarn:
		return "warning"
	}
	return "note"
}

// sarifURI converts a file path into a URI. Paths inside the working
// directory are relative to %SRCROOT% (what GitHub expects); those
// outside are an absolute file URI.
func sarifURI(path, cwd string) sarifArtifactLocation {
	abs, err := filepath.Abs(path)
	if err == nil {
		if rel, err := filepath.Rel(cwd, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return sarifArtifactLocation{URI: (&url.URL{Path: filepath.ToSlash(rel)}).String(), URIBaseID: "%SRCROOT%"}
		}
		return sarifArtifactLocation{URI: (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()}
	}
	return sarifArtifactLocation{URI: (&url.URL{Path: filepath.ToSlash(path)}).String()}
}

// writeSARIF writes the report of the problems found in srcs.
func writeSARIF(w io.Writer, srcs []core.Source, issues []core.Issue, lang core.Lang) error {
	coreRules := core.RulesFor(lang)
	cwd, _ := os.Getwd()
	ids := make([]string, 0, len(coreRules))
	for id := range coreRules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	index := map[string]int{}
	rules := make([]sarifRule, len(ids))
	for i, id := range ids {
		index[id] = i
		rules[i] = sarifRule{ID: id, ShortDescription: sarifText{coreRules[id]}}
	}
	run := sarifRun{
		Tool: sarifTool{Driver: sarifDriver{Name: "haro", Version: version, InformationURI: "https://github.com/pkarc/haro", Rules: rules}},
		// results is never null: GitHub rejects it.
		Results:    []sarifResult{},
		ColumnKind: "utf16CodeUnits",
	}
	for _, s := range srcs {
		run.Artifacts = append(run.Artifacts, sarifArtifact{Location: sarifURI(s.Path, cwd)})
	}
	for _, i := range issues {
		run.Results = append(run.Results, sarifResult{
			RuleID: i.Rule, RuleIndex: index[i.Rule], Level: sarifLevel(i.Severity), Message: sarifText{i.Message},
			Locations: []sarifLocation{{Physical: sarifPhysical{Artifact: sarifURI(srcs[i.File].Path, cwd), Region: sarifRegion{StartLine: max(1, i.Line)}}}},
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(sarifLog{Schema: sarifSchema, Version: "2.1.0", Runs: []sarifRun{run}})
}

// writeMarkdown writes a markdown summary of the problems, meant for pull
// request comments.
func writeMarkdown(w io.Writer, srcs []core.Source, issues []core.Issue, lang core.Lang) {
	counts := map[core.Severity]int{}
	for _, i := range issues {
		counts[i.Severity]++
	}
	io.WriteString(w, "### haro lint\n\n")
	if len(issues) == 0 {
		io.WriteString(w, tx(lang, "✅ Sin problemas detectados. Valida siempre con `haproxy -c`.\n", "✅ No problems found. Always validate with `haproxy -c`.\n"))
		return
	}
	io.WriteString(w, summary(lang, counts)+"\n\n")
	io.WriteString(w, tx(lang, "| | Ubicación | Problema | Regla |\n|---|---|---|---|\n", "| | Location | Problem | Rule |\n|---|---|---|---|\n"))
	const maxRows = 50
	icon := map[core.Severity]string{core.SevError: "❌", core.SevWarn: "⚠️", core.SevInfo: "💡"}
	esc := strings.NewReplacer("|", "\\|", "\n", " ")
	for n, i := range issues {
		if n == maxRows {
			io.WriteString(w, tx(lang, "\n_… y "+plural(len(issues)-maxRows, "problema más", "problemas más")+". Ejecuta `haro lint` para verlos todos._\n", "\n_… and "+plural(len(issues)-maxRows, "more problem", "more problems")+". Run `haro lint` to see them all._\n"))
			break
		}
		io.WriteString(w, "| "+icon[i.Severity]+" | `"+esc.Replace(srcs[i.File].Path)+":"+strconv.Itoa(i.Line)+"` | "+esc.Replace(i.Message)+" | `"+i.Rule+"` |\n")
	}
}
