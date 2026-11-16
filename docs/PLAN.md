# Plan de trabajo de haro

Plan detallado para continuar el proyecto en un entorno con red (Claude Code en la máquina del autor). Cada fase tiene objetivo, tareas, notas de diseño y criterios de aceptación. Las fases están en el orden recomendado; dentro de cada una, el orden de las tareas también importa.

Reglas generales para todas las fases:

- Lee `CLAUDE.md` antes de empezar: recoge las decisiones ya tomadas.
- Cada fase termina con `make test` en verde, el binario compilado (`make build`) y la funcionalidad probada de verdad (ejecutándola, no solo compilándola).
- Un commit por tarea o grupo pequeño de tareas, con mensajes en español.
- Si una tarea obliga a cambiar una decisión de `CLAUDE.md`, pregunta antes.

---

## Fase 0 · Puesta a punto del repositorio

**Objetivo:** dejar el repo listo para publicarse y para recibir contribuciones.

Tareas:

1. ~~Licencia~~: hecho, MIT con `LICENSE` (Ivan Jaramillo).
2. ~~Ruta del módulo~~: hecho, `github.com/pkarc/haro` en `go.mod`, imports y `proto` (`go_package`).
3. **CI en GitHub Actions** (`.github/workflows/ci.yml`):
   - `go vet ./...`, `go test ./...` y `gofmt -l .` (falla si hay archivos sin formatear) en Linux y macOS.
   - Job opcional que ejecuta `scripts/fetch-corpus.sh` y `go test ./core -run TestCorpus -v`, y publica la cobertura en el resumen del job.
   - Job de la web (`npm ci && npm run build`) en cuanto exista la fase 2.
4. **Validación real con HAProxy en CI**: instalar `haproxy` (paquete de Ubuntu o imagen `haproxy:lts`) y añadir un test de integración que pase cada `core/testdata/*.cfg` sin errores esperados por `haproxy -c`, para comprobar que los casos «válidos» lo son de verdad.
5. `CONTRIBUTING.md` breve: cómo añadir explicaciones (knowledge.json + caso en testdata + `make golden`) y reglas del detector.

Aceptación: CI en verde en un push; `LICENSE` presente; `go install <ruta>/cmd/haro@latest` funciona desde un repo de prueba.

---

## Fase 1 · Operaciones de edición en el núcleo (`apply`)

**Objetivo:** que todas las ediciones (modo visual, TUI, MCP, asistente) pasen por el núcleo y produzcan cambios mínimos en el texto.

Diseño:

- Nuevo archivo `core/edit.go` con un tipo `Op` serializable a JSON:
  - `add_section {type, name, at?, template?}`
  - `remove_section {section}`
  - `move_section {section, to}`
  - `rename_section {section, name, update_refs: bool}` (actualiza `default_backend` / `use_backend` en todos los archivos)
  - `add_directive {section, text, at?}`
  - `update_directive {section, line_index, text}`
  - `remove_directive {section, line_index}`
  - `move_directive {section, line_index, delta}` (salta líneas en blanco)
- `func Apply(cfg *Config, ops ...Op) (*Config, []FileChange, error)`: aplica sobre la vista unificada y devuelve qué archivos cambiaron con su texto nuevo. Debe rechazar una línea que en realidad sea cabecera de sección en `add/update_directive`.
- Mantener el formato: líneas nuevas con `DefaultIndent`; `EnsureSeparation` solo en los archivos tocados.
- Plantillas de sección en `core/knowledge/templates.json` (las mismas que usa el prototipo en `TEMPLATES`).

Tareas:

1. Implementar `Op` y `Apply` con tests unitarios por operación (texto antes → texto después).
2. Caso de varios archivos: renombrar un backend definido en `30-api.cfg` actualiza la referencia en `20-web.cfg`.
3. Exponer `POST /api/v1/apply {files, ops}` en `haro serve` y añadirlo al `proto`.
4. Añadir `apply` a `CoreClient`.

Aceptación: todos los tests de edición pasan; `Serialize` de un archivo no tocado por la operación sigue idéntico byte a byte.

---

## Fase 2 · Interfaz web en React

**Objetivo:** portar `docs/prototype/haro-prototype.html` a `web/`, usando el núcleo en Go a través de `CoreClient` (nada de lógica de HAProxy en la web).

Dependencias sugeridas: `@codemirror/*` 6, `@uiw/react-codemirror` (o integración propia), `@xyflow/react` (React Flow), `zustand` (estado + persistencia en localStorage), Radix UI o shadcn/ui para diálogos y menús.

Tareas, en este orden:

1. **Estado y datos** (`zustand`): archivos (`SourceFile[]`), archivo activo, análisis más reciente, modo (código/visual), historial de deshacer/rehacer, versiones guardadas. Análisis con *debounce* de ~200 ms tras cada cambio.
2. **Modo código** con CodeMirror 6:
   - Lenguaje HAProxy propio (`StreamLanguage` o gramática Lezer simple) con los mismos colores que el prototipo (`hlLine` en el prototipo es la referencia).
   - Linter de CodeMirror alimentado por `analysis.issues` del archivo activo.
   - Autocompletado de directivas por tipo de sección (los `SUGGEST` del prototipo; mejor generarlos desde `knowledge.json`, expuesto por un endpoint `GET /api/v1/knowledge`).
   - Barra inferior con la explicación de la línea del cursor.
   - Pestañas por archivo cuando hay varios.
3. **Modo visual**: tarjetas por sección (tipo, nombre, título, descripción, modo efectivo y timeouts heredados), directivas con categoría, argumentos y explicación (renderizando el markdown mínimo). Edición en línea, añadir, mover, borrar, renombrar y añadir sección desde plantilla, **todo mediante `apply`** (fase 1).
4. **Mapa del tráfico** con React Flow: entradas → backends → servidores a partir de `analysis.topology`; aristas por defecto (continuas) y condicionales (discontinuas, con la condición en el tooltip); backend inexistente en rojo; clic en un nodo lleva a su sección.
5. **Panel de problemas**: lista con archivo:línea, ir a la línea, filtro por gravedad.
6. **Guardar** (si `capabilities.canWrite`): botón que llama a `putFile` y muestra la salida de `haproxy -c` si falla. Mostrar diff antes de guardar.
7. **Importar/exportar, historial de versiones y deshacer/rehacer** como en el prototipo.
8. **Diseño responsive**: pestañas inferiores en móvil (Código / Visual / Asistente) como en el prototipo.
9. Compilar con `make web` y verificar que `make build` embebe la web (`web/dist`).

Aceptación: `make run` abre la interfaz con el ejemplo; editar en modo visual cambia el texto de forma mínima; los problemas aparecen en el editor y en el panel; con `--file dir/ --allow-write` se pueden editar y guardar archivos distintos y el guardado se rechaza si rompe una referencia entre archivos. Probar en escritorio y en un viewport de móvil (Playwright).

---

## Fase 3 · Connect RPC

**Objetivo:** tipos compartidos y generados entre Go y TypeScript.

Tareas:

1. `buf.yaml` y `buf.gen.yaml` en la raíz; plugins `protoc-gen-go`, `protoc-gen-connect-go` y `@bufbuild/protoc-gen-es`.
2. `buf generate` → `gen/haro/v1` (Go) y `web/src/gen` (TS). Añadir `make proto`.
3. Implementar `CoreService` y `ConfigService` en Go reutilizando la lógica actual de `serve.go`. Mantener temporalmente las rutas JSON `/api/v1/*` si la web aún las usa, y retirarlas al terminar.
4. Sustituir `web/src/core/types.ts` y `client.ts` por el cliente generado detrás de la misma interfaz `CoreClient`.
5. Conservar `localOnly`, el límite de tamaño y las reglas de escritura.

Aceptación: la web funciona igual usando solo el cliente Connect; los tests de `serve` cubren las rutas Connect.

Nota: la conversión de structs del núcleo a mensajes proto debe hacerse en `cmd/haro` (o un paquete `internal/rpc`), nunca dentro de `core`, para no añadirle dependencias.

---

## Fase 4 · Interfaz de terminal con Bubble Tea

**Objetivo:** `haro` sin argumentos (o `haro tui [rutas]`) abre una interfaz interactiva.

Dependencias: `charmbracelet/bubbletea`, `lipgloss`, `bubbles` (list, viewport, help, key), `fsnotify` para recarga.

Diseño de pantalla:

```
┌ Secciones ───────────┬ Detalle ─────────────────────────────────────┐
│ global               │ backend api_servers  Grupo de servidores     │
│ defaults             │ modo http (heredado de defaults)             │
│ frontend http_in   ! │                                              │
│ frontend https_in    │  49 balance leastconn                        │
│ backend web_servers  │     [Servidores] Reparto: menos conexiones   │
│ backend api_servers  │  50 server api1 10.0.0.21:9000 check …       │
│ …                    │     [Servidores] Servidor api1 en …          │
├──────────────────────┴──────────────────────────────────────────────┤
│ 2 errores · 3 avisos   [tab] panel  [m] mapa  [e] editar  [?] ayuda │
└─────────────────────────────────────────────────────────────────────┘
```

Tareas:

1. Modelo con lista de secciones (agrupadas por archivo si hay varios) y panel de detalle con explicaciones (reutilizar el render de markdown mínimo de `cli.go`).
2. Vista de problemas (`p`): lista navegable; Enter salta a la sección y resalta la línea.
3. Vista de mapa (`m`): árbol de texto `frontend ──si cond──▶ backend ──▶ servidores`.
4. `e`: abre `$EDITOR` en el archivo y línea seleccionados (`+N` para vim/nano/emacs; `-g archivo:N` para VS Code) y reanaliza al volver.
5. Recarga automática con `fsnotify` cuando cambian los archivos.
6. Ayuda (`?`), colores adaptados a terminal clara u oscura (`lipgloss.AdaptiveColor`), soporte de `NO_COLOR`.
7. `haro` sin argumentos y sin terminal interactiva (por ejemplo, en un pipe) sigue mostrando la ayuda.

Aceptación: probado manualmente con el ejemplo, con `core/testdata/multi/` y con archivos del corpus; tests del modelo con `teatest` para la navegación básica.

---

## Fase 5 · Servidor MCP (`haro mcp`)

**Objetivo:** que Claude Code, Claude Desktop y otros clientes MCP usen el análisis determinista de haro.

Diseño:

- Transporte stdio con JSON-RPC 2.0. Se puede implementar con la biblioteca estándar o con el SDK oficial de Go para MCP (`github.com/modelcontextprotocol/go-sdk`), si su API es estable en ese momento; consultar la especificación vigente antes de empezar.
- Herramientas:
  - `haro_lint {paths?: string[], text?: string, disable?: string[]}` → problemas con archivo y línea.
  - `haro_explain {paths?: string[], text?: string, section?: string}` → explicaciones por sección.
  - `haro_analyze {paths?|text?}` → análisis completo (JSON).
  - `haro_diff {before, after}`.
  - `haro_check {paths?|text?}` → `haproxy -c` si está disponible.
  - `haro_apply {paths, ops, write?: bool}` (tras la fase 1): solo escribe si el servidor se lanzó con `--allow-write`, con las mismas garantías que `serve`.
- Recursos opcionales: `haro://knowledge` (base de conocimiento) y `haro://rules` (reglas del detector).
- Rutas: aceptar las que reciba, pero permitir restringirlas con `--root dir` (repetible).

Tareas:

1. Implementar `haro mcp` con las herramientas de solo lectura.
2. Tests: sesión JSON-RPC simulada por stdin/stdout (initialize, tools/list, tools/call).
3. Documentar en el README cómo registrarlo en Claude Code (`claude mcp add haro -- haro mcp`) y en Claude Desktop (bloque de configuración).
4. Opcional: una skill de Claude con buenas prácticas de HAProxy que use estas herramientas antes de proponer una configuración.

Aceptación: registrado en Claude Code, una petición como «revisa /etc/haproxy/haproxy.cfg» llama a `haro_lint` y `haro_explain` y la respuesta cita archivo y línea.

---

## Fase 6 · Distribución e integraciones

**Objetivo:** que instalar y usar haro sea trivial.

Tareas:

1. **GoReleaser** (`.goreleaser.yaml`): Linux, macOS (y Windows si no cuesta) en amd64 y arm64; checksums; `-ldflags "-X main.version=…"`; changelog desde los commits. Workflow que publica al crear una etiqueta `v*`.
2. **Homebrew tap** (`pkarc/homebrew-tap`) generado por GoReleaser.
3. **Imagen Docker** mínima (`FROM scratch` o `distroless`) con el binario; ejemplo `docker run -v /etc/haproxy:/cfg:ro haro lint /cfg`.
4. **GitHub Action** (`pkarc/haro` (`action.yml`) o `action.yml` en el repo): ejecuta `haro lint --format sarif` y sube el resultado; comenta en el PR.
5. **Salida SARIF** en `haro lint --format sarif` (reglas con `Rules` como descripción).
6. **Hook de pre-commit** (`.pre-commit-hooks.yaml`).
7. Paquetes `.deb`/`.rpm` con nfpm (opcional).

Aceptación: `brew install pkarc/tap/haro`, `docker run …` y la Action funcionan en un repo de prueba.

---

## Fase 7 · Base de conocimiento e idiomas

**Objetivo:** explicar casi todas las directivas reales, en inglés y español.

Tareas:

1. Ejecutar el corpus y priorizar las directivas sin explicación por frecuencia.
2. Usar el catálogo de directivas de `haproxytech/client-native` (Apache-2.0) para completar `sections` y categorías; citar el origen en `knowledge.json` o en `NOTICE`.
3. **Idiomas**: pasar los textos de `knowledge.json` y de `describe.go` a claves con traducciones (`es`, `en`); `--lang` en el CLI, `LANG` por defecto, selector en la web. Las frases con argumentos de `describe.go` necesitan plantillas por idioma.
4. Versiones de HAProxy: campo `since` en las directivas y aviso si se usa algo más nuevo que la versión indicada con `--haproxy-version`.
5. Nuevas reglas del detector sugeridas por el corpus: exposición pública sin ACL de IPs, contraseña de estadísticas `test`, `stats` sin `auth` en un bind público, `option forwardfor` ausente en http, etc. Cada una con identificador, caso y golden.

Aceptación: cobertura del corpus por encima del 95 %; golden en inglés y español.

---

## Fase 8 · Plantillas, asistente, WASM y web pública

1. **Plantillas por tecnología** (`core/knowledge/templates/`) — **hecho (8.1)**: Proxmox VE, Home Assistant, Nextcloud, Jellyfin, Gitea, Grafana, Vaultwarden, PostgreSQL, Redis, Elasticsearch y Kubernetes API, con TLS y redirección las web. Un JSON por tecnología interpretado por `core.RenderTechTemplate` (devuelve operaciones para `Apply`); `CoreService.ListTemplates`/`RenderTemplate`, `haro template list|show|add`, herramientas MCP `haro_template_list`/`haro_template_render` y diálogo en el modo visual de la web. Validadas con `haproxy -c -dW` (LTS 3.4 y 2.8). Pendiente: los defaults del usuario con directivas solo-HTTP (`option forwardfor`) hacen que HAProxy avise en las secciones TCP que los heredan (habría que usar `defaults` con nombre y `from`).
2. **Asistente** — **hecho (8.2)**: núcleo (`core/assist.go`: contexto y extracción de la propuesta), servidor (`AssistService.Assist` en streaming, `haro serve --assistant`, `haro assist`), chat en la web (`web/src/assistant/`, tarjeta de propuesta con diff, aplicar con versión previa y deshacer, e2e con API falsa) y panel en la TUI (tecla `a`, `haro tui --assistant`). Solo `haro serve`/CLI/TUI llaman a la API de Anthropic; la web sin servidor no. Detalle original:
   - Construcción del contexto en el núcleo (`core.BuildPrompt(cfg, issues, request)`) y extracción de la configuración propuesta de la respuesta.
   - Llamada a la API de Anthropic en `haro serve` (clave en `ANTHROPIC_API_KEY`, nunca en el navegador) y en la TUI.
   - En la web: chat con streaming, diff antes de aplicar y versión guardada antes de aplicar (como en el prototipo). El prompt del prototipo (`SYSTEM_PROMPT`) es un buen punto de partida.
3. **WASM**: `wasm/main.go` con `syscall/js` exponiendo `analyze`, `apply`, `diff`; probar con Go estándar y con TinyGo y medir tamaño; `WasmCoreClient` en la web.
4. **Web pública** (repo aparte): la misma web con `WasmCoreClient`, sesión en localStorage, asistente con clave propia del usuario o con un intermediario con límites.

---

## Referencias

- Prototipo: `docs/prototype/haro-prototype.html` (interfaz completa, lógica en JS como referencia de comportamiento).
- Política de marca de HAProxy: usar «HAProxy» solo de forma descriptiva y con aviso de no afiliación.
- Carga de varios archivos: documentación de `haproxy -f` (orden de argumentos; directorios con `.cfg` en orden lexicográfico, sin ocultos ni subdirectorios).
- Corpus: https://github.com/HariSekhon/HAProxy-configs (MIT).
- client-native: https://github.com/haproxytech/client-native (Apache-2.0).
