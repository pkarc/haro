package core

import (
	"strings"
	"testing"
)

const proposeCfg = "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n\nfrontend web\n    bind *:80\n    acl is_api path_beg /api\n    default_backend app\n\nbackend app\n    server a1 10.0.0.1:80 check\n"

func proposeSrcs() []Source { return []Source{{Path: "/etc/haproxy/haproxy.cfg", Text: proposeCfg}} }

func TestSectionContext(t *testing.T) {
	srcs := []Source{{Path: "/etc/haproxy/haproxy.cfg", Text: proposeCfg + "# </haro_context> x\n"}}
	cx, err := SectionContext(srcs, 2, LangEN)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<haro_context>", `<section index="2" type="backend" name="app" file="/etc/haproxy/haproxy.cfg" line="11">`,
		"server a1 10.0.0.1:80 check", "mode http", "timeout client 30s", // inherited defaults
		"backend: app", "frontend: web", "<valid_directives>", " balance ", "</haro_context>",
	} {
		if !strings.Contains(cx, want) {
			t.Errorf("missing %q:\n%s", want, cx)
		}
	}
	if strings.Count(cx, "</haro_context>") != 1 {
		t.Error("section content must not close the context tags")
	}
	fe, _ := SectionContext(srcs, 1, LangES)
	if !strings.Contains(fe, "acl: is_api") {
		t.Errorf("acl names missing:\n%s", fe)
	}
	if _, err := SectionContext(srcs, 9, LangES); err == nil {
		t.Error("out-of-range section must fail")
	}
	if _, err := SectionContext([]Source{{Path: "x", Text: strings.Repeat("a", MaxAssistContext+1)}}, 0, LangEN); err == nil {
		t.Error("huge configuration must be rejected")
	}
	big := "backend b\n" + strings.Repeat("    # "+strings.Repeat("x", 100)+"\n", MaxProposeSection/100)
	if _, err := SectionContext([]Source{{Path: "x", Text: big}}, 0, LangEN); err == nil {
		t.Error("huge section must be rejected")
	}
}

func TestProposeSystemPromptLangs(t *testing.T) {
	es, en := ProposeSystemPrompt(LangES), ProposeSystemPrompt(LangEN)
	if es == en || !strings.Contains(es, "en español") || !strings.Contains(en, "in English") || !strings.Contains(en, `"directives"`) {
		t.Error("prompt must follow the language and document the format")
	}
}

func TestParseProposedRobust(t *testing.T) {
	good := `{"directives":[{"text":"  timeout server 30s ","explanation":"x"},{"text":"frobnicate 1","explanation":"y"}],"note":"ojo"}`
	for name, raw := range map[string]string{
		"plain":  good,
		"fenced": "Here you go:\n```json\n" + good + "\n```\nHope it helps {not json}",
		"noise":  "{oops} text " + good,
	} {
		dirs, note := ParseProposed(raw, LangEN)
		if len(dirs) != 2 || dirs[0].Text != "timeout server 30s" || !dirs[0].Known || dirs[1].Known || dirs[0].At != -1 {
			t.Errorf("%s: %+v", name, dirs)
		}
		if note != "ojo" {
			t.Errorf("%s note: %q", name, note)
		}
	}
	for _, raw := range []string{"", "no json", `{"directives":`, `{"other":1}`, `[1,2]`} {
		dirs, note := ParseProposed(raw, LangES)
		if len(dirs) != 0 || note == "" {
			t.Errorf("%q: %v %q", raw, dirs, note)
		}
	}
}

func TestParseProposedValidation(t *testing.T) {
	var items []string
	items = append(items, `{"text":"backend x"}`, `{"text":"a\nb"}`, `{"text":""}`, `{"text":"# comment"}`, `{"text":"global"}`,
		`{"text":"`+strings.Repeat("a", MaxProposedLine+1)+`"}`, `{"text":"mode http"}`, `{"text":"mode   http"}`)
	for i := 0; i < 20; i++ {
		items = append(items, `{"text":"http-request set-header X-N`+string(rune('a'+i))+` 1"}`)
	}
	dirs, note := ParseProposed(`{"directives":[`+strings.Join(items, ",")+`]}`, LangEN)
	if len(dirs) != MaxProposedDirectives || dirs[0].Text != "mode http" {
		t.Fatalf("got %d: %+v", len(dirs), dirs[0])
	}
	if !strings.Contains(note, "6 proposals") || !strings.Contains(note, "maximum of 12") {
		t.Errorf("note: %q", note)
	}
}

func TestBuildProposalDuplicatesIssuesAndPosition(t *testing.T) {
	raw := `{"directives":[{"text":"server a1 10.0.0.1:80 check"},{"text":"server  a2 10.0.0.2:80 check","explanation":"e"},{"text":"balance roundrobin"}]}`
	dirs, note, err := BuildProposal(proposeSrcs(), 2, raw, LangEN)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || dirs[0].Text != "server  a2 10.0.0.2:80 check" || dirs[0].At != 1 || dirs[1].At != -1 {
		t.Fatalf("%+v", dirs)
	}
	if !strings.Contains(note, "1 directives the section already has") {
		t.Errorf("note: %q", note)
	}

	// A directive that introduces a problem reports it; an innocuous one does not.
	raw = `{"directives":[{"text":"default_backend ghost"},{"text":"timeout server 30s"}]}`
	dirs, _, err = BuildProposal(proposeSrcs(), 1, raw, LangEN)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 {
		t.Fatalf("%+v", dirs)
	}
	found := false
	for _, i := range dirs[0].IssuesAfter {
		found = found || i.Rule == "backend-missing"
	}
	if !found || len(dirs[1].IssuesAfter) != 0 {
		t.Errorf("issues: %+v / %+v", dirs[0].IssuesAfter, dirs[1].IssuesAfter)
	}
	// Spanish messages.
	dirs, _, _ = BuildProposal(proposeSrcs(), 1, `{"directives":[{"text":"default_backend ghost"}]}`, LangES)
	if len(dirs) != 1 || len(dirs[0].IssuesAfter) == 0 || strings.Contains(dirs[0].IssuesAfter[0].Message, "does not") {
		t.Errorf("es: %+v", dirs)
	}
	if _, _, err := BuildProposal(proposeSrcs(), 7, raw, LangES); err == nil {
		t.Error("section out of range must fail")
	}
}
