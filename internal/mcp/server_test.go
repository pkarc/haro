package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func testServer() *Server {
	return &Server{
		Name: "t", Version: "1", Instructions: "ayuda",
		Tools: []Tool{
			{Name: "eco", Description: "d", InputSchema: map[string]any{"type": "object"}, ReadOnly: true,
				Handler: func(_ context.Context, a json.RawMessage) (Result, error) {
					var in struct{ X string }
					json.Unmarshal(a, &in)
					if in.X == "falla" {
						return Result{}, errString("mal")
					}
					return Result{Text: "eco " + in.X, Structured: map[string]any{"x": in.X}}, nil
				}},
		},
		Resources: []Resource{{URI: "t://r", Name: "r", MIMEType: "text/plain", Read: func() (string, error) { return "hola", nil }}},
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// session envía las líneas y devuelve las respuestas (una por línea).
func session(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("la salida no es JSON por líneas: %q", l)
		}
		res = append(res, m)
	}
	return res
}

func result(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("se esperaba result: %v", m)
	}
	return r
}

func TestLegacyHandshake(t *testing.T) {
	res := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)
	if len(res) != 3 {
		t.Fatalf("la notificación no debe tener respuesta: %d respuestas", len(res))
	}
	init := result(t, res[0])
	if init["protocolVersion"] != "2025-06-18" {
		t.Errorf("debe devolver la misma versión que pide el cliente: %v", init["protocolVersion"])
	}
	if init["serverInfo"].(map[string]any)["name"] != "t" || init["instructions"] != "ayuda" {
		t.Errorf("initialize incompleto: %v", init)
	}
	caps := init["capabilities"].(map[string]any)
	if caps["tools"] == nil || caps["resources"] == nil {
		t.Errorf("capabilities: %v", caps)
	}
	list := result(t, res[1])
	if _, has := list["resultType"]; has {
		t.Error("la era legada no lleva resultType")
	}
	tool := list["tools"].([]any)[0].(map[string]any)
	if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
		t.Errorf("annotations: %v", tool)
	}
}

func TestLegacyUnknownVersionGetsLatest(t *testing.T) {
	res := session(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := result(t, res[0])["protocolVersion"]; v != LegacyVersions[0] {
		t.Errorf("versión desconocida debe recibir la más reciente de la era legada: %v", v)
	}
}

func TestModernDiscoverAndCalls(t *testing.T) {
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"c","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
	res := session(t, testServer(),
		`{"jsonrpc":"2.0","id":"d","method":"server/discover","params":{`+meta+`}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"eco","arguments":{"x":"hi"},`+meta+`}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{`+meta+`}}`,
	)
	d := result(t, res[0])
	if d["resultType"] != "complete" || d["supportedVersions"].([]any)[0] != ModernVersion {
		t.Errorf("discover: %v", d)
	}
	if d["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"] == nil {
		t.Errorf("discover sin serverInfo: %v", d)
	}
	call := result(t, res[1])
	if call["resultType"] != "complete" || call["isError"] != false {
		t.Errorf("tools/call moderno: %v", call)
	}
	if call["structuredContent"].(map[string]any)["x"] != "hi" {
		t.Errorf("structuredContent: %v", call)
	}
	if result(t, res[2])["resultType"] != "complete" {
		t.Error("tools/list moderno debe llevar resultType")
	}
}

func TestUnsupportedModernVersion(t *testing.T) {
	res := session(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1900-01-01"}}}`)
	e := res[0]["error"].(map[string]any)
	if e["code"].(float64) != CodeUnsupportedVersion {
		t.Fatalf("código: %v", e)
	}
	data := e["data"].(map[string]any)
	if data["requested"] != "1900-01-01" || data["supported"].([]any)[0] != ModernVersion {
		t.Errorf("data: %v", data)
	}
}

func TestToolErrorsAndProtocolErrors(t *testing.T) {
	res := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"eco","arguments":{"x":"falla"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nada"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"no/existe"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"eco"}}`,
		`esto no es json`,
	)
	r := result(t, res[0])
	if r["isError"] != true || !strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), "mal") {
		t.Errorf("el error de la herramienta debe ir en el resultado con isError: %v", r)
	}
	if res[1]["error"].(map[string]any)["code"].(float64) != CodeInvalidParams {
		t.Errorf("herramienta desconocida: %v", res[1])
	}
	if res[2]["error"].(map[string]any)["code"].(float64) != CodeMethodNotFound {
		t.Errorf("método desconocido: %v", res[2])
	}
	if result(t, res[3])["isError"] != false {
		t.Errorf("sin arguments debe llamar con {}: %v", res[3])
	}
	if res[4]["error"].(map[string]any)["code"].(float64) != CodeParse || res[4]["id"] != nil {
		t.Errorf("JSON inválido: %v", res[4])
	}
}

func TestResources(t *testing.T) {
	res := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"t://r"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"t://x"}}`,
	)
	if got := result(t, res[0])["resources"].([]any); len(got) != 1 {
		t.Errorf("resources/list: %v", got)
	}
	c := result(t, res[1])["contents"].([]any)[0].(map[string]any)
	if c["text"] != "hola" || c["mimeType"] != "text/plain" {
		t.Errorf("resources/read: %v", c)
	}
	if res[2]["error"] == nil {
		t.Error("recurso inexistente debe dar error")
	}
}

func TestNoOutputBesidesMessages(t *testing.T) {
	// Cada línea de salida es un mensaje JSON-RPC completo y sin saltos internos.
	res := session(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"eco","arguments":{"x":"a\nb"}}}`)
	if len(res) != 1 {
		t.Fatalf("una petición, una línea: %d", len(res))
	}
}

func TestLargeMessage(t *testing.T) {
	big := strings.Repeat("x", 3<<20)
	res := session(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"eco","arguments":{"x":"`+big+`"}}}`)
	if r := result(t, res[0]); r["isError"] != false {
		t.Errorf("un mensaje grande debe procesarse: %v", r["isError"])
	}
}
