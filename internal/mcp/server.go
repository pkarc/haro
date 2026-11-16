// Package mcp implementa un servidor MCP (Model Context Protocol) sobre stdio
// con la biblioteca estándar. Habla las dos «eras» del protocolo:
//
//   - legado (2025-11-25 y anteriores): sesión con el handshake initialize;
//   - moderna (2026-07-28): sin sesión, con la versión en _meta de cada
//     petición y server/discover.
//
// El cliente decide: si abre con initialize, se le sirve el protocolo legado;
// si sus peticiones llevan _meta con la versión, el moderno. Cada mensaje es
// una línea JSON (sin saltos de línea dentro), como pide el transporte stdio.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
)

// Versiones del protocolo que se hablan.
const (
	ModernVersion = "2026-07-28"
)

// LegacyVersions son las versiones con handshake initialize, de la más nueva a la más antigua.
var LegacyVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Códigos de error JSON-RPC.
const (
	CodeParse              = -32700
	CodeInvalidRequest     = -32600
	CodeMethodNotFound     = -32601
	CodeInvalidParams      = -32602
	CodeInternal           = -32603
	CodeUnsupportedVersion = -32022
)

const (
	metaVersion = "io.modelcontextprotocol/protocolVersion"
	metaServer  = "io.modelcontextprotocol/serverInfo"
)

// Tool es una herramienta expuesta al modelo.
type Tool struct {
	Name         string
	Title        string
	Description  string
	InputSchema  map[string]any
	OutputSchema map[string]any
	// ReadOnly marca herramientas que no modifican nada (annotations.readOnlyHint).
	ReadOnly bool
	// Destructive indica que puede sobrescribir datos (solo tiene sentido si !ReadOnly).
	Destructive bool
	// Handler recibe los argumentos en JSON. Un error se devuelve al modelo como
	// resultado con isError (para que pueda corregir la llamada), no como error de protocolo.
	Handler func(ctx context.Context, args json.RawMessage) (Result, error)
}

// Result es el resultado de una herramienta: texto para leer y, opcionalmente,
// un objeto estructurado (que se duplica serializado en el texto si no hay otro).
type Result struct {
	Text       string
	Structured any
}

// Resource es un recurso legible.
type Resource struct {
	URI         string
	Name        string
	Description string
	MIMEType    string
	Read        func() (string, error)
}

// Server es un servidor MCP.
type Server struct {
	Name, Title, Version string
	Instructions         string
	Tools                []Tool
	Resources            []Resource
	// Log recibe mensajes de diagnóstico (stderr); puede ser nil.
	Log func(format string, args ...any)

	mu  sync.Mutex // serializa las escrituras en la salida
	out io.Writer
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// maxMessage es el tamaño máximo de un mensaje (una línea).
const maxMessage = 16 << 20

// Serve lee mensajes de r y escribe las respuestas en w hasta que r se cierra
// (fin de entrada: el cliente terminó) o ctx se cancela. Las peticiones se
// atienden de una en una, en orden.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.out = w
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := readLine(br)
		if len(line) > 0 {
			s.handleLine(ctx, line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// readLine lee una línea completa, sin límite del buffer del lector pero con
// tope de tamaño total.
func readLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		buf = append(buf, chunk...)
		if len(buf) > maxMessage {
			return nil, fmt.Errorf("mensaje de más de %d bytes", maxMessage)
		}
		if err != nil {
			return buf, err
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

func (s *Server) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		s.logf("no se pudo serializar la respuesta: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out.Write(append(b, '\n'))
}

func (s *Server) reply(id json.RawMessage, result any) {
	s.write(response{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) fail(id json.RawMessage, code int, msg string, data any) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	s.write(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}})
}

func (s *Server) handleLine(ctx context.Context, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.fail(nil, CodeParse, "invalid JSON", nil)
		return
	}
	if req.Method == "" {
		// Una respuesta del cliente (no se le piden): se ignora.
		return
	}
	isNotification := len(req.ID) == 0
	result, err := s.dispatch(ctx, req)
	if isNotification {
		return
	}
	if err != nil {
		var re *rpcError
		if errors.As(err, &re) {
			s.fail(req.ID, re.Code, re.Message, re.Data)
		} else {
			s.fail(req.ID, CodeInternal, err.Error(), nil)
		}
		return
	}
	s.reply(req.ID, result)
}

// modern devuelve si la petición es de la era moderna (lleva la versión en
// _meta) y la versión pedida.
func modern(params json.RawMessage) (bool, string) {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(params) == 0 || json.Unmarshal(params, &p) != nil {
		return false, ""
	}
	raw, ok := p.Meta[metaVersion]
	if !ok {
		return false, ""
	}
	var v string
	json.Unmarshal(raw, &v)
	return true, v
}

func (s *Server) dispatch(ctx context.Context, req request) (any, error) {
	isModern, version := modern(req.Params)
	if isModern && version != ModernVersion {
		return nil, &rpcError{Code: CodeUnsupportedVersion, Message: "Unsupported protocol version",
			Data: map[string]any{"supported": []string{ModernVersion}, "requested": version}}
	}
	// Las respuestas de la era moderna llevan resultType.
	wrap := func(m map[string]any) map[string]any {
		if isModern {
			m["resultType"] = "complete"
		}
		return m
	}

	switch req.Method {
	case "initialize":
		return s.initialize(req.Params)
	case "server/discover":
		res := map[string]any{
			"resultType":        "complete",
			"supportedVersions": []string{ModernVersion},
			"capabilities":      s.capabilities(),
			"_meta":             map[string]any{metaServer: s.serverInfo()},
			"ttlMs":             3600000,
			"cacheScope":        "public",
		}
		if s.Instructions != "" {
			res["instructions"] = s.Instructions
		}
		return res, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return wrap(map[string]any{"tools": s.toolList()}), nil
	case "tools/call":
		res, err := s.callTool(ctx, req.Params)
		if err != nil {
			return nil, err
		}
		return wrap(res), nil
	case "resources/list":
		return wrap(map[string]any{"resources": s.resourceList()}), nil
	case "resources/templates/list":
		return wrap(map[string]any{"resourceTemplates": []any{}}), nil
	case "resources/read":
		res, err := s.readResource(req.Params)
		if err != nil {
			return nil, err
		}
		return wrap(res), nil
	}
	if len(req.ID) == 0 { // notificaciones (initialized, cancelled…): sin respuesta
		return nil, nil
	}
	return nil, &rpcError{Code: CodeMethodNotFound, Message: "Method not found: " + req.Method}
}

func (s *Server) capabilities() map[string]any {
	caps := map[string]any{}
	if len(s.Tools) > 0 {
		caps["tools"] = map[string]any{}
	}
	if len(s.Resources) > 0 {
		caps["resources"] = map[string]any{}
	}
	return caps
}

func (s *Server) serverInfo() map[string]any {
	info := map[string]any{"name": s.Name, "version": s.Version}
	if s.Title != "" {
		info["title"] = s.Title
	}
	return info
}

func (s *Server) initialize(params json.RawMessage) (any, error) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "initialize: invalid params"}
	}
	// Si se conoce la versión pedida, se responde con la misma; si no, con la
	// más reciente de la era legada.
	version := LegacyVersions[0]
	for _, v := range LegacyVersions {
		if v == p.ProtocolVersion {
			version = v
		}
	}
	res := map[string]any{
		"protocolVersion": version,
		"capabilities":    s.capabilities(),
		"serverInfo":      s.serverInfo(),
	}
	if s.Instructions != "" {
		res["instructions"] = s.Instructions
	}
	return res, nil
}

func (s *Server) toolList() []map[string]any {
	tools := make([]map[string]any, 0, len(s.Tools))
	for _, t := range s.Tools {
		ann := map[string]any{"readOnlyHint": t.ReadOnly, "openWorldHint": false}
		if !t.ReadOnly {
			ann["destructiveHint"] = t.Destructive
		}
		m := map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema, "annotations": ann}
		if t.Title != "" {
			m["title"] = t.Title
		}
		if t.OutputSchema != nil {
			m["outputSchema"] = t.OutputSchema
		}
		tools = append(tools, m)
	}
	return tools
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (map[string]any, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "tools/call: falta name"}
	}
	for _, t := range s.Tools {
		if t.Name != p.Name {
			continue
		}
		if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
			p.Arguments = json.RawMessage("{}")
		}
		res, err := t.Handler(ctx, p.Arguments)
		if err != nil {
			// Los errores de la herramienta los ve el modelo para corregirse.
			return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		text := res.Text
		out := map[string]any{"isError": false}
		if res.Structured != nil {
			out["structuredContent"] = res.Structured
			if text == "" {
				b, _ := json.Marshal(res.Structured)
				text = string(b)
			}
		}
		out["content"] = []map[string]any{{"type": "text", "text": text}}
		return out, nil
	}
	return nil, &rpcError{Code: CodeInvalidParams, Message: "Unknown tool: " + p.Name}
}

func (s *Server) resourceList() []map[string]any {
	list := make([]map[string]any, 0, len(s.Resources))
	for _, r := range s.Resources {
		list = append(list, map[string]any{"uri": r.URI, "name": r.Name, "description": r.Description, "mimeType": r.MIMEType})
	}
	return list
}

func (s *Server) readResource(params json.RawMessage) (map[string]any, error) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "resources/read: falta uri"}
	}
	for _, r := range s.Resources {
		if r.URI == p.URI {
			text, err := r.Read()
			if err != nil {
				return nil, &rpcError{Code: CodeInternal, Message: err.Error()}
			}
			return map[string]any{"contents": []map[string]any{{"uri": r.URI, "mimeType": r.MIMEType, "text": text}}}, nil
		}
	}
	return nil, &rpcError{Code: -32002, Message: "Resource not found", Data: map[string]any{"uri": p.URI}}
}

// SortedNames devuelve los nombres de las herramientas, para pruebas y documentación.
func (s *Server) SortedNames() []string {
	var n []string
	for _, t := range s.Tools {
		n = append(n, t.Name)
	}
	sort.Strings(n)
	return n
}
