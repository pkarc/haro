# haro — contexto para Claude Code

haro (*HAProxy readout*) es una herramienta open source para leer, explicar, validar y editar configuraciones de HAProxy. Público: comunidad DevOps, homelab y usuarios de Claude. El proyecto no está afiliado a HAProxy Technologies; no uses «HAProxy» como parte del nombre del producto (solo de forma descriptiva).

## Decisiones tomadas (no reabrir sin preguntar)

- **Núcleo en Go** (`core/`), sin dependencias externas. Es la única implementación de la lógica de HAProxy; la web y la TUI solo presentan.
- **Parser propio y sin pérdidas**: `Serialize(Parse(x)) == x` salvo espacios finales y `\r`. Se descartó usar `haproxytech/config-parser` (archivado, movido a client-native) porque regenera el archivo con su propio formato y trae muchas dependencias. Su catálogo de directivas (Apache-2.0) sí puede usarse como referencia para ampliar `core/knowledge/knowledge.json`.
- **Varios archivos**: se cargan como `haproxy -f` (orden dado; de cada directorio, los `.cfg` no ocultos en orden lexicográfico, sin subdirectorios). Cada archivo empieza fuera de sección; herencia de `defaults`, referencias y nombres cruzan archivos.
- **Explicaciones en markdown mínimo**: `**negrita**` y `` `código` ``. La web y la terminal lo pintan cada una a su manera. No metas HTML en el núcleo.
- **Binario único**: `haro` = CLI + `haro serve` (servidor local con la web embebida con `go:embed`). Sin nube.
- **API local**: Connect RPC (`/haro.v1.CoreService/*`, `/haro.v1.ConfigService/*`), generado con `buf` (`make proto`) desde `proto/haro/v1/haro.proto`. El código generado (`gen/`, `web/src/gen`) se commitea. La conversión núcleo↔proto vive en `cmd/haro/rpc.go`, nunca en `core`.
- **Web**: React + Vite + TypeScript. La web solo habla con el núcleo a través de `CoreClient` (`web/src/core/client.ts`), para poder añadir después un `WasmCoreClient` (web pública sin servidor).
- **TUI**: Bubble Tea (`haro` sin argumentos), con lipgloss y bubbles.
- **Web pública futura** (otro repo, p. ej. `haro-cloud`): misma web, núcleo compilado a WASM, sesión en localStorage.
- **Seguridad de `haro serve`**: solo `127.0.0.1` por defecto, rechaza `Host` externos (DNS rebinding), escritura solo con `--allow-write`, valida la configuración completa con `haproxy -c` (o con el detector si no hay binario) antes de guardar, escritura atómica y copia `.bak`. No relajes esto.

## Estructura

```
core/                 parser, serializer, knowledge, describe, lint, model (herencia, topología), diff, config (varios archivos), edit (Op/Apply)
core/knowledge/       knowledge.json (estructura) y text.es.json / text.en.json (textos): secciones, directivas, opciones, fetches… (datos, no código)
core/testdata/        *.cfg + *.golden.json (es) y *.golden.en.json (en); cada subdirectorio es un caso de varios archivos
cmd/haro/             main, cli (lint/explain/diff), files (expansión de rutas), serve + rpc (API Connect), tui (arranque)
internal/mcp/         servidor MCP por stdio (JSON-RPC a mano, dos eras del protocolo); las herramientas están en cmd/haro/mcp.go
skills/haro/          skill de Claude para usar las herramientas MCP
internal/tui/         interfaz de terminal (Bubble Tea); solo presenta, la lógica es de core
proto/haro/v1/        contrato del API
web/                  React + Vite + TS (esqueleto); web/placeholder.html se sirve si no hay build
docs/prototype/       prototipo completo de la interfaz en un solo HTML (referencia para portar)
scripts/              fetch-corpus.sh (corpus de HariSekhon/HAProxy-configs, MIT)
```

## Comandos

```sh
make test                         # go test ./...
make golden                       # regenera .golden.json tras un cambio intencionado (revisa el diff)
make build && make run            # binario y serve con el ejemplo
scripts/fetch-corpus.sh && go test ./core -run TestCorpus -v   # no-regresión con configs reales
cd web && npm install && npm run dev                           # web con recarga; /api va a haro serve :7468
```

## Convenciones

- Idioma: código y nombres en inglés; comentarios, mensajes, explicaciones y documentación en español. Está previsto soportar varios idiomas en `knowledge.json` (inglés y español).
- Todo cambio en explicaciones o reglas del detector lleva su caso en `core/testdata/` y `make golden` con el diff revisado.
- Las reglas del detector tienen identificador estable (`core.Rules`); no los renombres.
- El detector es orientativo: la validación definitiva es `haproxy -c`.
- Mantén el núcleo sin dependencias externas (debe compilar a WASM).

## Estado y próximos pasos

Hecho: núcleo con tests golden, varios archivos, CLI (`lint`, `explain`, `diff`, `serve`), API local con guardado seguro, contrato proto, esqueleto web, test opcional de corpus.

**El plan detallado, con tareas y criterios de aceptación por fase, está en `docs/PLAN.md`.** Resumen del orden:

0. Puesta a punto: licencia, ruta del módulo, CI (incluida validación real con `haproxy -c`).
1. Operaciones de edición en el núcleo (`apply`), base del modo visual, la TUI y el MCP.
2. Interfaz web en React portando el prototipo, usando `CoreClient` y `apply`.
3. Connect RPC generado con `buf`.
4. Interfaz de terminal con Bubble Tea.
5. Servidor MCP (`haro mcp`).
6. Distribución: GoReleaser, Homebrew, Docker, GitHub Action, SARIF, pre-commit.
7. Base de conocimiento ampliada e idiomas (inglés y español).
8. Plantillas, asistente, WASM y web pública.

## Licencia y módulo

- Licencia **MIT** (titular: Ivan Jaramillo). Si se usa el catálogo de `client-native` (Apache-2.0) como referencia, citar el origen en un `NOTICE`.
- Ruta del módulo: `github.com/pkarc/haro`.
