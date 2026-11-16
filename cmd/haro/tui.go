package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/pkarc/haro/core"
	"github.com/pkarc/haro/internal/tui"
)

// isTerminal reports whether f is an interactive terminal (not a pipe or a file).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	noColor := fs.Bool("no-color", false, "no colors (also with NO_COLOR)")
	noWatch := fs.Bool("no-watch", false, "do not reload when the files change")
	langArg := langFlag(fs)
	versionArg := versionFlag(fs)
	editor := fs.String("editor", "", "editor for the e key (default $VISUAL or $EDITOR)")
	assistant := fs.Bool("assistant", false, "enable the AI assistant (key a): SENDS the configuration to the Anthropic API (key in ANTHROPIC_API_KEY or `ant auth login`)")
	assistModel := fs.String("assistant-model", envOr("HARO_ASSISTANT_MODEL", defaultAssistantModel), "Anthropic model for the assistant")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: haro tui [options] [file|directory]...")
		fmt.Fprintln(fs.Output(), "Terminal UI to browse the configuration. With no paths, opens /etc/haproxy/haproxy.cfg.")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	lang, err := resolveLang(*langArg)
	if err != nil {
		return err
	}
	if err := checkVersion(*versionArg); err != nil {
		return err
	}
	paths := fs.Args()
	for _, p := range paths {
		if p == "-" {
			return errors.New("the terminal UI cannot read from stdin; use haro lint or haro explain")
		}
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return errors.New("the terminal UI needs an interactive terminal; use haro lint or haro explain in pipes")
	}
	load := func() ([]core.Source, error) { return readSources(paths) }
	if _, err := load(); err != nil { // fail before opening the screen if there is nothing to read
		return fmt.Errorf("could not read the configuration: %w\nGive the path: haro tui file.cfg", err)
	}
	opts := tui.Options{Load: load, Dirs: paths, Editor: *editor, Lang: lang, HAProxyVersion: *versionArg, NoColor: *noColor, NoWatch: *noWatch}
	if *assistant {
		fmt.Fprintln(os.Stderr, "haro: assistant enabled: the configuration is sent to the Anthropic API (model "+*assistModel+")")
		opts.Assist, opts.Save = tuiAssist(newAnthropicStreamer(*assistModel), paths, load), tuiSave(paths)
		opts.AssistModel = *assistModel
	}
	return tui.Run(opts)
}

// tuiAssist connects the TUI assistant to runAssist. It reads the configuration
// on every request, so the model always sees the current version.
func tuiAssist(st streamer, paths []string, load func() ([]core.Source, error)) func(context.Context, []core.AssistMessage, core.Lang, func(string)) (tui.AssistOutcome, error) {
	return func(ctx context.Context, msgs []core.AssistMessage, lang core.Lang, onText func(string)) (tui.AssistOutcome, error) {
		srcs, err := load()
		if err != nil {
			return tui.AssistOutcome{}, err
		}
		out, err := runAssist(ctx, st, srcs, msgs, lang, func(t string) error { onText(t); return ctx.Err() })
		return tui.AssistOutcome{
			Text: out.Text, Proposal: out.Proposal, IssuesAfter: out.IssuesAfter,
			StopReason: out.Result.StopReason, RefusalCategory: out.Result.RefusalCategory,
		}, err
	}
}

// tuiSave saves a proposal like `haro serve --allow-write`: validates, atomic write and .bak.
func tuiSave(paths []string) func([]core.FileChange) error {
	return func(changes []core.FileChange) error {
		_, err := saveProposal(context.Background(), paths, changes)
		return err
	}
}
