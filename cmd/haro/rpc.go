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
	a := core.AnalyzeConfig(sources(req.Msg.Text, req.Msg.Files))
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
	_, changes, err := core.Apply(core.NewConfig(srcs), ops...)
	if err != nil {
		var ae *core.ApplyError
		if errors.As(err, &ae) {
			return nil, connect.NewError(connect.CodeInvalidArgument, ae)
		}
		return nil, internal(err)
	}
	out := &harov1.ApplyResponse{Changes: []*harov1.FileChange{}}
	for _, ch := range changes {
		srcs[ch.File].Text = ch.Text
		out.Changes = append(out.Changes, &harov1.FileChange{File: int32(ch.File), Path: ch.Path, Text: ch.Text})
	}
	out.Files = sourceFiles(srcs)
	return connect.NewResponse(out), nil
}

func (c coreService) Diff(_ context.Context, req *connect.Request[harov1.DiffRequest]) (*connect.Response[harov1.DiffResponse], error) {
	out := &harov1.DiffResponse{}
	for _, op := range core.DiffLines(core.Normalize(req.Msg.Before), core.Normalize(req.Msg.After)) {
		out.Ops = append(out.Ops, &harov1.DiffOp{Op: op.Op, Text: op.Text})
	}
	return connect.NewResponse(out), nil
}

func (c coreService) GetKnowledge(context.Context, *connect.Request[harov1.GetKnowledgeRequest]) (*connect.Response[harov1.Knowledge], error) {
	out, err := toProto(core.KB.Public(), &harov1.Knowledge{})
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
