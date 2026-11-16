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

// Implementation of the Connect services of proto/haro/v1/haro.proto. The
// conversion between core structures and contract messages lives here, never
// inside core, so the core stays dependency-free.
//
// Core structures already serialize to JSON with the same (camelCase) names as
// protobuf's JSON mapping, so conversion goes through JSON. It is strict: if
// the core adds a field the contract lacks, it fails, and TestRPCContract
// catches it.

func (s *server) mountRPC(mux *http.ServeMux) {
	opts := []connect.HandlerOption{connect.WithReadMaxBytes(10 << 20)}
	mux.Handle(harov1connect.NewCoreServiceHandler(coreService{s}, opts...))
	mux.Handle(harov1connect.NewConfigServiceHandler(configService{s}, opts...))
	mux.Handle(harov1connect.NewAssistServiceHandler(assistService{s}, opts...))
}

// toProto copies v (a core structure) into message m.
func toProto[M proto.Message](v any, m M) (M, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return m, err
	}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(b, m); err != nil {
		return m, fmt.Errorf("core and proto contract do not match: %w", err)
	}
	return m, nil
}

// fromProto copies message m into v (a core structure).
func fromProto(m proto.Message, v any) error {
	b, err := protojson.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// requestLang interprets the lang field of a request: empty is English and a
// language the core does not know is a client error.
func requestLang(v string) (core.Lang, error) {
	if v == "" {
		return core.LangDefault, nil
	}
	l, ok := core.ParseLang(v)
	if !ok {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown language %q (use es or en)", v))
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

// sources gathers the files of a request: files if present, or text as a
// single haproxy.cfg.
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

// Apply applies edit operations without touching the disk: saving is
// ConfigService.PutConfig, with all its validations. Unchanged files
// come back with their original text, byte for byte.
func (c coreService) Apply(_ context.Context, req *connect.Request[harov1.ApplyRequest]) (*connect.Response[harov1.ApplyResponse], error) {
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return nil, err
	}
	srcs := sources(req.Msg.Text, req.Msg.Files)
	ops := make([]core.Op, len(req.Msg.Ops))
	for i, op := range req.Msg.Ops {
		if err := fromProto(op, &ops[i]); err != nil {
			return nil, internal(err)
		}
	}
	res, err := coreapi.Apply(srcs, ops, lang)
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

// ListTemplates lists the complete per-technology templates (Nextcloud,
// PostgreSQL...) with their parameters, in the requested language.
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

// RenderTemplate generates the operations of a per-technology template and
// applies them without touching the disk, so the interface shows the diff
// before accepting. Saving is still ConfigService.PutConfig (or Apply with the ops).
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
		CanAssist: s.assist != nil, AssistantModel: s.assistModel,
	}), nil
}

func (c configService) GetConfig(context.Context, *connect.Request[harov1.GetConfigRequest]) (*connect.Response[harov1.ConfigFile], error) {
	if len(c.s.paths) == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("haro serve was started without --file"))
	}
	srcs, err := readSources(c.s.paths)
	if err != nil {
		return nil, internal(err)
	}
	return connect.NewResponse(&harov1.ConfigFile{Files: sourceFiles(srcs)}), nil
}

// PutConfig saves one or more files (see server.put). A save rejected
// by validation answers with saved=false and the reason, not with an
// RPC error.
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
