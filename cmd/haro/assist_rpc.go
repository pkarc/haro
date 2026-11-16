package main

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	"github.com/pkarc/haro/core"
	harov1 "github.com/pkarc/haro/gen/haro/v1"
	"github.com/pkarc/haro/gen/haro/v1/harov1connect"
)

// assistService implementa AssistService. Sin --assistant (server.assist == nil)
// responde UNIMPLEMENTED y la configuración nunca sale de la máquina.
type assistService struct{ s *server }

var _ harov1connect.AssistServiceHandler = assistService{}

func (a assistService) Assist(ctx context.Context, req *connect.Request[harov1.AssistRequest], stream *connect.ServerStream[harov1.AssistEvent]) error {
	if a.s.assist == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("el asistente está desactivado: inicia haro serve con --assistant"))
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
		// El texto ya enviado queda en el cliente; el error cierra el stream.
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
