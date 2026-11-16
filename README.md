English | [Español](README.es.md)

# haro

**haro** (*HAProxy readout*) reads, explains and validates HAProxy configurations.

```
$ haro explain --section api_servers
backend api_servers  Server group
  mode http (inherited from defaults)

    49 balance leastconn
       [Servers] Load balancing: to the server with the fewest active connections (ideal for long sessions)
    50 server api1 10.0.0.21:9000 check inter 2s fall 3 rise 2
       [Servers] Server api1 at 10.0.0.21:9000 · active health checks · check every 2 s · …
```

> haro is an independent project. It is not affiliated with or endorsed by HAProxy Technologies. HAProxy® is a registered trademark of HAProxy Technologies.

## What it does

- **`haro explain`**: explains every section and directive in plain language, including `defaults` inheritance.
- **`haro lint`**: detects problems (nonexistent backends, undefined ACLs, missing timeouts, incompatible http/tcp modes…). Exits with code 1 if there are errors, so it works in CI.
- **`haro diff`**: compares two configurations.
- **`haro` / `haro tui`**: terminal interface: sections with their explanations, navigable problems (`p`), traffic map (`m`), editing in your `$EDITOR` at the exact line (`e`) and automatic reload when the files change. Without an interactive terminal (a pipe), `haro` shows the help.
- **`haro mcp`**: MCP server over stdio for Claude Code, Claude Desktop and other clients (see below).
- **`haro serve`**: opens a local web interface with code mode, visual mode and a traffic map.

## Installation

```sh
go install github.com/pkarc/haro/cmd/haro@latest
```

`go install` builds without the web UI (the `haro serve` interface is reduced to a placeholder page, because `web/dist` is not versioned). Release binaries, Homebrew and the Docker image do include it. To build with the web UI from source: `make web build`.

## Usage

```sh
haro                                    # terminal interface (in a terminal)
haro tui haproxy.cfg conf.d/            # with paths, like haproxy -f
haro lint                               # /etc/haproxy/haproxy.cfg by default
haro lint haproxy.cfg conf.d/           # several files, like haproxy -f
haro lint --format json my.cfg          # output for CI (also sarif and markdown)
haro lint --disable no-global,tls-no-alpn my.cfg
haro lint --haproxy-version 2.8 my.cfg   # warns about directives that require a later version (newer-than-target)
haro lint --rules                       # list of rules
haro explain my.cfg
haro explain --lang es my.cfg           # explanations in Spanish (also in lint, tui and mcp)
cat my.cfg | haro explain -
haro diff before.cfg after.cfg
haro template list                      # templates per technology (Nextcloud, PostgreSQL…)
haro template show nextcloud            # its parameters
haro template add nextcloud --param domain=cloud.example.com --param servers=10.0.0.10:80 \
    --param cert=/etc/haproxy/certs/cloud.pem haproxy.cfg   # shows the diff; --write saves
haro serve --file /etc/haproxy/haproxy.cfg               # read-only
haro serve --file /etc/haproxy/haproxy.cfg --allow-write # allows saving
haro serve --file haproxy.cfg --file conf.d/             # several files
haro serve --file haproxy.cfg --assistant                # with the AI assistant (see below)
haro tui --assistant haproxy.cfg                         # TUI with the assistant (key a)
haro assist "Review my configuration" haproxy.cfg        # a single request from the command line
```

### Serverless: the core in WebAssembly

The same core compiles to WebAssembly (`make wasm` → `web/public/wasm/`) and the web UI can use it instead of `haro serve`: `cd web && VITE_CORE=wasm npm run build`. The configuration never leaves the browser. There is no disk access or `haproxy -c` in this mode (only the detector). The module built with standard Go weighs about 6 MB (1.6 MB gzipped); `make wasm-tiny` uses TinyGo in Docker and brings it down to about 1.8 MB (620 KB gzipped). `node wasm/smoke.mjs [dir]` checks either one.

### In CI and in your repository

```yaml
# .github/workflows/haproxy.yml
on: [pull_request]
permissions: { contents: read, security-events: write, pull-requests: write }
jobs:
  haro:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pkarc/haro@v0.1.0
        with:
          paths: haproxy/            # files or directories, like haproxy -f
```

The Action installs haro, runs `haro lint`, uploads the SARIF report to *code scanning* and leaves (and updates) a comment on the pull request. Output formats of `haro lint`: `--format text|json|sarif|markdown`.

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/pkarc/haro
    rev: v0.1.0
    hooks:
      - id: haro-lint
```

```sh
brew install pkarc/tap/haro            # Homebrew (macOS and Linux)
docker run --rm -v /etc/haproxy:/cfg:ro ghcr.io/pkarc/haro lint /cfg
```

Binaries, `.deb` and `.rpm` packages are also available on the [releases page](https://github.com/pkarc/haro/releases). The Docker image is minimal (no `haproxy`): it works for `lint`, `explain` and `mcp`; to validate with `haproxy -c`, use the binary next to HAProxy.

### MCP server

`haro mcp` exposes haro's analysis as MCP tools: `haro_lint`, `haro_explain`, `haro_analyze`, `haro_diff`, `haro_check` (`haproxy -c`), `haro_apply` (structural edits), `haro_template_list` and `haro_template_render` (per-technology templates, dry run only), plus the `haro://knowledge` and `haro://rules` resources. It speaks both protocol versions (the `initialize` handshake of 2025-11-25 and earlier, and the sessionless 2026-07-28 version).

```sh
# Claude Code
claude mcp add haro -- haro mcp --root /etc/haproxy
```

```jsonc
// Claude Desktop: claude_desktop_config.json
{ "mcpServers": { "haro": { "command": "haro", "args": ["mcp", "--root", "/etc/haproxy"] } } }
```

Security: with `--root` (repeatable), only paths inside those directories are read and written (symbolic links are resolved). Without `--root`, the client can read any path the process can read. `haro_apply` only writes with `--allow-write` and `write: true`, with the same guarantees as `haro serve`: it validates the full configuration, saves everything or nothing and leaves a `.bak` copy. [`skills/haro`](skills/haro/SKILL.md) contains a Claude skill with the recommended workflow.

### Per-technology templates

Each template adds the frontends and backends needed to publish a service behind HAProxy, with the settings that technology needs: **Proxmox VE** (TLS to the backend and noVNC WebSocket), **Home Assistant** (WebSocket, `X-Forwarded-*`), **Nextcloud** (HSTS, `/.well-known/carddav|caldav`, timeout for large uploads), **Jellyfin**, **Gitea** (web and, optionally, SSH over TCP), **Grafana**, **Vaultwarden**, **PostgreSQL** (`pgsql-check`), **Redis** (`tcp-check` with PING and, optionally, master only), **Elasticsearch** (several nodes, `http-check` against `/_cluster/health`) and **Kubernetes API** (TCP 6443, several control planes). Web templates include the port 80 `bind` with a redirect to HTTPS and the port 443 one with `ssl crt … alpn h2,http/1.1`.

```sh
haro template show proxmox              # parameters, types and defaults
haro template add nextcloud --param domain=cloud.example.com --param servers=10.0.0.10:80 \
    --param cert=/etc/haproxy/certs/cloud.pem /etc/haproxy/haproxy.cfg
haro template add postgresql --param bind=10.0.0.5:5432 --param servers=10.0.0.10:5432,10.0.0.11:5432 --write haproxy.cfg
```

`add` prints the unified diff and **does not write**; with `--write` it saves with the same guarantees as `haro serve` (validates the full configuration with `haproxy -c`, atomic write and `.bak` copy). List parameters accept several `--param` flags with the same key or comma-separated values. The template reuses the `global` and `defaults` that already exist (and only adds the missing ones), picks collision-free names (`nextcloud_2`…) and, if a TLS frontend already exists on 443, joins it with a per-domain ACL instead of opening another `bind` on the same port. In the web UI, the "Add from technology template" button in visual mode opens a form with the diff before you accept. For Claude, `haro_template_list` and `haro_template_render` (they simulate and return the diff and the ops, which are applied with `haro_apply`). In the API: `CoreService.ListTemplates` and `RenderTemplate` (parameters are a map of `google.protobuf.Value`).

### AI assistant

Chat about your configuration in natural language ("add a backend with three servers", "limit to 20 requests per second per IP", "what would you improve"). The assistant knows the current configuration and its problems, answers by streaming and, if it proposes changes, shows them as a **per-file diff before applying anything**, with the errors and warnings that would remain after applying them.

- **It is opt-in.** Without `--assistant`, no external service is called and nothing appears in the interface (in the web UI, the mobile "Assistant" tab only explains how to enable it).
- **What is sent.** The full configuration (with its passwords, addresses and certificates referenced by path) and the conversation are sent to the **Anthropic API** from the local process. The web UI and the TUI show a visible notice; the web UI asks for confirmation the first time it is used in each session.
- **The key** is read by the Anthropic SDK in the local server: `ANTHROPIC_API_KEY` or an `ant auth login` profile. It never reaches the browser or the chat history. `ANTHROPIC_BASE_URL` redirects the calls (useful for tests with a fake API).
- **Model.** `claude-opus-5-5` by default; change it with `--assistant-model` or `HARO_ASSISTANT_MODEL`.
- **Web.** With `haro serve --assistant`, the "✦ Assistant" button opens the chat on one side (on mobile, the bottom tab). The history is stored in the browser along with the rest of the state; "New conversation" clears it. **Apply** first saves a version in the history ("Before applying assistant proposal") and replaces the text of those files as a single action that can be undone with "Undo"; if the configuration changed since the proposal was made, it asks for confirmation. **Stop** cuts the response. Saving to disk remains separate and requires `--allow-write`.
- **TUI.** With `haro tui --assistant`, the `a` key opens a request line; the response arrives by streaming, `Esc` cancels it (or goes back to the sections), and when it finishes it shows the diff and "Apply and save? [y/N]". With `y` it saves with the same guarantees as `haro serve --allow-write` (validates the full configuration, atomic write and `.bak` copy); if validation rejects the proposal, it shows the reason. The conversation lasts as long as the session. Without `--assistant`, `a` reports that it is disabled.
- **CLI.** `haro assist "request" [files]` prints the response and the proposed diff; with `--write` it saves (asks for confirmation unless `--yes`).
- **Limits.** Up to 40 messages and 200 KB per conversation (the web UI sends the last 20); responses of up to 32,000 tokens (if cut off, it says so); if the response contains a configuration block that is truncated or cannot be assigned to a file, it is discarded and a warning is shown. The assistant can be wrong: review the diff and validate with `haproxy -c`. Each request consumes your Anthropic API quota. The serverless (WebAssembly) version does not include the assistant, so that the key is never in the browser.

### Multi-file configurations

haro loads paths like `haproxy -f`: in the given order and, for each directory, its non-hidden `.cfg` files in lexicographic order (without descending into subdirectories). The analysis treats the set as a single configuration:

- each file starts outside any section, because a section cannot be split across files;
- `defaults` inheritance, backend references and duplicate names do cross files;
- each problem reports its file and line (`conf.d/20-web.cfg:4`).

When saving from `haro serve`, the full configuration with the change applied is validated, not just the edited file. `ConfigService.PutConfig` accepts `files` (a list) to save several files at once (all or nothing), which is needed when a change, such as renaming a backend, touches two files.

`haro serve` listens only on `127.0.0.1:7468` and rejects requests with an external `Host`. Saving requires `--allow-write`. Before writing, haro validates with `haproxy -c` if the binary is available; if not, it uses its own detector and refuses to save if it finds errors. It always leaves a `.bak` copy of the previous version.

## Architecture

```
core/        Go core: lossless parser, serializer, knowledge base,
             explanations, detector, inheritance, topology, diff
core/knowledge/knowledge.json   Structure of the knowledge base (sections, directives…)
core/knowledge/text.es.json     Spanish texts (same shape and keys as text.en.json)
core/knowledge/text.en.json     English texts
core/testdata/                  .cfg cases with their expected output (.golden.json in English,
                                .golden.es.json in Spanish); each subdirectory is a multi-file case
scripts/fetch-corpus.sh         Downloads real configurations for regression tests
cmd/haro/    CLI and local server (serve)
proto/       API contract (Connect), source of truth for the types
web/         React + Vite + TypeScript interface, embedded in the binary
```

- The core does not depend on any interface. It is used by the CLI and `haro serve`, and by a WASM module for the web UI without a server.
- Explanations use minimal markdown (`**bold**`, `` `code` ``) that the terminal and the web UI each render in their own way.
- `core/testdata` is the project's specification: each `.cfg` has its `.golden.json` (English) and its `.golden.es.json` (Spanish) with the expected problems, explanations and topology.

### Language

Explanations and detector messages exist in English and Spanish. `haro lint`, `haro explain`, `haro tui` and `haro mcp` accept `--lang en|es`; English is the default product language. `haro lint --rules` and the SARIF and markdown formats are output in that language. The MCP server tools also accept a `lang` argument. In the API, `AnalyzeRequest` and `GetKnowledgeRequest` carry a `lang` field, and in the web UI the language is chosen in Settings. Only the text produced by the core changes: command messages and the interface text follow the project's default language.

## Development

```sh
make test     # tests
make golden   # regenerates the .golden.json files after an intentional change
make web      # builds the interface into web/dist (requires Node)
make proto    # regenerates gen/ and web/src/gen from proto/ (requires buf)
make build    # binary in bin/haro, with the web UI embedded
make run      # serve with the example configuration
```

To work on the web UI with hot reload: `go run ./cmd/haro serve` in one terminal and `cd web && npm run dev` in another. Vite proxies `/api` to the local server.

### Editing from code: `core.Apply`

All interfaces edit with operations (`add_section`, `rename_section`, `move_directive`…) that `core.Apply` applies to a copy and returns as the files that changed. Files the operation does not mention come back byte for byte. They are exposed in `CoreService.Apply` (`{files, ops}`); saving remains `ConfigService.PutConfig`. The operations are documented in `core/edit.go`.

### Corpus of real configurations

```sh
scripts/fetch-corpus.sh                 # downloads HariSekhon/HAProxy-configs (MIT) into core/testdata/corpus
go test ./core -run TestCorpus -v       # lossless rewrite + knowledge base coverage
```

The corpus is not included in the repo. The test is skipped if it has not been downloaded.

### Adding or fixing explanations

1. Structure (which sections and category a directive belongs to…) in `core/knowledge/knowledge.json`.
2. Text in **both** `core/knowledge/text.es.json` and `text.en.json`, with the same keys (a test checks this).
3. If the sentence depends on the arguments, `core/describe.go`, written in full in both languages (`x.t("Spanish", "English")`, or `x.f(templateES, templateEN, ...)` if the word order changes).
4. Add or extend a case in `core/testdata/`.
5. `make golden` and review the diff of the `.golden.json` and `.golden.es.json` files.

## Roadmap

The detailed plan is in [`docs/PLAN.md`](docs/PLAN.md).

- [x] Go core with golden cases
- [x] CLI: `lint`, `explain`, `diff`, `serve`
- [x] Local API (Connect RPC) with safe saving
- [x] Multi-file and multi-directory configurations
- [x] Optional test with a corpus of real configurations
- [ ] Extend the knowledge base with the client-native catalog (Apache-2.0)
- [x] Per-technology templates (Proxmox, Nextcloud, PostgreSQL, Redis, Elasticsearch, Kubernetes…)
- [x] Connect server and client generated from `proto/` with buf (`make proto`)
- [x] Web: code mode (CodeMirror 6), visual mode, map (React Flow) and problems panel
- [x] Web and TUI: AI assistant (`--assistant`)
- [x] Terminal interface with Bubble Tea (`haro` with no arguments)
- [x] Edit operations in the core (`apply`)
- [x] MCP server (`haro mcp`)
- [x] Assistant: context and proposal in the core, API call in the local server
- [x] Core in WebAssembly (`make wasm`, `WasmCoreClient`)
- [ ] Public web UI (separate repo)

## License and credits

MIT (see [`LICENSE`](LICENSE)). The catalog of directive names was derived from `haproxytech/config-parser` (Apache-2.0) and the optional test corpus is `HariSekhon/HAProxy-configs` (MIT); details in [`NOTICE`](NOTICE).
