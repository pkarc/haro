English | [Español](IMPLEMENTACION.es.md)

# Implementation summary

Status of each phase of [`PLAN.md`](PLAN.md): what was done, which gaps were found when reviewing it and how they were resolved, and what is still pending. Everything is verified with tests; anything that could not be tested on this machine is stated explicitly.

## Phase 0 · Setup
**Done:** MIT license, module `github.com/pkarc/haro`, CI (Go on Linux/macOS, `haproxy -c`, corpus, web, e2e, WASM, buf, Docker), `CONTRIBUTING.md`, integration tests against a real `haproxy -c`.

| Gap | Solution |
|---|---|
| `testdata/multi` is not valid for HAProxy (repeated name and a directive outside a section, on purpose) | New valid case `multiok/`; the test also checks that HAProxy **rejects** `multi` |
| `haproxy` in Docker could not read 0700 temporary directories | Readable temporary directories and a container running with the user's UID |
| `go install …@latest` fails until the repo is published | Pending push; `go install ./cmd/haro` works |

## Phase 1 · Editing (`core.Apply`)
**Done:** 8 operations (`add/remove/move/rename_section`, `add/update/remove/move_directive`), section templates, `POST apply`, merging of multiple files, minimal changes and untouched files byte for byte.

| Gap | Solution |
|---|---|
| Comments attached to a header were left orphaned when moving or deleting | Temporary decomposition into blocks (comment + section) |
| An `append` on sub-slices overwrote lines | Slices with bounded capacity |
| Removing an added section did not restore the original text | Inverse-operation tests over `testdata` and the corpus; edge cases documented |
| Templates could be invalid | Each template is tested against `haproxy -c` |

## Phase 2 · React web
**Done:** code mode (CodeMirror 6), visual mode through `apply`, map (React Flow), problems, save with diff, import/export, history, undo, responsive layout; 15→44 Vitest tests and Playwright e2e.

| Gap | Solution |
|---|---|
| A change touching two files (renaming a backend with references) could not be saved: saving each file separately left a broken reference | `PutConfig` accepts `files[]`: the whole set is validated and saved **all or nothing**, with rollback |
| `make build` embedded an old web build if `make web` was not run | Documented and used in all verifications |
| `haproxy -c` from the UI untested | Verified against a real `haproxy:lts` through the API |
| Pending | Autocomplete covers only the first word; bundle of ~940 kB without code-splitting |

## Phase 3 · Connect RPC
**Done:** `buf` generates Go (`gen/`) and TypeScript (`web/src/gen`); `CoreService`, `ConfigService` (and later `AssistService`); the web uses the generated client behind `CoreClient`; JSON routes `/api/v1` removed.

| Gap | Solution |
|---|---|
| Drift between the core and the contract | Strict core→proto conversion and `TestRPCContract` over all cases and the corpus |
| An external page could make "simple" POSTs | Connect only accepts its own content types (415 for `text/plain`); specific test |
| `gofmt` not applied to `rpc.go`/`serve.go` (would break CI) | Fixed; CI checks it |
| Outdated generated code | `proto` job in CI that fails if `make proto` changes anything |
| `placeholder.html` still used the old route | Updated |

## Phase 4 · TUI (Bubble Tea)
**Done:** sections with detail, problems with filter and jump to the line, tree map, `e` opens `$EDITOR` at the exact line, reload with `fsnotify`, help, adaptive colors and `NO_COLOR`, `haro` without a terminal shows the help.

| Gap | Solution |
|---|---|
| The scroll window was not remembered (it jumped when scrolling up) | Persistent offsets + test |
| Panels taller than the screen | Content height computed (borders, title, bar) |
| Robustness with real configurations | Test that walks the corpus at several widths |
| Tests in tmux could affect other sessions | Dedicated socket (`tmux -L`); never `kill-server` |

## Phase 5 · MCP server
**Done:** `haro mcp` (stdio, no dependencies): `haro_lint/explain/analyze/diff/check/apply`, resources `haro://knowledge` and `haro://rules`, Claude skill. **Verified with real Claude Code** (it cited `problems.cfg:13`).

| Gap | Solution |
|---|---|
| The specification changed: there is a modern era (2026-07-28, sessionless) and the legacy one with `initialize` | "Dual-era" server (the client decides); `server/discover` and an unsupported-version error |
| Reading/writing arbitrary paths | Repeatable `--root` with symlinks resolved and no prefix tricks; `haro_apply` only writes with `--allow-write` and `serve`'s guarantees |
| Injection from comments in the configuration | The server instructions treat the content as untrusted data |
| Known limits | Requests are serial (cancellation is ignored); `haro_analyze` can be large |

## Phase 6 · Distribution
**Done:** GoReleaser (6 binaries, `.deb`, `.rpm`, Homebrew formula, checksums; snapshot verified), 22 MB Docker image (tested), GitHub Action, SARIF output (validated against the official schema) and markdown, pre-commit.

| Gap | Solution |
|---|---|
| `brews` is deprecated in GoReleaser v2 | Kept on purpose: its replacement (casks) is macOS-only and Linuxbrew would be lost; noted in the file |
| `go install` builds without the web (`web/dist` is not versioned) | Documented; releases, Homebrew and Docker do include it |
| The Action cannot be run outside GitHub | The shell logic was tested locally for the error/warning/never cases; the full flow remains to be tested on publishing |
| Pending (requires your account) | Create `pkarc/homebrew-tap` and the `HOMEBREW_TAP_GITHUB_TOKEN` secret; publish the first tag |

## Phase 7 · Knowledge and languages
**Done:** Spanish and English in explanations and detector (`--lang`, `lang` in the API, selector in the web, MCP and TUI); catalog of 324 directives (derived from `config-parser`, Apache-2.0, with `NOTICE`); **100% corpus coverage** (previously 98.8%); `since` and `--haproxy-version` (rule `newer-than-target`); 7 new detector rules.

| Gap | Solution |
|---|---|
| The language cannot be a global variable (concurrent requests) | `tr{lang}` context passed along in the call; test with `-race` |
| English texts inherited defects from the Spanish ("1 days", "the path is greater than 100", `tcp-request` without a phase) | Fixed in both languages with tests |
| The agent for the second half stopped midway | What was done was reviewed and finished by hand (`NOTICE`, docs, verification) |
| `since` coverage | Only 31 directives, the safe ones; the rest have no `since` on purpose |
| Done later | English became the default language (core, CLI, RPC, MCP, wasm, web, TUI, docs); `Apply` errors and interface texts are translated (`es` is neutral Latin American Spanish). Goldens: English `x.golden.json`, Spanish `x.golden.es.json` |

## Phase 8 · Templates, assistant, WASM
**Done:**
- **Technology templates:** 11 (Proxmox, Home Assistant, Nextcloud, Jellyfin, Gitea, Grafana, Vaultwarden, Elasticsearch, PostgreSQL, Redis, Kubernetes) in the core, API, CLI, MCP and web; validated with `haproxy -c -dW` (2.8 and LTS).
- **WASM:** `make wasm` (6.2 MB; 1.6 MB gzip) and `make wasm-tiny` (TinyGo, 1.8 MB; 620 KB gzip); `WasmCoreClient`; the web works without a server with `VITE_CORE=wasm` and an e2e test checks that it makes no API calls.
- **Assistant:** context and proposal extraction in the core; streaming `AssistService`; `haro assist`; chat in the web with a proposal card (diff, apply with a prior version, undo); panel in the TUI. Opt-in with `--assistant`; the key never reaches the browser.

| Gap | Solution |
|---|---|
| A response cut off by `max_tokens` could propose a half file | Unclosed blocks are discarded (`truncated`) |
| The proposal lost the trailing newline | Matched to that of the current file |
| A second web template would open another `bind *:443` | It joins the existing 443 frontend with a host ACL |
| The assistant could not be tested against the real API (no key) | Fake SSE API in tests and e2e; 401 error verified; **to be tested with your key** |
| `refusal fallbacks` | Not enabled: it requires a beta endpoint I could not verify; the user sees the refusal category |
| Pending | `ConfigStore`/`GitHubStore` and the OAuth Worker (see below) |

## Public web (pending your decision)
Agreed: a separate `haro-site` repo, open and free, which includes this repo as a git submodule (`haro/`) and `haro/web` as an npm workspace (no published npm package); GitHub App with contents permission only on the chosen repos, Cloudflare Worker only for the OAuth exchange, token in the browser, write to a branch or PR at the user's choice, free. Missing: registering the GitHub App on your account and creating the `haro-site` repo with the `GitHubStore`, the Worker and the sign-in. In this repo, the extension points that make it possible (`ConfigStore` with `LocalStore`, `mountApp`, exporting `web/src`) are prepared by the "haro-site" session. The "own account" mode with end-to-end encryption remains a candidate for a premium feature.
