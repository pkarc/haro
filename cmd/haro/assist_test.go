package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/pkarc/haro/core"
	harov1 "github.com/pkarc/haro/gen/haro/v1"
	"github.com/pkarc/haro/gen/haro/v1/harov1connect"
)

const assistCfgText = "defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend web\n    bind *:80\n    default_backend nope\n\nbackend app\n    server a1 10.0.0.1:80 check\n"

// fakeStreamer devuelve una respuesta fija y recuerda lo que recibió.
type fakeStreamer struct {
	reply  string
	result assistResult
	err    error

	system string
	msgs   []core.AssistMessage
}

func (f *fakeStreamer) Stream(_ context.Context, system string, msgs []core.AssistMessage, onText func(string) error) (assistResult, error) {
	f.system, f.msgs = system, msgs
	for _, chunk := range strings.SplitAfter(f.reply, "\n") { // en trozos, como un stream
		if chunk == "" {
			continue
		}
		if err := onText(chunk); err != nil {
			return f.result, err
		}
	}
	return f.result, f.err
}

const fixReply = "Cambio el backend por defecto a `app`.\n\n```haproxy\n" +
	"defaults\n    mode http\n    timeout connect 5s\n    timeout client 30s\n    timeout server 30s\n\nfrontend web\n    bind *:80\n    default_backend app\n\nbackend app\n    server a1 10.0.0.1:80 check\n" +
	"```\n"

func TestRunAssistBuildsContextAndExtractsProposal(t *testing.T) {
	f := &fakeStreamer{reply: fixReply, result: assistResult{StopReason: "end_turn"}}
	srcs := []core.Source{{Path: "/etc/haproxy/haproxy.cfg", Text: assistCfgText}}
	var streamed strings.Builder
	out, err := runAssist(context.Background(), f, srcs,
		[]core.AssistMessage{{Role: "user", Content: "arregla el backend"}}, core.LangES,
		func(s string) error { streamed.WriteString(s); return nil })
	if err != nil {
		t.Fatal(err)
	}
	// El contexto va delante de la petición del último turno, con la configuración y sus problemas.
	last := f.msgs[len(f.msgs)-1].Content
	for _, want := range []string{"<haro_context>", `<file path="/etc/haproxy/haproxy.cfg">`, "default_backend nope", "[backend-missing]", "arregla el backend"} {
		if !strings.Contains(last, want) {
			t.Errorf("falta %q en el último mensaje:\n%s", want, last)
		}
	}
	if !strings.Contains(f.system, "Responde en español") {
		t.Error("el prompt de sistema debe seguir el idioma")
	}
	if streamed.String() != fixReply || out.Text != fixReply {
		t.Error("el texto debe entregarse completo y en orden")
	}
	if len(out.Proposal.Changes) != 1 || !strings.Contains(out.Proposal.Changes[0].Text, "default_backend app") {
		t.Fatalf("propuesta: %+v", out.Proposal)
	}
	for _, i := range out.IssuesAfter {
		if i.Rule == "backend-missing" {
			t.Errorf("la propuesta corrige el backend, pero IssuesAfter lo sigue dando: %+v", i)
		}
	}
}

func TestRunAssistDoesNotMutateInput(t *testing.T) {
	f := &fakeStreamer{reply: "ok"}
	msgs := []core.AssistMessage{{Role: "user", Content: "hola"}}
	if _, err := runAssist(context.Background(), f, []core.Source{{Path: "a.cfg", Text: assistCfgText}}, msgs, core.LangEN, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if msgs[0].Content != "hola" {
		t.Error("runAssist no debe modificar la conversación del llamador")
	}
	if !strings.Contains(f.system, "Answer in English") {
		t.Error("prompt en inglés")
	}
}

func TestValidateConversation(t *testing.T) {
	u := core.AssistMessage{Role: "user", Content: "a"}
	a := core.AssistMessage{Role: "assistant", Content: "b"}
	for name, msgs := range map[string][]core.AssistMessage{
		"vacía":              nil,
		"acaba en asistente": {u, a},
		"roles invertidos":   {a, u},
		"mensaje vacío":      {{Role: "user", Content: "  "}},
		"rol raro":           {{Role: "system", Content: "x"}},
	} {
		if err := validateConversation(msgs); err == nil {
			t.Errorf("%s: debería rechazarse", name)
		}
	}
	long := make([]core.AssistMessage, 0, 45)
	for i := 0; i < 41; i++ {
		long = append(long, []core.AssistMessage{u, a}[i%2])
	}
	if err := validateConversation(long); err == nil {
		t.Error("demasiados mensajes")
	}
	if err := validateConversation([]core.AssistMessage{u, a, u}); err != nil {
		t.Errorf("conversación válida rechazada: %v", err)
	}
}

func assistClient(t *testing.T, s *server) (harov1connect.AssistServiceClient, clients) {
	t.Helper()
	c := newTestServer(t, s)
	return harov1connect.NewAssistServiceClient(http.DefaultClient, c.url), c
}

func TestAssistRPCStreamsTextProposalDone(t *testing.T) {
	f := &fakeStreamer{reply: fixReply, result: assistResult{StopReason: "end_turn", InputTokens: 100, OutputTokens: 50}}
	cl, c := assistClient(t, &server{assist: f, assistModel: "claude-test"})

	caps, err := c.config.GetCapabilities(context.Background(), connect.NewRequest(&harov1.GetCapabilitiesRequest{}))
	if err != nil || !caps.Msg.CanAssist || caps.Msg.AssistantModel != "claude-test" {
		t.Fatalf("capabilities: %+v, %v", caps, err)
	}

	stream, err := cl.Assist(context.Background(), connect.NewRequest(&harov1.AssistRequest{
		Files:    []*harov1.SourceFile{file("a.cfg", assistCfgText)},
		Messages: []*harov1.AssistMessage{{Role: "user", Content: "arregla"}},
		Lang:     "es",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var prop *harov1.Proposal
	var done *harov1.AssistDone
	var order []string
	for stream.Receive() {
		switch ev := stream.Msg().Event.(type) {
		case *harov1.AssistEvent_Text:
			text.WriteString(ev.Text)
			order = append(order, "text")
		case *harov1.AssistEvent_Proposal:
			prop = ev.Proposal
			order = append(order, "proposal")
		case *harov1.AssistEvent_Done:
			done = ev.Done
			order = append(order, "done")
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if text.String() != fixReply {
		t.Errorf("texto: %q", text.String())
	}
	if prop == nil || len(prop.Changes) != 1 || prop.Changes[0].Path != "a.cfg" || !strings.Contains(prop.Changes[0].Text, "default_backend app") {
		t.Fatalf("propuesta: %+v", prop)
	}
	for _, i := range prop.IssuesAfter {
		if i.Rule == "backend-missing" {
			t.Error("issues_after no debe contener el problema corregido")
		}
	}
	if done == nil || done.StopReason != "end_turn" || done.InputTokens != 100 || done.OutputTokens != 50 {
		t.Fatalf("done: %+v", done)
	}
	if got := order[len(order)-1]; got != "done" || order[len(order)-2] != "proposal" {
		t.Errorf("orden de eventos: %v", order)
	}
}

func TestAssistRPCDisabledAndInvalid(t *testing.T) {
	cl, c := assistClient(t, &server{}) // sin --assistant
	stream, err := cl.Assist(context.Background(), connect.NewRequest(&harov1.AssistRequest{Messages: []*harov1.AssistMessage{{Role: "user", Content: "x"}}}))
	if err == nil {
		stream.Receive()
		err = stream.Err()
	}
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("sin --assistant debe ser Unimplemented: %v", err)
	}
	caps, _ := c.config.GetCapabilities(context.Background(), connect.NewRequest(&harov1.GetCapabilitiesRequest{}))
	if caps.Msg.CanAssist {
		t.Error("can_assist debe ser false")
	}

	cl2, _ := assistClient(t, &server{assist: &fakeStreamer{reply: "x"}})
	for name, req := range map[string]*harov1.AssistRequest{
		"sin mensajes":       {},
		"idioma malo":        {Messages: []*harov1.AssistMessage{{Role: "user", Content: "x"}}, Lang: "fr"},
		"acaba en asistente": {Messages: []*harov1.AssistMessage{{Role: "user", Content: "x"}, {Role: "assistant", Content: "y"}}},
	} {
		stream, err := cl2.Assist(context.Background(), connect.NewRequest(req))
		if err == nil {
			stream.Receive()
			err = stream.Err()
		}
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: debe ser InvalidArgument, fue %v", name, err)
		}
	}
}

func TestAssistRPCUpstreamErrorClosesStream(t *testing.T) {
	cl, _ := assistClient(t, &server{assist: &fakeStreamer{reply: "parcial", err: errors.New("la API de Anthropic rechazó las credenciales")}})
	stream, err := cl.Assist(context.Background(), connect.NewRequest(&harov1.AssistRequest{Messages: []*harov1.AssistMessage{{Role: "user", Content: "x"}}}))
	if err != nil {
		t.Fatal(err)
	}
	for stream.Receive() {
	}
	if err := stream.Err(); err == nil || !strings.Contains(err.Error(), "credenciales") {
		t.Errorf("el error del proveedor debe llegar al cliente: %v", err)
	}
}

// El asistente nunca ofrece un archivo cortado: una respuesta que termina
// dentro de un bloque no genera cambios.
func TestAssistRPCTruncatedBlockIsNotProposed(t *testing.T) {
	f := &fakeStreamer{reply: "Aquí va:\n```haproxy\nglobal\n    maxconn 10\n", result: assistResult{StopReason: "max_tokens"}}
	cl, _ := assistClient(t, &server{assist: f})
	stream, _ := cl.Assist(context.Background(), connect.NewRequest(&harov1.AssistRequest{
		Files: []*harov1.SourceFile{file("a.cfg", assistCfgText)}, Messages: []*harov1.AssistMessage{{Role: "user", Content: "x"}}}))
	var prop *harov1.Proposal
	var done *harov1.AssistDone
	for stream.Receive() {
		switch ev := stream.Msg().Event.(type) {
		case *harov1.AssistEvent_Proposal:
			prop = ev.Proposal
		case *harov1.AssistEvent_Done:
			done = ev.Done
		}
	}
	if prop == nil || len(prop.Changes) != 0 || !prop.Truncated {
		t.Errorf("propuesta: %+v", prop)
	}
	if done == nil || done.StopReason != "max_tokens" {
		t.Errorf("done: %+v", done)
	}
}

// ---------- el streamer real contra una API falsa ----------

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		var m struct{ Type string }
		json.Unmarshal([]byte(e), &m)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", m.Type, e)
	}
	return b.String()
}

func fakeAnthropic(t *testing.T, status int, body string, got *map[string]any) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			json.Unmarshal(b, got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestAnthropicStreamerEndToEnd(t *testing.T) {
	body := sse(
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":123,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"pensando"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hola "}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"mundo"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":42}}`,
		`{"type":"message_stop"}`,
	)
	var req map[string]any
	ts := fakeAnthropic(t, 200, body, &req)
	st := newAnthropicStreamer("", option.WithBaseURL(ts.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))

	var got strings.Builder
	res, err := st.Stream(context.Background(), "SISTEMA", []core.AssistMessage{{Role: "user", Content: "uno"}, {Role: "assistant", Content: "dos"}, {Role: "user", Content: "tres"}},
		func(s string) error { got.WriteString(s); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Hola mundo" {
		t.Errorf("solo el texto debe entregarse (no el pensamiento): %q", got.String())
	}
	if res.StopReason != "end_turn" || res.InputTokens != 123 || res.OutputTokens != 42 {
		t.Errorf("resultado: %+v", res)
	}
	// La petición: modelo por defecto, streaming, pensamiento adaptativo, sistema cacheable y roles en orden.
	if req["model"] != "claude-opus-5-5" || req["stream"] != true {
		t.Errorf("petición: %v", req)
	}
	if th, _ := req["thinking"].(map[string]any); th["type"] != "adaptive" {
		t.Errorf("thinking: %v", req["thinking"])
	}
	sys := req["system"].([]any)[0].(map[string]any)
	if sys["text"] != "SISTEMA" || sys["cache_control"] == nil {
		t.Errorf("system: %v", sys)
	}
	msgs := req["messages"].([]any)
	if len(msgs) != 3 || msgs[1].(map[string]any)["role"] != "assistant" || msgs[2].(map[string]any)["role"] != "user" {
		t.Errorf("mensajes: %v", msgs)
	}
	if _, has := req["temperature"]; has {
		t.Error("no debe enviar temperature (no se admite en los modelos actuales)")
	}
	if mt := req["max_tokens"].(float64); mt < 16000 {
		t.Errorf("max_tokens demasiado bajo: %v", mt)
	}
}

func TestAnthropicStreamerRefusalAndErrors(t *testing.T) {
	body := sse(
		`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"x","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":null}},"usage":{"output_tokens":0}}`,
		`{"type":"message_stop"}`,
	)
	ts := fakeAnthropic(t, 200, body, nil)
	st := newAnthropicStreamer("claude-sonnet-5-5", option.WithBaseURL(ts.URL), option.WithAPIKey("k"), option.WithMaxRetries(0))
	res, err := st.Stream(context.Background(), "s", []core.AssistMessage{{Role: "user", Content: "x"}}, func(string) error { return nil })
	if err != nil || res.StopReason != "refusal" || res.RefusalCategory != "cyber" {
		t.Errorf("refusal: %+v, %v", res, err)
	}

	for status, want := range map[int]string{401: "credenciales", 404: "modelo no encontrado", 429: "límite de uso"} {
		ts := fakeAnthropic(t, status, fmt.Sprintf(`{"type":"error","error":{"type":"x","message":"m%d"}}`, status), nil)
		st := newAnthropicStreamer("", option.WithBaseURL(ts.URL), option.WithAPIKey("k"), option.WithMaxRetries(0))
		_, err := st.Stream(context.Background(), "s", []core.AssistMessage{{Role: "user", Content: "x"}}, func(string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("HTTP %d: error %v, quería «%s»", status, err, want)
		}
	}
}

// ---------- haro assist (CLI) ----------

func TestFinishAssistCLIWriteValidatesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "haproxy.cfg")
	os.WriteFile(p, []byte(assistCfgText), 0o640)
	srcs, _ := readSources([]string{p})
	good := assistOutcome{Result: assistResult{StopReason: "end_turn"}, Proposal: core.ExtractProposal(strings.ReplaceAll(fixReply, "```haproxy\n", "```haproxy path="+p+"\n"), srcs)}
	if len(good.Proposal.Changes) != 1 {
		t.Fatalf("propuesta de prueba: %+v", good.Proposal)
	}
	// Sin --write no escribe.
	if err := finishAssistCLI(good, srcs, []string{p}, false, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != assistCfgText {
		t.Error("sin --write no debe tocar el archivo")
	}
	// Con --write --yes escribe y deja .bak.
	if err := finishAssistCLI(good, srcs, []string{p}, true, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "default_backend app") {
		t.Errorf("no se escribió: %s", b)
	}
	if b, _ := os.ReadFile(p + ".bak"); string(b) != assistCfgText {
		t.Error("falta la copia .bak con el original")
	}
	// Una propuesta que rompe la configuración no se guarda.
	os.WriteFile(p, []byte(assistCfgText), 0o640)
	srcs, _ = readSources([]string{p})
	bad := assistOutcome{Result: assistResult{StopReason: "end_turn"}, Proposal: core.Proposal{Changes: []core.FileChange{{File: 0, Path: p, Text: strings.Replace(assistCfgText, "default_backend nope", "default_backend otro_inexistente", 1)}}}}
	err := finishAssistCLI(bad, srcs, []string{p}, true, true)
	if err == nil || !strings.Contains(err.Error(), "no se guardó") {
		t.Errorf("una propuesta inválida no debe guardarse: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != assistCfgText {
		t.Error("el archivo cambió pese al rechazo")
	}
	// Una negativa del modelo es un error claro.
	if err := finishAssistCLI(assistOutcome{Result: assistResult{StopReason: "refusal", RefusalCategory: "cyber"}}, srcs, nil, false, false); err == nil || !strings.Contains(err.Error(), "declinó") {
		t.Errorf("refusal: %v", err)
	}
}
