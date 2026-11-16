English | [Español](CLAUDE.es.md)

# haro — context for Claude Code

haro (*HAProxy readout*) is an open source tool to read, explain, validate and edit HAProxy configurations. Audience: the DevOps community, homelab users and Claude users. The project is not affiliated with HAProxy Technologies; do not use "HAProxy" as part of the product name (only descriptively).

## Decisions already made (do not reopen without asking)

- **Core in Go** (`core/`), with no external dependencies. It is the only implementation of the HAProxy logic; the web UI and the TUI only present.
- **Own lossless parser**: `Serialize(Parse(x)) == x` except for trailing whitespace and `\r`. Using `haproxytech/config-parser` was ruled out (archived, moved to client-native) because it regenerates the file with its own format and brings many dependencies. Its directive catalog (Apache-2.0) can be used as a reference to extend `core/knowledge/knowledge.json`.
- **Multiple files**: loaded like `haproxy -f` (order as given; for each directory, the non-hidden `.cfg` files in lexicographic order, no subdirectories). Each file starts outside any section; `defaults` inheritance, references and names cross files.
- **Explanations in minimal markdown**: `**bold**` and `` `code` ``. The web UI and the terminal each render it their own way. Do not put HTML in the core.
- **Single binary**: `haro` = CLI + `haro serve` (local server with the web UI embedded via `go:embed`). No cloud.
- **Local API**: Connect RPC (`/haro.v1.CoreService/*`, `/haro.v1.ConfigService/*`), generated with `buf` (`make proto`) from `proto/haro/v1/haro.proto`. The generated code (`gen/`, `web/src/gen`) is committed. The core↔proto conversion lives in `cmd/haro/rpc.go`, never in `core`.
- **Web**: React + Vite + TypeScript. The web UI only talks to the core through `CoreClient` (`web/src/core/client.ts`), so that a `WasmCoreClient` can be added later (public web UI without a server).
- **TUI**: Bubble Tea (`haro` with no arguments), with lipgloss and bubbles.
- **Public web UI** (separate repo, open and free: `haro-site`): includes this repo as a **git submodule** (`haro/`) and declares `haro/web` as an npm workspace (a single install; `resolve.dedupe` for react). The same web UI with the core in WASM; its own code (`GitHubStore`, sign-in, a Cloudflare Worker only for the OAuth exchange, a GitHub App with *Contents* permission on the repos the user installs it on) lives there, never inside the submodule. That is why `web/` exposes extension points (`ConfigStore`, `mountApp`). No npm package is published.
- **`haro serve` security**: only `127.0.0.1` by default, rejects external `Host` headers (DNS rebinding), writing only with `--allow-write`, validates the complete configuration with `haproxy -c` (or with the detector if there is no binary) before saving, atomic write and `.bak` copy. Do not relax this.

## Structure

```
core/                 parser, serializer, knowledge, describe, lint, model (inheritance, topology), diff, config (multiple files), edit (Op/Apply)
core/knowledge/       knowledge.json (structure) and text.es.json / text.en.json (texts): sections, directives, options, fetches… (data, not code)
core/testdata/        *.cfg + *.golden.json (en) and *.golden.es.json (es); each subdirectory is a multi-file case
cmd/haro/             main, cli (lint/explain/diff), files (path expansion), serve + rpc (Connect API), tui (startup)
internal/mcp/         MCP server over stdio (hand-written JSON-RPC, two protocol eras); the tools are in cmd/haro/mcp.go
skills/haro/          Claude skill to use the MCP tools (English only: skills load a single SKILL.md)
internal/tui/         terminal interface (Bubble Tea); only presents, the logic lives in core (assist.go: assistant panel)
proto/haro/v1/        API contract
web/                  React + Vite + TS; web/src/assistant/ is the assistant chat; web/placeholder.html is served if there is no build
docs/prototype/       complete interface prototype in a single HTML file (reference for porting)
scripts/              fetch-corpus.sh (corpus from HariSekhon/HAProxy-configs, MIT)
```

## Commands

```sh
make test                         # go test ./...
make golden                       # regenerates .golden.json after an intentional change (review the diff)
make build && make run            # binary and serve with the example
scripts/fetch-corpus.sh && go test ./core -run TestCorpus -v   # non-regression with real configs
cd web && npm install && npm run dev                           # web UI with reload; /api goes to haro serve :7468
```

## Conventions

- Language: code, names, comments, messages and documentation in English by default; Spanish is the alternative language (`core/knowledge/text.es.json`, documentation in `.es.md` files, e.g. `README.es.md`; Spanish text in neutral Latin American Spanish). The product's default language is English. `knowledge.json` supports English and Spanish.
- Documentation: the English file keeps the plain name (`README.md`, `docs/PLAN.md`, `docs/IMPLEMENTATION.md`) and the Spanish one uses the `.es.md` suffix (`docs/IMPLEMENTACION.es.md`); each carries a cross-link at the top. Keep both in sync.
- Every change to explanations or detector rules comes with its case in `core/testdata/` and `make golden` with the diff reviewed.
- Detector rules have a stable identifier (`core.Rules`); do not rename them.
- The detector is advisory: the definitive validation is `haproxy -c`.
- Keep the core free of external dependencies (it must compile to WASM).

## Status and next steps

Phases 0 to 8 of `docs/PLAN.md` are done, except the public web UI (separate repo, pending decisions: GitHub App, OAuth Worker, `ConfigStore`). The summary of each phase, with the gaps found and how they were resolved, is in `docs/IMPLEMENTATION.md`.

Pending push/publication: create `pkarc/homebrew-tap` and the `HOMEBREW_TAP_GITHUB_TOKEN` secret, the first `v*` tag, test the assistant with a real key and the GitHub Action in a repo.

## License and module

- **MIT** license (holder: Ivan Jaramillo). If the `client-native` catalog (Apache-2.0) is used as a reference, cite the origin in a `NOTICE`.
- Module path: `github.com/pkarc/haro`.
