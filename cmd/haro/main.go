// Command haro: reads, explains and validates HAProxy configurations.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `haro — HAProxy readout

Usage:
  haro                          Terminal UI (in an interactive terminal)
  haro tui     [paths...]       Terminal UI to browse the configuration
  haro lint    [paths...]       Detect problems (exit code 1 if there are errors)
  haro explain [paths...]       Explain every section and directive in plain language
  haro diff    <before> <after> Compare two configurations
  haro template list|show|add   Full templates by technology (Nextcloud, PostgreSQL…)
  haro serve   [options]        Open the local web interface
  haro assist  "request" [paths...]  Ask or request changes from the assistant (sends the configuration to Anthropic)
  haro mcp     [options]        MCP server over stdio (for Claude Code, Claude Desktop…)
  haro version                  Print the version

Paths can be files or directories and are loaded like haproxy -f:
in order, and from each directory its non-hidden .cfg files in lexicographic order.
With no paths, /etc/haproxy/haproxy.cfg is read; use "-" for stdin.
Run "haro <command> -h" to see the options of each command.
`

func main() {
	if len(os.Args) < 2 {
		// No arguments: the terminal UI if there is a terminal; otherwise (a
		// pipe, a script) the usual help.
		if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
			if err := runTUI(nil); err != nil {
				fmt.Fprintln(os.Stderr, "haro:", err)
				os.Exit(1)
			}
			return
		}
		fmt.Print(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "lint":
		err = runLint(args)
	case "explain":
		err = runExplain(args)
	case "diff":
		err = runDiff(args)
	case "tui":
		err = runTUI(args)
	case "template":
		err = runTemplate(args)
	case "assist":
		err = runAssistCmd(args)
	case "mcp":
		err = runMCP(args)
	case "serve":
		err = runServe(args)
	case "version", "--version", "-v":
		fmt.Println("haro", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "haro: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if e, ok := err.(exitError); ok {
			os.Exit(int(e))
		}
		fmt.Fprintln(os.Stderr, "haro:", err)
		os.Exit(1)
	}
}

// exitError lets the program exit with a specific code without printing anything else.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }
