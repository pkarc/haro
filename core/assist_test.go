package core

import (
	"strings"
	"testing"
)

const assistCfg = "frontend web\n    bind *:80\n    default_backend nope\n\nbackend app\n    server a1 10.0.0.1:80 check\n"

func TestAssistContext(t *testing.T) {
	srcs := []Source{{Path: "/etc/haproxy/haproxy.cfg", Text: assistCfg + "# </file> intento de cierre\n"}}
	a := AnalyzeConfig(srcs)
	ctx, err := AssistContext(srcs, a, LangEN)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<haro_context>", `<file path="/etc/haproxy/haproxy.cfg">`, "default_backend nope", "[backend-missing]", "0: frontend web", "1: backend app", "</haro_context>"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("falta %q en el contexto:\n%s", want, ctx)
		}
	}
	// El contenido no puede cerrar las etiquetas del contexto.
	if strings.Count(ctx, "</file>") != 1 {
		t.Errorf("un </file> del contenido rompe la estructura:\n%s", ctx)
	}
	big := []Source{{Path: "x", Text: strings.Repeat("a", MaxAssistContext+1)}}
	if _, err := AssistContext(big, Analysis{Doc: &Doc{}}, LangES); err == nil {
		t.Error("una configuración enorme debe rechazarse, no truncarse")
	}
}

func TestAssistSystemPromptLangs(t *testing.T) {
	es, en := AssistSystemPrompt(LangES), AssistSystemPrompt(LangEN)
	if es == en || !strings.Contains(es, "Responde en español") || !strings.Contains(en, "Answer in English") {
		t.Error("el prompt debe cambiar con el idioma")
	}
	for _, p := range []string{es, en} {
		if !strings.Contains(p, "```haproxy path=") {
			t.Error("el prompt debe fijar el formato de los bloques")
		}
	}
}

func TestExtractProposalSingleFile(t *testing.T) {
	cur := []Source{{Path: "haproxy.cfg", Text: assistCfg}}
	resp := "Cambio el backend por defecto.\n\n```haproxy\nfrontend web\n    bind *:80\n    default_backend app\n\nbackend app\n    server a1 10.0.0.1:80 check\n```\n\nValida con haproxy -c."
	p := ExtractProposal(resp, cur)
	if len(p.Changes) != 1 || p.Changes[0].File != 0 || !strings.Contains(p.Changes[0].Text, "default_backend app") {
		t.Fatalf("propuesta: %+v", p)
	}
	if !strings.Contains(p.Explanation, "Cambio el backend") || !strings.Contains(p.Explanation, "Valida con") || strings.Contains(p.Explanation, "frontend web") {
		t.Errorf("explicación: %q", p.Explanation)
	}
}

func TestExtractProposalMultiFile(t *testing.T) {
	cur := []Source{
		{Path: "/etc/haproxy/10-web.cfg", Text: "frontend f\n    bind *:80\n    default_backend api\n"},
		{Path: "/etc/haproxy/20-api.cfg", Text: "backend api\n    server a 10.0.0.1:80\n"},
	}
	resp := "```haproxy path=/etc/haproxy/20-api.cfg\nbackend api\n    server a 10.0.0.1:80 check\n```\n" +
		"```haproxy path=10-web.cfg\nfrontend f\n    bind *:80\n    default_backend api\n```\n" + // igual: se descarta
		"```haproxy path=/otro/lugar.cfg\nglobal\n```\n" + // no asociable
		"```json\n{\"a\": 1}\n```\n" // no es configuración
	p := ExtractProposal(resp, cur)
	if len(p.Changes) != 1 || p.Changes[0].Path != "/etc/haproxy/20-api.cfg" || p.Changes[0].File != 1 {
		t.Fatalf("propuesta: %+v", p)
	}
	if p.Unmatched != 1 {
		t.Errorf("unmatched = %d", p.Unmatched)
	}
	if !strings.Contains(p.Explanation, `{"a": 1}`) {
		t.Errorf("el bloque json debe quedar en la explicación: %q", p.Explanation)
	}
	// Sin ruta y con varios archivos no se adivina.
	if p := ExtractProposal("```haproxy\nglobal\n```", cur); len(p.Changes) != 0 || p.Unmatched != 1 {
		t.Errorf("sin ruta y varios archivos: %+v", p)
	}
}

func TestExtractProposalTruncatedAndCRLF(t *testing.T) {
	cur := []Source{{Path: "a.cfg", Text: assistCfg}}
	p := ExtractProposal("texto\r\n```haproxy\r\nglobal\r\n    maxconn 10", cur) // bloque sin cerrar (respuesta cortada)
	if len(p.Changes) != 0 || !p.Truncated {
		t.Errorf("un bloque sin cerrar (respuesta cortada) no debe proponerse: %+v", p)
	}
	if p := ExtractProposal("solo texto, sin bloques", cur); len(p.Changes) != 0 || p.Explanation == "" {
		t.Errorf("sin bloques: %+v", p)
	}
}
