English | [Español](PLAN.es.md)

# haro work plan

> **Status:** phases 0 to 8 done, except the public web (8.4). Summary and gaps in [`IMPLEMENTATION.md`](IMPLEMENTATION.md).

Detailed plan to continue the project in an environment with network access (Claude Code on the author's machine). Each phase has an objective, tasks, design notes and acceptance criteria. The phases are in the recommended order; within each one, the order of the tasks also matters.

General rules for all phases:

- Read `CLAUDE.md` before starting: it records the decisions already made.
- Each phase ends with `make test` green, the binary built (`make build`) and the feature actually tried out (by running it, not just compiling it).
- One commit per task or small group of tasks.
- If a task forces a change to a decision in `CLAUDE.md`, ask first.

---

## Phase 0 · Repository setup

**Objective:** leave the repo ready to be published and to receive contributions.

Tasks:

1. ~~License~~: done, MIT with `LICENSE` (Ivan Jaramillo).
2. ~~Module path~~: done, `github.com/pkarc/haro` in `go.mod`, imports and `proto` (`go_package`).
3. **CI in GitHub Actions** (`.github/workflows/ci.yml`):
   - `go vet ./...`, `go test ./...` and `gofmt -l .` (fails if there are unformatted files) on Linux and macOS.
   - Optional job that runs `scripts/fetch-corpus.sh` and `go test ./core -run TestCorpus -v`, and publishes coverage in the job summary.
   - Web job (`npm ci && npm run build`) as soon as phase 2 exists.
4. **Real HAProxy validation in CI**: install `haproxy` (Ubuntu package or `haproxy:lts` image) and add an integration test that runs every `core/testdata/*.cfg` through `haproxy -c` with no unexpected errors, to check that the "valid" cases really are valid.
5. Short `CONTRIBUTING.md`: how to add explanations (knowledge.json + testdata case + `make golden`) and detector rules.

Acceptance: CI green on a push; `LICENSE` present; `go install <path>/cmd/haro@latest` works from a test repo.

---

## Phase 1 · Editing operations in the core (`apply`)

**Objective:** have all edits (visual mode, TUI, MCP, assistant) go through the core and produce minimal changes to the text.

Design:

- New file `core/edit.go` with an `Op` type serializable to JSON:
  - `add_section {type, name, at?, template?}`
  - `remove_section {section}`
  - `move_section {section, to}`
  - `rename_section {section, name, update_refs: bool}` (updates `default_backend` / `use_backend` in all files)
  - `add_directive {section, text, at?}`
  - `update_directive {section, line_index, text}`
  - `remove_directive {section, line_index}`
  - `move_directive {section, line_index, delta}` (skips blank lines)
- `func Apply(cfg *Config, ops ...Op) (*Config, []FileChange, error)`: applies on the unified view and returns which files changed with their new text. It must reject a line that is actually a section header in `add/update_directive`.
- Preserve formatting: new lines use `DefaultIndent`; `EnsureSeparation` only on the touched files.
- Section templates in `core/knowledge/templates.json` (the same ones the prototype uses in `TEMPLATES`).

Tasks:

1. Implement `Op` and `Apply` with unit tests per operation (text before → text after).
2. Multi-file case: renaming a backend defined in `30-api.cfg` updates the reference in `20-web.cfg`.
3. Expose `POST /api/v1/apply {files, ops}` in `haro serve` and add it to the `proto`.
4. Add `apply` to `CoreClient`.

Acceptance: all editing tests pass; `Serialize` of a file not touched by the operation is still byte-for-byte identical.

---

## Phase 2 · React web interface

**Objective:** port `docs/prototype/haro-prototype.html` to `web/`, using the Go core through `CoreClient` (no HAProxy logic in the web).

Suggested dependencies: `@codemirror/*` 6, `@uiw/react-codemirror` (or a custom integration), `@xyflow/react` (React Flow), `zustand` (state + localStorage persistence), Radix UI or shadcn/ui for dialogs and menus.

Tasks, in this order:

1. **State and data** (`zustand`): files (`SourceFile[]`), active file, latest analysis, mode (code/visual), undo/redo history, saved versions. Analysis *debounced* by ~200 ms after each change.
2. **Code mode** with CodeMirror 6:
   - A custom HAProxy language (`StreamLanguage` or a simple Lezer grammar) with the same colors as the prototype (`hlLine` in the prototype is the reference).
   - CodeMirror linter fed by the active file's `analysis.issues`.
   - Directive autocompletion by section type (the prototype's `SUGGEST`; better generated from `knowledge.json`, exposed through a `GET /api/v1/knowledge` endpoint).
   - Bottom bar with the explanation of the line under the cursor.
   - One tab per file when there are several.
3. **Visual mode**: cards per section (type, name, title, description, effective mode and inherited timeouts), directives with category, arguments and explanation (rendering the minimal markdown). Inline editing, add, move, delete, rename and add section from a template, **all through `apply`** (phase 1).
4. **Traffic map** with React Flow: entries → backends → servers from `analysis.topology`; default edges (solid) and conditional edges (dashed, with the condition in the tooltip); missing backend in red; clicking a node jumps to its section.
5. **Problems panel**: list with file:line, go to line, filter by severity.
6. **Save** (if `capabilities.canWrite`): a button that calls `putFile` and shows the `haproxy -c` output if it fails. Show the diff before saving.
7. **Import/export, version history and undo/redo** as in the prototype.
8. **Responsive design**: bottom tabs on mobile (Code / Visual / Assistant) as in the prototype.
9. Build with `make web` and verify that `make build` embeds the web (`web/dist`).

Acceptance: `make run` opens the interface with the example; editing in visual mode changes the text minimally; problems appear in the editor and in the panel; with `--file dir/ --allow-write` different files can be edited and saved, and saving is rejected if it breaks a cross-file reference. Test on desktop and on a mobile viewport (Playwright).

---

## Phase 3 · Connect RPC

**Objective:** shared, generated types between Go and TypeScript.

Tasks:

1. `buf.yaml` and `buf.gen.yaml` at the root; plugins `protoc-gen-go`, `protoc-gen-connect-go` and `@bufbuild/protoc-gen-es`.
2. `buf generate` → `gen/haro/v1` (Go) and `web/src/gen` (TS). Add `make proto`.
3. Implement `CoreService` and `ConfigService` in Go reusing the current logic of `serve.go`. Temporarily keep the JSON routes `/api/v1/*` if the web still uses them, and remove them when finished.
4. Replace `web/src/core/types.ts` and `client.ts` with the generated client behind the same `CoreClient` interface.
5. Keep `localOnly`, the size limit and the write rules.

Acceptance: the web works the same using only the Connect client; the `serve` tests cover the Connect routes.

Note: converting core structs to proto messages must be done in `cmd/haro` (or an `internal/rpc` package), never inside `core`, to avoid adding dependencies to it.

---

## Phase 4 · Terminal interface with Bubble Tea

**Objective:** `haro` with no arguments (or `haro tui [paths]`) opens an interactive interface.

Dependencies: `charmbracelet/bubbletea`, `lipgloss`, `bubbles` (list, viewport, help, key), `fsnotify` for reload.

Screen design:

```
┌ Sections ────────────┬ Detail ──────────────────────────────────────┐
│ global               │ backend api_servers  Server group            │
│ defaults             │ mode http (inherited from defaults)          │
│ frontend http_in   ! │                                              │
│ frontend https_in    │  49 balance leastconn                        │
│ backend web_servers  │     [Servers] Balancing: fewest connections  │
│ backend api_servers  │  50 server api1 10.0.0.21:9000 check …       │
│ …                    │     [Servers] Server api1 at …               │
├──────────────────────┴──────────────────────────────────────────────┤
│ 2 errors · 3 warnings  [tab] panel  [m] map  [e] edit  [?] help     │
└─────────────────────────────────────────────────────────────────────┘
```

Tasks:

1. Model with a list of sections (grouped by file if there are several) and a detail panel with explanations (reuse the minimal markdown renderer from `cli.go`).
2. Problems view (`p`): navigable list; Enter jumps to the section and highlights the line.
3. Map view (`m`): text tree `frontend ──if cond──▶ backend ──▶ servers`.
4. `e`: opens `$EDITOR` on the selected file and line (`+N` for vim/nano/emacs; `-g file:N` for VS Code) and re-analyzes on return.
5. Automatic reload with `fsnotify` when the files change.
6. Help (`?`), colors adapted to light or dark terminals (`lipgloss.AdaptiveColor`), `NO_COLOR` support.
7. `haro` with no arguments and no interactive terminal (for example, in a pipe) still shows the help.

Acceptance: tested manually with the example, with `core/testdata/multi/` and with corpus files; model tests with `teatest` for basic navigation.

---

## Phase 5 · MCP server (`haro mcp`)

**Objective:** have Claude Code, Claude Desktop and other MCP clients use haro's deterministic analysis.

Design:

- stdio transport with JSON-RPC 2.0. It can be implemented with the standard library or with the official Go SDK for MCP (`github.com/modelcontextprotocol/go-sdk`), if its API is stable at the time; check the current specification before starting.
- Tools:
  - `haro_lint {paths?: string[], text?: string, disable?: string[]}` → problems with file and line.
  - `haro_explain {paths?: string[], text?: string, section?: string}` → explanations per section.
  - `haro_analyze {paths?|text?}` → full analysis (JSON).
  - `haro_diff {before, after}`.
  - `haro_check {paths?|text?}` → `haproxy -c` if available.
  - `haro_apply {paths, ops, write?: bool}` (after phase 1): only writes if the server was launched with `--allow-write`, with the same guarantees as `serve`.
- Optional resources: `haro://knowledge` (knowledge base) and `haro://rules` (detector rules).
- Paths: accept whatever it receives, but allow restricting them with `--root dir` (repeatable).

Tasks:

1. Implement `haro mcp` with the read-only tools.
2. Tests: simulated JSON-RPC session over stdin/stdout (initialize, tools/list, tools/call).
3. Document in the README how to register it in Claude Code (`claude mcp add haro -- haro mcp`) and in Claude Desktop (configuration block).
4. Optional: a Claude skill with HAProxy best practices that uses these tools before proposing a configuration.

Acceptance: registered in Claude Code, a request like "review /etc/haproxy/haproxy.cfg" calls `haro_lint` and `haro_explain` and the response cites file and line.

---

## Phase 6 · Distribution and integrations

**Objective:** make installing and using haro trivial.

Tasks:

1. **GoReleaser** (`.goreleaser.yaml`): Linux, macOS (and Windows if it is not costly) on amd64 and arm64; checksums; `-ldflags "-X main.version=…"`; changelog from the commits. A workflow that publishes when a `v*` tag is created.
2. **Homebrew tap** (`pkarc/homebrew-tap`) generated by GoReleaser.
3. Minimal **Docker image** (`FROM scratch` or `distroless`) with the binary; example `docker run -v /etc/haproxy:/cfg:ro haro lint /cfg`.
4. **GitHub Action** (`pkarc/haro` (`action.yml`) or `action.yml` in the repo): runs `haro lint --format sarif` and uploads the result; comments on the PR.
5. **SARIF output** in `haro lint --format sarif` (rules with `Rules` as the description).
6. **Pre-commit hook** (`.pre-commit-hooks.yaml`).
7. `.deb`/`.rpm` packages with nfpm (optional).

Acceptance: `brew install pkarc/tap/haro`, `docker run …` and the Action work in a test repo.

---

## Phase 7 · Knowledge base and languages

**Objective:** explain almost all real-world directives, in English and Spanish.

Tasks:

1. Run the corpus and prioritize the directives without an explanation by frequency.
2. Use the directive catalog of `haproxytech/client-native` (Apache-2.0) to complete `sections` and categories; cite the origin in `knowledge.json` or in `NOTICE`.
3. **Languages**: move the texts of `knowledge.json` and `describe.go` to keys with translations (`es`, `en`); `--lang` in the CLI, `LANG` by default, selector in the web. The phrases with arguments in `describe.go` need per-language templates.
4. HAProxy versions: `since` field on directives and a warning if something newer than the version given with `--haproxy-version` is used.
5. New detector rules suggested by the corpus: public exposure without an IP ACL, stats password `test`, `stats` without `auth` on a public bind, missing `option forwardfor` in http, etc. Each with an identifier, case and golden.

Acceptance: corpus coverage above 95%; golden files in English and Spanish.

---

## Phase 8 · Templates, assistant, WASM and public web

1. **Technology templates** (`core/knowledge/templates/`) — **done (8.1)**: Proxmox VE, Home Assistant, Nextcloud, Jellyfin, Gitea, Grafana, Vaultwarden, PostgreSQL, Redis, Elasticsearch and Kubernetes API, with TLS and redirection for the web ones. One JSON per technology interpreted by `core.RenderTechTemplate` (returns operations for `Apply`); `CoreService.ListTemplates`/`RenderTemplate`, `haro template list|show|add`, MCP tools `haro_template_list`/`haro_template_render` and a dialog in the web's visual mode. Validated with `haproxy -c -dW` (LTS 3.4 and 2.8). Pending: user defaults with HTTP-only directives (`option forwardfor`) make HAProxy warn on the TCP sections that inherit them (a named `defaults` with `from` would be needed).
2. **Assistant** — **done (8.2)**: core (`core/assist.go`: context and proposal extraction), server (streaming `AssistService.Assist`, `haro serve --assistant`, `haro assist`), chat in the web (`web/src/assistant/`, proposal card with diff, apply with a prior version and undo, e2e with a fake API) and a TUI panel (key `a`, `haro tui --assistant`). Only `haro serve`/CLI/TUI call the Anthropic API; the serverless web does not. Original detail:
   - Context building in the core (`core.BuildPrompt(cfg, issues, request)`) and extraction of the proposed configuration from the response.
   - Anthropic API call in `haro serve` (key in `ANTHROPIC_API_KEY`, never in the browser) and in the TUI.
   - In the web: chat with streaming, diff before applying and a version saved before applying (as in the prototype). The prototype's prompt (`SYSTEM_PROMPT`) is a good starting point.
3. **WASM**: `wasm/main.go` with `syscall/js` exposing `analyze`, `apply`, `diff`; try with standard Go and with TinyGo and measure size; `WasmCoreClient` in the web.
4. **Public web** (separate repo): the same web with `WasmCoreClient`, session in localStorage, assistant with the user's own key or with an intermediary with limits.

---

## References

- Prototype: `docs/prototype/haro-prototype.html` (full interface, logic in JS as a behavior reference).
- HAProxy brand policy: use "HAProxy" only descriptively and with a non-affiliation notice.
- Loading multiple files: `haproxy -f` documentation (argument order; directories with `.cfg` in lexicographic order, no hidden files or subdirectories).
- Corpus: https://github.com/HariSekhon/HAProxy-configs (MIT).
- client-native: https://github.com/haproxytech/client-native (Apache-2.0).
