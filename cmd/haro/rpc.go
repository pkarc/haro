package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/pkarc/haro/core"
	harov1 "github.com/pkarc/haro/gen/haro/v1"
	"github.com/pkarc/haro/gen/haro/v1/harov1connect"
	"github.com/pkarc/haro/internal/coreapi"
)

// Implementación de los servicios Connect de proto/haro/v1/haro.proto. La
// conversión entre las estructuras del núcleo y los mensajes del contrato vive
// aquí, nunca dentro de core, para que el núcleo siga sin dependencias.
//
// Las estructuras del núcleo ya se serializan a JSON con los mismos nombres
// (camelCase) que el mapeo JSON de protobuf, así que se convierte pasando por
// JSON. La conversión es estricta: si el núcleo añade un campo que el contrato
// no tiene, falla, y lo detecta TestRPCContract.

func (s *server) mountRPC(mux *http.ServeMux) {
	opts := []connect.HandlerOption{connect.WithReadMaxBytes(10 << 20)}
	mux.Handle(harov1connect.NewCoreServiceHandler(coreService{s}, opts...))
	mux.Handle(harov1connect.NewConfigServiceHandler(configService{s}, opts...))
}

// toProto copia v (estructura del núcleo) en el mensaje m.
func toProto[M proto.Message](v any, m M) (M, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return m, err
	}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(b, m); err != nil {
		return m, fmt.Errorf("el núcleo y el contrato proto no coinciden: %w", err)
	}
	return m, nil
}

// fromProto copia el mensaje m en v (estructura del núcleo).
func fromProto(m proto.Message, v any) error {
	b, err := protojson.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// requestLang interpreta el campo lang de una petición: vacío es español y un
// idioma que el núcleo no conoce es un error del cliente.
func requestLang(v string) (core.Lang, error) {
	if v == "" {
		return core.LangES, nil
	}
	l, ok := core.ParseLang(v)
	if !ok {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("idioma desconocido %q (usa es o en)", v))
	}
	return l, nil
}

func changesToProto(changes []core.FileChange) []*harov1.FileChange {
	out := make([]*harov1.FileChange, 0, len(changes))
	for _, ch := range changes {
		out = append(out, &harov1.FileChange{File: int32(ch.File), Path: ch.Path, Text: ch.Text})
	}
	return out
}

func internal(err error) error { return connect.NewError(connect.CodeInternal, err) }

// sources reúne los archivos de una petición: files si llega, o text como un
// único haproxy.cfg.
func sources(text string, files []*harov1.SourceFile) []core.Source {
	if len(files) > 0 {
		out := make([]core.Source, len(files))
		for i, f := range files {
			out[i] = core.Source{Path: f.Path, Text: f.Text}
		}
		return out
	}
	return []core.Source{{Path: "haproxy.cfg", Text: text}}
}

func sourceFiles(srcs []core.Source) []*harov1.SourceFile {
	out := make([]*harov1.SourceFile, len(srcs))
	for i, s := range srcs {
		out[i] = &harov1.SourceFile{Path: s.Path, Text: s.Text}
	}
	return out
}

func checkToProto(c checkResult) *harov1.CheckResult {
	return &harov1.CheckResult{Available: c.Available, Ok: c.OK, Output: c.Output}
}

type coreService struct{ s *server }

func (c coreService) Analyze(_ context.Context, req *connect.Request[harov1.AnalyzeRequest]) (*connect.Response[harov1.Analysis], error) {
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return nil, err
	}
	if err := checkVersion(req.Msg.HaproxyVersion); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	a := core.AnalyzeConfigWith(sources(req.Msg.Text, req.Msg.Files), core.LintOptions{Lang: lang, HAProxyVersion: req.Msg.HaproxyVersion})
	out, err := toProto(a, &harov1.Analysis{})
	if err != nil {
		return nil, internal(err)
	}
	return connect.NewResponse(out), nil
}

// Apply aplica operaciones de edición sin tocar el disco: guardar es
// ConfigService.PutConfig, con todas sus validaciones. Los archivos sin
// cambios vuelven con su texto original, byte a byte.
func (c coreService) Apply(_ context.Context, req *connect.Request[harov1.ApplyRequest]) (*connect.Response[harov1.ApplyResponse], error) {
	srcs := sources(req.Msg.Text, req.Msg.Files)
	ops := make([]core.Op, len(req.Msg.Ops))
	for i, op := range req.Msg.Ops {
		if err := fromProto(op, &ops[i]); err != nil {
			return nil, internal(err)
		}
	}
	res, err := coreapi.Apply(srcs, ops)
	if err != nil {
		var ae *core.ApplyError
		if errors.As(err, &ae) {
			return nil, connect.NewError(connect.CodeInvalidArgument, ae)
		}
		return nil, internal(err)
	}
	out := &harov1.ApplyResponse{Files: sourceFiles(res.Files), Changes: changesToProto(res.Changes)}
	return connect.NewResponse(out), nil
}

// ListTemplates lista las plantillas completas por tecnología (Nextcloud,
// PostgreSQL…) con sus parámetros, en el idioma pedido.
func (c coreService) ListTemplates(_ context.Context, req *connect.Request[harov1.ListTemplatesRequest]) (*connect.Response[harov1.ListTemplatesResponse], error) {
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return nil, err
	}
	out := &harov1.ListTemplatesResponse{}
	for _, t := range core.TechTemplates(lang) {
		m, err := toProto(t, &harov1.TechTemplate{})
		if err != nil {
			return nil, internal(err)
		}
		out.Templates = append(out.Templates, m)
	}
	return connect.NewResponse(out), nil
}

// RenderTemplate genera las operaciones de una plantilla por tecnología y las
// aplica sin tocar el disco, para que la interfaz muestre el diff antes de
// aceptar. Guardar sigue siendo ConfigService.PutConfig (o Apply con las ops).
func (c coreService) RenderTemplate(_ context.Context, req *connect.Request[harov1.RenderTemplateRequest]) (*connect.Response[harov1.RenderTemplateResponse], error) {
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return nil, err
	}
	srcs := sources(req.Msg.Text, req.Msg.Files)
	params := make(map[string]any, len(req.Msg.Params))
	for k, v := range req.Msg.Params {
		params[k] = v.AsInterface()
	}
	res, err := coreapi.RenderTemplate(srcs, req.Msg.Id, params, lang)
	if err != nil {
		var te *core.TemplateError
		if errors.As(err, &te) {
			return nil, connect.NewError(connect.CodeInvalidArgument, te)
		}
		return nil, internal(err)
	}
	out := &harov1.RenderTemplateResponse{Ops: make([]*harov1.Op, len(res.Ops)), Files: sourceFiles(res.Files), Changes: changesToProto(res.Changes)}
	for i, op := range res.Ops {
		if out.Ops[i], err = toProto(op, &harov1.Op{}); err != nil {
			return nil, internal(err)
		}
	}
	return connect.NewResponse(out), nil
}

func (c coreService) Diff(_ context.Context, req *connect.Request[harov1.DiffRequest]) (*connect.Response[harov1.DiffResponse], error) {
	out := &harov1.DiffResponse{}
	for _, op := range core.DiffLines(core.Normalize(req.Msg.Before), core.Normalize(req.Msg.After)) {
		out.Ops = append(out.Ops, &harov1.DiffOp{Op: op.Op, Text: op.Text})
	}
	return connect.NewResponse(out), nil
}

func (c coreService) GetKnowledge(_ context.Context, req *connect.Request[harov1.GetKnowledgeRequest]) (*connect.Response[harov1.Knowledge], error) {
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return nil, err
	}
	out, err := toProto(core.KB.PublicFor(lang), &harov1.Knowledge{})
	if err != nil {
		return nil, internal(err)
	}
	return connect.NewResponse(out), nil
}

func (c coreService) Check(ctx context.Context, req *connect.Request[harov1.CheckRequest]) (*connect.Response[harov1.CheckResult], error) {
	res, err := c.s.check(ctx, sources(req.Msg.Text, req.Msg.Files))
	if err != nil {
		return nil, internal(err)
	}
	return connect.NewResponse(checkToProto(res)), nil
}

type configService struct{ s *server }

func (c configService) GetCapabilities(context.Context, *connect.Request[harov1.GetCapabilitiesRequest]) (*connect.Response[harov1.Capabilities], error) {
	s := c.s
	return connect.NewResponse(&harov1.Capabilities{
		Version: version, Paths: s.paths, CanRead: len(s.paths) > 0, CanWrite: s.allowWrite,
		CanCheck: s.haproxyBin != "", HaproxyBin: s.haproxyBin,
	}), nil
}

func (c configService) GetConfig(context.Context, *connect.Request[harov1.GetConfigRequest]) (*connect.Response[harov1.ConfigFile], error) {
	if len(c.s.paths) == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("haro serve se inició sin --file"))
	}
	srcs, err := readSources(c.s.paths)
	if err != nil {
		return nil, internal(err)
	}
	return connect.NewResponse(&harov1.ConfigFile{Files: sourceFiles(srcs)}), nil
}

// PutConfig guarda uno o varios archivos (ver server.put). Un guardado
// rechazado por la validación responde con saved=false y el motivo, no con un
// error de RPC.
func (c configService) PutConfig(ctx context.Context, req *connect.Request[harov1.PutConfigRequest]) (*connect.Response[harov1.PutConfigResponse], error) {
	changes := []core.Source{{Path: req.Msg.Path, Text: req.Msg.Text}}
	if len(req.Msg.Files) > 0 {
		changes = sources("", req.Msg.Files)
	}
	res, err := c.s.put(ctx, changes)
	switch {
	case errors.Is(err, errDenied):
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	case err != nil:
		var bad errClient
		if errors.As(err, &bad) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, internal(err)
	}
	out := &harov1.PutConfigResponse{Saved: res.Err == "", Error: res.Err, Path: res.Path}
	if res.Check != nil {
		out.Check = checkToProto(*res.Check)
	}
	if res.Issue != nil {
		iss, err := toProto(*res.Issue, &harov1.Issue{})
		if err != nil {
			return nil, internal(err)
		}
		out.Issue = iss
	}
	for _, sv := range res.Saved {
		out.Files = append(out.Files, &harov1.SavedFile{Path: sv.Path, Backup: sv.Backup})
	}
	if len(res.Saved) == 1 {
		out.Path, out.Backup = res.Saved[0].Path, res.Saved[0].Backup
	}
	return connect.NewResponse(out), nil
}
