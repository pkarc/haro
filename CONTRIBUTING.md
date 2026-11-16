English | [Español](CONTRIBUTING.es.md)

# Contributing to haro

Thanks for wanting to help. Read `CLAUDE.md` first: it records the design decisions already made (Go core with no dependencies, lossless parser, `haro serve` security…). A Spanish version is in `CLAUDE.es.md`.

## Setting up the environment

```sh
make test      # go test ./...
make build     # binary in bin/haro
```

The web UI needs Node: `make web`. The e2e tests (`cd web && PW_CHANNEL=chrome npx playwright test`, after `make web && make build` and `npm run build:wasm`) include the assistant against a fake Anthropic API (`web/e2e/fake-anthropic.mjs`): never call the real API in tests.

## Adding or improving explanations

Explanations exist in English and Spanish. A complete change touches:

1. **Structure** in `core/knowledge/knowledge.json`: sections where the directive is valid (`sections`), category (`category`), colors, `booleanFetches`, `builtinAcls`…
2. **Text** in `core/knowledge/text.es.json` and `core/knowledge/text.en.json`, with exactly the same keys and no empty values (`TestKnowledgeTextsSameShape` checks this). The text in each language must sound natural and technical, not like a literal translation.
3. **Sentences with arguments** in `core/describe.go`, written in full in both languages with the `x tr` context: `x.t("Cambia la ruta a ", "Sets the path to ")`. When the word order changes, use two complete templates with `x.f(templateES, templateEN, args...)` instead of concatenating. The language always travels in `x`; never store it in a global variable (the server handles several languages at once).
4. A case in `core/testdata/` (`*.cfg`; a subdirectory is a multi-file case). `coverage.cfg` goes through almost all the sentences.
5. `make golden`, and **review the diff** of the `.golden.json` files (English) and the `.golden.es.json` files (Spanish). The goldens of a language should not change if you did not touch it. Read the new text by hand: `TestNoLanguageLeftovers` catches leftovers from the other language, but it is no substitute for reading it.

Explanations use minimal markdown: `**bold**` and `` `code` ``. Do not add HTML.

Edit errors (`core/edit.go`, `ApplyError`) and interface texts (CLI, TUI, web) are not translated.

## Adding a technology template

Per-technology templates (`haro template`, Phase 8.1) are data: one JSON per technology in `core/knowledge/templates/<id>.json` (the file name is the `id`), which `core/techtemplates.go` interprets and returns as edit operations (`add_section` + `add_directive`) for `core.Apply`. No code is written to add one.

```json
{
  "id": "myapp", "kind": "web",
  "name": {"es": "Mi app", "en": "My app"},
  "description": {"es": "…", "en": "…"},
  "params": [
    {"key": "servers", "type": "list", "required": true, "example": "10.0.0.1:8080",
     "label": {"es": "Servidores (host:puerto)", "en": "Servers (host:port)"},
     "help": {"es": "…", "en": "…"}},
    {"key": "ssh", "type": "bool", "default": false, "label": {…}},
    {"key": "ssh_port", "type": "int", "default": 22, "dependsOn": "ssh", "label": {…}}
  ],
  "sections": [
    {"type": "backend", "lines": [
      "mode http",
      {"comment": {"es": "…", "en": "…"}},
      {"if": "ssh", "text": "timeout tunnel 1h"},
      "http-check send meth GET uri /health ver HTTP/1.1 hdr Host {{domain}}",
      {"each": "servers", "text": "server app{{n}} {{item}} check"}
    ]}
  ]
}
```

- `kind` `web`: the engine adds the `domain` and `cert` parameters and generates the frontends (port 80 with a redirect to HTTPS and port 443 with `ssl crt … alpn h2,http/1.1`) pointing at the backend without `suffix`; if a TLS frontend already exists on 443 it joins it with a per-domain ACL. The JSON only describes the backend and extras, and **all technology-specific logic goes in the backend** (headers, redirects, `timeout tunnel`, checks), because that is the only thing that can be applied the same way when the frontend is shared. `kind` `tcp`: you describe all the sections (frontend with `suffix` `_in` and backend). A frontend and a backend cannot have the same name (HAProxy 3.3+).
- Parameter types: `string` (optional `validate` validators: `domain`, `time`, `bind`; by default, a token without spaces, quotes, `#` or braces), `int` (port 1–65535), `bool` and `list` (`host:port` items). `required` and `default` are mutually exclusive; `dependsOn` points to a `bool` declared earlier and hides the parameter (in the web UI) and ignores its value if it is off.
- Lines: a string, or an object with `text`, `if` (`key`, `!key` or several joined with `&`; besides the parameters, `logging` indicates there is a log destination, to set `option tcplog`), `each` (+ `where`: `first`/`rest`) or `comment`. Placeholders: `{{key}}`, `{{base}}` (collision-free name) and, inside `each`, `{{item}}`, `{{host}}`, `{{port}}`, `{{n}}`. All visible text carries `es` and `en`. The engine validates the template on load (an unknown placeholder makes startup and the tests fail).
- Do not put public `bind`s (`*`) on internal services (databases, caches): ask for `bind` as a required parameter, so the detector (`internal-port-public`) does not warn. Do not use directives missing from the oldest supported LTS (2.8): if a newer one is needed, put it behind an opt-in parameter.
- Tests: add the case to `typicalParams` in `core/techtemplates_test.go` (typical parameters; the result must pass the detector with **no warnings or suggestions at all**), run `go test ./core -run TechTemplates -update` and **review** `core/testdata/templates/<id>.golden.cfg` and `.golden.en.cfg`. `TestHAProxyAcceptsTechTemplates` (see "Validating with HAProxy") checks that `haproxy -c -dW` accepts it without warnings.
- Template texts are original; if you take inspiration from a published configuration, cite it in the commit message.

## Detector rules

- Each rule has a stable identifier in `core.Rules`. Do not rename it: `--disable`, the JSON output and users' continuous integration depend on it.
- Every new rule comes with its case in `core/testdata/` and its goldens, in English and Spanish: a description in `rulesES` and `rulesEN` (`core/lint.go`) and the message in both languages in the `add` call.
- The detector is advisory. The definitive validation is `haproxy -c`.

## Validating with HAProxy

The `HAProxy*` tests in `core` check the cases against `haproxy -c`. They are skipped if there is no binary. To run them:

```sh
HARO_HAPROXY=/path/to/haproxy go test ./core -run HAProxy -v
# or with Docker, using a wrapper that mounts /tmp:
#   exec docker run --rm -v /tmp:/tmp:ro haproxy:lts haproxy "$@"
```

`HARO_REQUIRE_HAPROXY=1` makes them fail instead of being skipped (this is how they run in CI).

## Corpus of real configurations

```sh
scripts/fetch-corpus.sh && go test ./core -run TestCorpus -v
```

It is used to check that the parser loses nothing and to see which directives are still unexplained.

## Style

- Code, names, comments, messages and documentation in English by default; Spanish is the alternative language (`core/knowledge/text.es.json`, `.es.md` documents).
- `gofmt` and `go vet` clean. The core (`core/`) cannot depend on external packages: it must compile to WASM.
- One commit per logical change, with an English message.

## HAProxy versions (`since`)

A directive can carry `"since": "2.4"` in `knowledge.json`: the HAProxy version in which it appeared. With `--haproxy-version`, the detector warns (rule `newer-than-target`) about anything that requires a later version. Set `since` only if you are sure of the version; if in doubt, leave it empty.
