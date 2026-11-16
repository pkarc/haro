package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/pkarc/haro/core"
)

// AI assistant. Context building and proposal extraction live in core
// (core/assist.go); only the call to the Anthropic API lives here, which is
// the only thing that leaves the machine.
//
// It sends the configuration (with its passwords and addresses) to the
// Anthropic API. That is why it is opt-in: `haro serve --assistant` or the
// `haro assist` command. The key (ANTHROPIC_API_KEY or an `ant auth login`
// profile) is read by the SDK on the local server; it never reaches the browser.

// defaultAssistantModel is the assistant model unless another is given.
const defaultAssistantModel = "claude-opus-5-5"

// assistResult summarizes how a response ended.
type assistResult struct {
	StopReason      string
	RefusalCategory string
	InputTokens     int64
	OutputTokens    int64
}

// streamer sends a conversation to the model and delivers the response in
// chunks. The real implementation uses the Anthropic SDK; tests use a fake.
type streamer interface {
	Stream(ctx context.Context, system string, msgs []core.AssistMessage, onText func(string) error) (assistResult, error)
}

type anthropicStreamer struct {
	client anthropic.Client
	model  string
}

func newAnthropicStreamer(model string, opts ...option.RequestOption) *anthropicStreamer {
	if model == "" {
		model = defaultAssistantModel
	}
	return &anthropicStreamer{client: anthropic.NewClient(opts...), model: model}
}

func (a *anthropicStreamer) Stream(ctx context.Context, system string, msgs []core.AssistMessage, onText func(string) error) (assistResult, error) {
	params := make([]anthropic.MessageParam, len(msgs))
	for i, m := range msgs {
		block := anthropic.NewTextBlock(m.Content)
		if m.Role == "assistant" {
			params[i] = anthropic.NewAssistantMessage(block)
		} else {
			params[i] = anthropic.NewUserMessage(block)
		}
	}
	// Adaptive thinking; the system prompt is stable and is cached.
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	stream := a.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 32000,
		System:    []anthropic.TextBlockParam{{Text: system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages:  params,
	})
	defer stream.Close()
	var res assistResult
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "message_start":
			res.InputTokens = ev.Message.Usage.InputTokens
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				if err := onText(ev.Delta.Text); err != nil {
					return res, err
				}
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				res.StopReason = string(ev.Delta.StopReason)
				res.RefusalCategory = string(ev.Delta.StopDetails.Category)
			}
			res.OutputTokens = ev.Usage.OutputTokens
		}
	}
	if err := stream.Err(); err != nil {
		return res, assistError(err)
	}
	return res, nil
}

// assistError translates SDK errors into useful messages for the user.
func assistError(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401:
			return errors.New("the Anthropic API rejected the credentials: set ANTHROPIC_API_KEY or run `ant auth login`")
		case 403:
			return errors.New("the Anthropic API denied access (permissions, or model not available for your account)")
		case 404:
			return errors.New("model not found in the Anthropic API: check --assistant-model")
		case 429:
			return errors.New("Anthropic API rate limit reached: wait a moment and retry")
		}
		return fmt.Errorf("Anthropic API error (%d): %s", apierr.StatusCode, apierr.Error())
	}
	if strings.Contains(err.Error(), "could not resolve authentication") || strings.Contains(err.Error(), "apiKey") {
		return errors.New("missing Anthropic credentials: set ANTHROPIC_API_KEY or run `ant auth login`")
	}
	return err
}

// Conversation limits: they protect the cost and size of the request.
const (
	maxAssistMessages = 40
	maxAssistHistory  = 200 << 10
)

// validateConversation checks that the conversation is valid (alternating
// roles, ending with a user turn) and not oversized.
func validateConversation(msgs []core.AssistMessage) error {
	if len(msgs) == 0 {
		return errors.New("missing user request")
	}
	if len(msgs) > maxAssistMessages {
		return fmt.Errorf("the conversation has more than %d messages: start a new one", maxAssistMessages)
	}
	total := 0
	for i, m := range msgs {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		if m.Role != want {
			return fmt.Errorf("message %d must have role %q", i+1, want)
		}
		if strings.TrimSpace(m.Content) == "" {
			return fmt.Errorf("message %d is empty", i+1)
		}
		total += len(m.Content)
	}
	if msgs[len(msgs)-1].Role != "user" {
		return errors.New("the conversation must end with a user message")
	}
	if total > maxAssistHistory {
		return fmt.Errorf("the conversation is larger than %d KB: start a new one", maxAssistHistory>>10)
	}
	return nil
}

// assistOutcome is the complete result of an assistant turn.
type assistOutcome struct {
	Text     string
	Proposal core.Proposal
	// IssuesAfter are the configuration problems with the proposal applied.
	IssuesAfter []core.Issue
	Result      assistResult
}

// runAssist runs a turn: builds the context with the current configuration,
// sends the conversation, delivers the text as it arrives and finally extracts
// the proposal and analyzes how the configuration would look.
func runAssist(ctx context.Context, st streamer, srcs []core.Source, msgs []core.AssistMessage, lang core.Lang, onText func(string) error) (assistOutcome, error) {
	var out assistOutcome
	if err := validateConversation(msgs); err != nil {
		return out, err
	}
	a := core.AnalyzeConfigLang(srcs, lang)
	cx, err := core.AssistContext(srcs, a, lang)
	if err != nil {
		return out, err
	}
	send := append([]core.AssistMessage{}, msgs...)
	last := &send[len(send)-1]
	last.Content = cx + "\n\n" + last.Content

	var text strings.Builder
	res, err := st.Stream(ctx, core.AssistSystemPrompt(lang), send, func(t string) error {
		text.WriteString(t)
		return onText(t)
	})
	out.Text, out.Result = text.String(), res
	if err != nil {
		return out, err
	}
	out.Proposal = core.ExtractProposal(out.Text, srcs)
	if len(out.Proposal.Changes) > 0 {
		after := append([]core.Source{}, srcs...)
		for _, ch := range out.Proposal.Changes {
			after[ch.File].Text = ch.Text
		}
		out.IssuesAfter = core.LintLang(core.NewConfig(after).Merge(), lang)
	}
	return out, nil
}

// ---------- haro assist ----------

func runAssistCmd(args []string) error {
	fs := flag.NewFlagSet("assist", flag.ExitOnError)
	langFlag := fs.String("lang", "", "language: en or es (default: from the environment)")
	model := fs.String("model", envOr("HARO_ASSISTANT_MODEL", defaultAssistantModel), "Anthropic model")
	write := fs.Bool("write", false, "write the proposal (validates the full configuration and leaves a .bak copy)")
	yes := fs.Bool("yes", false, "with --write, do not ask for confirmation")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: haro assist [options] \"request\" [file|directory]...")
		fmt.Fprintln(fs.Output(), "Ask or request changes from the assistant. SENDS THE CONFIGURATION to the Anthropic API.")
		fmt.Fprintln(fs.Output(), "Needs ANTHROPIC_API_KEY (or `ant auth login`). Without --write it only shows the proposed diff.")
		fs.PrintDefaults()
	}
	pos, err := parseArgs(fs, args)
	if errors.Is(err, errHelpShown) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		fs.Usage()
		return exitError(2)
	}
	request, paths := pos[0], pos[1:]
	lang, err := resolveLang(*langFlag)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if p == "-" {
			return errors.New("haro assist does not read from stdin; give files or directories")
		}
	}
	srcs, err := readSources(paths)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "haro: the configuration is sent to the Anthropic API (model "+*model+")")
	st := newAnthropicStreamer(*model)
	out, err := runAssist(context.Background(), st, srcs, []core.AssistMessage{{Role: "user", Content: request}}, lang, func(t string) error {
		_, err := io.WriteString(os.Stdout, t)
		return err
	})
	fmt.Println()
	if err != nil {
		return err
	}
	return finishAssistCLI(out, srcs, paths, *write, *yes, lang)
}

// finishAssistCLI shows the proposal diff and, with --write, saves it.
func finishAssistCLI(out assistOutcome, srcs []core.Source, paths []string, write, yes bool, lang core.Lang) error {
	switch out.Result.StopReason {
	case "refusal":
		return fmt.Errorf(tx(lang, "el modelo declinó la petición (%s)", "the model declined the request (%s)"), out.Result.RefusalCategory)
	case "max_tokens":
		fmt.Fprintln(os.Stderr, tx(lang, "haro: la respuesta se cortó por el límite de tokens", "haro: the response was cut off by the token limit"))
	}
	p := out.Proposal
	if p.Truncated {
		fmt.Fprintln(os.Stderr, tx(lang, "haro: un bloque de configuración quedó cortado y se descartó", "haro: a configuration block was cut off and discarded"))
	}
	if p.Unmatched > 0 {
		fmt.Fprintf(os.Stderr, tx(lang, "haro: %d bloque(s) de configuración no se pudieron asociar a un archivo y se descartaron\n", "haro: %d configuration block(s) could not be matched to a file and were discarded\n"), p.Unmatched)
	}
	if len(p.Changes) == 0 {
		return nil
	}
	fmt.Println(tx(lang, "\n── propuesta ──", "\n── proposal ──"))
	for _, ch := range p.Changes {
		fmt.Print(core.UnifiedDiff(ch.Path, srcs[ch.File].Text, ch.Text, 3))
	}
	errs := 0
	for _, i := range out.IssuesAfter {
		if i.Severity == core.SevError {
			errs++
		}
	}
	fmt.Printf(tx(lang, "\nTras aplicarla: %s.\n", "\nAfter applying it: %s.\n"), plural(errs, "error", tx(lang, "errores", "errors")))
	if !write {
		fmt.Println(tx(lang, "Para escribirla: repite con --write (o aplícala desde la interfaz).", "To write it: repeat with --write (or apply it from the interface)."))
		return nil
	}
	if !yes {
		fmt.Print(tx(lang, "¿Escribir estos cambios? [s/N] ", "Write these changes? [y/N] "))
		var ans string
		fmt.Scanln(&ans)
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "s" && a != "y" && a != "si" && a != "sí" && a != "yes" {
			fmt.Println(tx(lang, "No se escribió nada.", "Nothing was written."))
			return nil
		}
	}
	res, err := saveProposal(context.Background(), paths, p.Changes)
	if err != nil {
		return err
	}
	for _, sv := range res.Saved {
		fmt.Printf(tx(lang, "Guardado %s (copia en %s)\n", "Saved %s (backup at %s)\n"), sv.Path, sv.Backup)
	}
	return nil
}

// saveProposal saves the files of a proposal with the same guarantees as
// `haro serve --allow-write`: validates the full configuration (haproxy -c or
// the detector), writes atomically and leaves a .bak copy. If validation
// rejects it, the error explains why and nothing is written.
func saveProposal(ctx context.Context, paths []string, changes []core.FileChange) (putOutcome, error) {
	files := make([]core.Source, len(changes))
	for i, ch := range changes {
		files[i] = core.Source{Path: ch.Path, Text: ch.Text}
	}
	s := &server{paths: paths, allowWrite: true}
	if bin, err := exec.LookPath("haproxy"); err == nil {
		s.haproxyBin = bin
	}
	res, err := s.put(ctx, files)
	if err != nil {
		return res, err
	}
	if res.Err != "" {
		msg := res.Err
		if res.Check != nil && res.Check.Output != "" {
			msg += "\n" + res.Check.Output
		}
		if res.Issue != nil {
			msg += fmt.Sprintf("\n%s:%d: %s", res.Path, res.Issue.Line, res.Issue.Message)
		}
		return res, errors.New("not saved: " + msg)
	}
	return res, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
