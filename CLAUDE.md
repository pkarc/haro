# haro — contexto para Claude Code

haro (*HAProxy readout*) es una herramienta open source para leer, explicar, validar y editar configuraciones de HAProxy. Público: comunidad DevOps, homelab y usuarios de Claude. El proyecto no está afiliado a HAProxy Technologies; no uses «HAProxy» como parte del nombre del producto (solo de forma descriptiva).

## Decisiones tomadas (no reabrir sin preguntar)

- **Núcleo en Go** (`core/`), sin dependencias externas. Es la única implementación de la lógica de HAProxy; la web y la TUI solo presentan.
- **Parser propio y sin pérdidas**: `Serialize(Parse(x)) == x` salvo espacios finales y `\r`. Se descartó usar `haproxytech/config-parser` (archivado, movido a client-native) porque regenera el archivo con su propio formato y trae muchas dependencias. Su catálogo de directivas (Apache-2.0) sí puede usarse como referencia para ampliar `core/knowledge/knowledge.json`.
- **Varios archivos**: se cargan como `haproxy -f` (orden dado; de cada directorio, los `.cfg` no ocultos en orden lexicográfico, sin subdirectorios). Cada archivo empieza fuera de sección; herencia de `defaults`, referencias y nombres cruzan archivos.
- **Explicaciones en markdown mínimo**: `**negrita**` y `` `código` ``. La web y la terminal lo pintan cada una a su manera. No metas HTML en el núcleo.
- **Binario único**: `haro` = CLI + `haro serve` (servidor local con la web embebida con `go:embed`). Sin nube.
- **API local**: hoy JSON sobre HTTP en `/api/v1/*`. El contrato está en `proto/haro/v1/haro.proto`; el plan es generar servidor y cliente con **Connect RPC** (`buf`), manteniendo las mismas rutas y mensajes.
- **Web**: React + Vite + TypeScript. La web solo habla con el núcleo a través de `CoreClient` (`web/src/core/client.ts`), para poder añadir después un `WasmCoreClient` (web pública sin servidor).
- **TUI**: Bubble Tea (`haro` sin argumentos), con lipgloss y bubbles.
- **Web pública futura** (otro repo, p. ej. `haro-cloud`): misma web, núcleo compilado a WASM, sesión en localStorage.
- **Seguridad de `haro serve`**: solo `127.0.0.1` por defecto, rechaza `Host` externos (DNS rebinding), escritura solo con `--allow-write`, valida la configuración completa con `haproxy -c` (o con el detector si no hay binario) antes de guardar, escritura atómica y copia `.bak`. No relajes esto.

## Estructura

```
core/                 parser, serializer, knowledge, describe, lint, model (herencia, topología), diff, config (varios archivos)
core/knowledge/       knowledge.json: secciones, directivas, opciones, fetches… (datos, no código)
core/testdata/        *.cfg + *.golden.json; cada subdirectorio es un caso de varios archivos
cmd/haro/             main, cli (lint/explain/diff), files (expansión de rutas), serve (API)
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

## Estado y próximos pasos (en este orden)

Hecho: núcleo con tests golden, varios archivos, CLI (`lint`, `explain`, `diff`, `serve`), API local con guardado seguro, contrato proto, esqueleto web, test opcional de corpus.

1. **Interfaz web**: portar `docs/prototype/haro-prototype.html` a React (CodeMirror 6 con modo HAProxy, modo visual por secciones, mapa con React Flow, problemas, diff, pestañas por archivo). Debe usar el núcleo vía `CoreClient`, no la lógica JS del prototipo. Las operaciones de edición del modo visual deberían acabar en el núcleo (`apply`), no en la web.
2. **Connect**: `buf generate` desde `proto/`; sustituir los handlers JSON y `web/src/core/{types,client}.ts` por el código generado.
3. **TUI con Bubble Tea**: lista de secciones, detalle con explicaciones, mapa en texto, panel de problemas, recarga al cambiar el archivo, abrir `$EDITOR` en la línea.
4. **Servidor MCP** (`haro mcp`): herramientas `lint`, `explain`, `analyze`, `diff` para Claude Code / Claude Desktop.
5. **Distribución**: GoReleaser (Linux/macOS, amd64/arm64), Homebrew tap, imagen Docker; GitHub Action y hook de pre-commit; salida SARIF.
6. **Base de conocimiento**: ampliar con el corpus y el catálogo de client-native; inglés además de español.
7. Plantillas por tecnología (Proxmox, Home Assistant, Nextcloud, PostgreSQL, Redis…), asistente, WASM y web pública.

## Pendiente de decidir por el autor

- Licencia (Apache-2.0 o MIT).
- Ruta definitiva del módulo: hoy `github.com/haro/haro`, suponiendo la organización `haro` en GitHub.
