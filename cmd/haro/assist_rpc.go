package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/pkarc/haro/core"
	harov1 "github.com/pkarc/haro/gen/haro/v1"
	"github.com/pkarc/haro/gen/haro/v1/harov1connect"
)

// assistService implements AssistService. Without --assistant (server.assist == nil)
// it answers UNIMPLEMENTED and the configuration never leaves the machine.
type assistService struct{ s *server }

var _ harov1connect.AssistServiceHandler = assistService{}

func (a assistService) Assist(ctx context.Context, req *connect.Request[harov1.AssistRequest], stream *connect.ServerStream[harov1.AssistEvent]) error {
	if a.s.assist == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("the assistant is disabled: start haro serve with --assistant"))
	}
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return err
	}
	srcs := sources("", req.Msg.Files)
	msgs := make([]core.AssistMessage, len(req.Msg.Messages))
	for i, m := range req.Msg.Messages {
		msgs[i] = core.AssistMessage{Role: m.Role, Content: m.Content}
	}
	if err := validateConversation(msgs); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	out, err := runAssist(ctx, a.s.assist, srcs, msgs, lang, func(t string) error {
		return stream.Send(&harov1.AssistEvent{Event: &harov1.AssistEvent_Text{Text: t}})
	})
	if err != nil {
		// The text already sent stays on the client; the error closes the stream.
		code := connect.CodeUnavailable
		if ctx.Err() != nil {
			code = connect.CodeCanceled
		}
		return connect.NewError(code, err)
	}
	if len(out.Proposal.Changes) > 0 || out.Proposal.Truncated || out.Proposal.Unmatched > 0 {
		prop := &harov1.Proposal{Changes: changesToProto(out.Proposal.Changes), Unmatched: int32(out.Proposal.Unmatched), Truncated: out.Proposal.Truncated}
		for _, i := range out.IssuesAfter {
			m, err := toProto(i, &harov1.Issue{})
			if err != nil {
				return internal(err)
			}
			prop.IssuesAfter = append(prop.IssuesAfter, m)
		}
		if err := stream.Send(&harov1.AssistEvent{Event: &harov1.AssistEvent_Proposal{Proposal: prop}}); err != nil {
			return err
		}
	}
	return stream.Send(&harov1.AssistEvent{Event: &harov1.AssistEvent_Done{Done: &harov1.AssistDone{
		StopReason: out.Result.StopReason, RefusalCategory: out.Result.RefusalCategory,
		InputTokens: int32(out.Result.InputTokens), OutputTokens: int32(out.Result.OutputTokens),
	}}})
}

// proposeMaxTokens bounds the answer of ProposeDirectives (a few lines of JSON).
const proposeMaxTokens = 2000

// proposeTimeout bounds the whole model call.
const proposeTimeout = 60 * time.Second

func (a assistService) ProposeDirectives(ctx context.Context, req *connect.Request[harov1.ProposeDirectivesRequest]) (*connect.Response[harov1.ProposeDirectivesResponse], error) {
	if a.s.assist == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("the assistant is disabled: start haro serve with --assistant"))
	}
	lang, err := requestLang(req.Msg.Lang)
	if err != nil {
		return nil, err
	}
	instruction := strings.TrimSpace(req.Msg.Instruction)
	if instruction == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("missing instruction"))
	}
	if len(instruction) > core.MaxProposeInstruction {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("the instruction is larger than %d bytes", core.MaxProposeInstruction))
	}
	srcs := sources("", req.Msg.Files)
	section := int(req.Msg.Section)
	cx, err := core.SectionContext(srcs, section, lang)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	ctx, cancel := context.WithTimeout(ctx, proposeTimeout)
	defer cancel()
	raw, res, err := complete(ctx, a.s.assist, core.ProposeSystemPrompt(lang), cx+"\n\n"+instruction, proposeMaxTokens)
	if err != nil {
		code := connect.CodeUnavailable
		if ctx.Err() != nil {
			code = connect.CodeDeadlineExceeded
			if errors.Is(ctx.Err(), context.Canceled) {
				code = connect.CodeCanceled
			}
		}
		return nil, connect.NewError(code, err)
	}
	dirs, note, err := core.BuildProposal(srcs, section, raw, lang)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	out := &harov1.ProposeDirectivesResponse{Note: note, InputTokens: int32(res.InputTokens), OutputTokens: int32(res.OutputTokens)}
	for _, d := range dirs {
		m, err := toProto(d, &harov1.ProposedDirective{})
		if err != nil {
			return nil, internal(err)
		}
		out.Directives = append(out.Directives, m)
	}
	return connect.NewResponse(out), nil
}
