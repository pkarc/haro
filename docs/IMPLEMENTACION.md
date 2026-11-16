# Resumen de implementación

Estado de cada fase de [`PLAN.md`](PLAN.md): qué se hizo, qué huecos (gaps) se encontraron al revisarla y cómo se resolvieron, y lo que sigue pendiente. Todo está verificado con tests; lo que no pudo probarse en esta máquina se dice expresamente.

## Fase 0 · Puesta a punto
**Hecho:** licencia MIT, módulo `github.com/pkarc/haro`, CI (Go en Linux/macOS, `haproxy -c`, corpus, web, e2e, WASM, buf, Docker), `CONTRIBUTING.md`, tests de integración contra `haproxy -c` real.

| Gap | Solución |
|---|---|
| `testdata/multi` no es válido para HAProxy (nombre repetido y directiva fuera de sección, a propósito) | Caso nuevo `multiok/` válido; el test comprueba además que HAProxy **rechaza** `multi` |
| `haproxy` en Docker no podía leer directorios temporales 0700 | Directorios temporales legibles y contenedor con el UID del usuario |
| `go install …@latest` falla hasta publicar el repo | Pendiente de hacer push; con `go install ./cmd/haro` funciona |

## Fase 1 · Edición (`core.Apply`)
**Hecho:** 8 operaciones (`add/remove/move/rename_section`, `add/update/remove/move_directive`), plantillas de sección, `POST apply`, unión de varios archivos, cambios mínimos y archivos no tocados byte a byte.

| Gap | Solución |
|---|---|
| Los comentarios pegados a una cabecera quedaban huérfanos al mover o borrar | Descomposición temporal en bloques (comentario + sección) |
| Un `append` sobre sub-slices pisaba líneas | Slices con capacidad acotada |
| Quitar una sección añadida no devolvía el texto original | Tests de operaciones inversas sobre `testdata` y el corpus; casos límite documentados |
| Las plantillas podían no ser válidas | Test de cada plantilla contra `haproxy -c` |

## Fase 2 · Web en React
**Hecho:** modo código (CodeMirror 6), modo visual por `apply`, mapa (React Flow), problemas, guardar con diff, importar/exportar, historial, deshacer, responsive; 15→44 tests Vitest y e2e Playwright.

| Gap | Solución |
|---|---|
| Un cambio que toca dos archivos (renombrar un backend con referencias) no se podía guardar: cada archivo por separado dejaba una referencia rota | `PutConfig` acepta `files[]`: se valida el conjunto y se guarda **todo o nada**, con rollback |
| `make build` embebía una web antigua si no se corría `make web` | Documentado y usado en todas las verificaciones |
| `haproxy -c` desde la UI sin probar | Verificado contra `haproxy:lts` real vía la API |
| Pendiente | Autocompletado solo de la primera palabra; bundle de ~940 kB sin code-splitting |

## Fase 3 · Connect RPC
**Hecho:** `buf` genera Go (`gen/`) y TypeScript (`web/src/gen`); `CoreService`, `ConfigService` (y luego `AssistService`); la web usa el cliente generado detrás de `CoreClient`; rutas JSON `/api/v1` retiradas.

| Gap | Solución |
|---|---|
| Deriva entre el núcleo y el contrato | Conversión estricta núcleo→proto y `TestRPCContract` sobre todos los casos y el corpus |
| Una página externa podría hacer POST «simples» | Connect solo acepta sus tipos (415 con `text/plain`); test específico |
| `gofmt` sin pasar en `rpc.go`/`serve.go` (rompería el CI) | Corregido; el CI lo comprueba |
| Código generado desactualizado | Job `proto` en CI que falla si `make proto` cambia algo |
| `placeholder.html` seguía con la ruta antigua | Actualizado |

## Fase 4 · TUI (Bubble Tea)
**Hecho:** secciones con detalle, problemas con filtro y salto a la línea, mapa en árbol, `e` abre `$EDITOR` en la línea exacta, recarga con `fsnotify`, ayuda, colores adaptativos y `NO_COLOR`, `haro` sin terminal muestra la ayuda.

| Gap | Solución |
|---|---|
| La ventana de scroll no se recordaba (saltaba al subir) | Offsets persistentes + test |
| Paneles más altos que la pantalla | Alto de contenido calculado (bordes, título, barra) |
| Robustez con configuraciones reales | Test que recorre el corpus a varios anchos |
| Pruebas en tmux podían afectar sesiones ajenas | Socket propio (`tmux -L`); nunca `kill-server` |

## Fase 5 · Servidor MCP
**Hecho:** `haro mcp` (stdio, sin dependencias): `haro_lint/explain/analyze/diff/check/apply`, recursos `haro://knowledge` y `haro://rules`, skill de Claude. **Verificado con Claude Code real** (citó `problems.cfg:13`).

| Gap | Solución |
|---|---|
| La especificación cambió: existe una era moderna (2026-07-28, sin sesión) y la legada con `initialize` | Servidor «dual-era» (el cliente decide); `server/discover` y error de versión no soportada |
| Lectura/escritura de rutas arbitrarias | `--root` repetible con enlaces simbólicos resueltos y sin trucos de prefijo; `haro_apply` solo escribe con `--allow-write` y las garantías de `serve` |
| Inyección desde comentarios de la configuración | Las instrucciones del servidor tratan el contenido como dato no confiable |
| Límites conocidos | Peticiones en serie (la cancelación se ignora); `haro_analyze` puede ser grande |

## Fase 6 · Distribución
**Hecho:** GoReleaser (6 binarios, `.deb`, `.rpm`, fórmula Homebrew, checksums; snapshot verificado), imagen Docker de 22 MB (probada), GitHub Action, salida SARIF (validada contra el esquema oficial) y markdown, pre-commit.

| Gap | Solución |
|---|---|
| `brews` está obsoleto en GoReleaser v2 | Se mantiene a propósito: el reemplazo (casks) es solo macOS y se pierde Linuxbrew; comentado en el archivo |
| `go install` compila sin la web (`web/dist` no se versiona) | Documentado; releases, Homebrew y Docker sí la llevan |
| La Action no se puede ejecutar fuera de GitHub | La lógica de shell se probó localmente en los casos error/warning/never; el flujo completo queda por probar al publicar |
| Pendiente (requiere tu cuenta) | Crear `pkarc/homebrew-tap` y el secreto `HOMEBREW_TAP_GITHUB_TOKEN`; publicar la primera etiqueta |

## Fase 7 · Conocimiento e idiomas
**Hecho:** español e inglés en explicaciones y detector (`--lang`, `lang` en la API, selector en la web, MCP y TUI); catálogo de 324 directivas (derivado de `config-parser`, Apache-2.0, con `NOTICE`); **100 % de cobertura del corpus** (antes 98,8 %); `since` y `--haproxy-version` (regla `newer-than-target`); 7 reglas nuevas del detector.

| Gap | Solución |
|---|---|
| El idioma no puede ser una variable global (peticiones concurrentes) | Contexto `tr{lang}` que viaja en la llamada; test con `-race` |
| Textos en inglés heredaron defectos del español («1 days», «the path is greater than 100», `tcp-request` sin fase) | Corregidos en los dos idiomas con tests |
| El agente de la segunda mitad se detuvo a mitad de trabajo | Se revisó lo hecho y se terminó a mano (`NOTICE`, docs, verificación) |
| Cobertura de `since` | Solo 31 directivas, las seguras; el resto sin `since` a propósito |
| Pendiente | Errores de `Apply`, mensajes de rechazo al guardar y textos de interfaz (web, TUI, CLI) siguen en español |

## Fase 8 · Plantillas, asistente, WASM
**Hecho:**
- **Plantillas por tecnología:** 11 (Proxmox, Home Assistant, Nextcloud, Jellyfin, Gitea, Grafana, Vaultwarden, Elasticsearch, PostgreSQL, Redis, Kubernetes) en el núcleo, API, CLI, MCP y web; validadas con `haproxy -c -dW` (2.8 y LTS).
- **WASM:** `make wasm` (6,2 MB; 1,6 MB gzip) y `make wasm-tiny` (TinyGo, 1,8 MB; 620 KB gzip); `WasmCoreClient`; la web funciona sin servidor con `VITE_CORE=wasm` y un e2e comprueba que no hace ninguna llamada a la API.
- **Asistente:** contexto y extracción de propuesta en el núcleo; `AssistService` en streaming; `haro assist`; chat en la web con tarjeta de propuesta (diff, aplicar con versión previa, deshacer); panel en la TUI. Opt-in con `--assistant`; la clave nunca llega al navegador.

| Gap | Solución |
|---|---|
| Una respuesta cortada por `max_tokens` podía proponer un archivo a medias | Los bloques sin cerrar se descartan (`truncated`) |
| La propuesta perdía el salto de línea final | Se iguala al del archivo actual |
| Una segunda plantilla web abriría otro `bind *:443` | Se une al frontend 443 existente con ACL por host |
| El asistente no se pudo probar contra la API real (sin clave) | API falsa SSE en tests y e2e; error 401 verificado; **por probar con tu clave** |
| `refusal fallbacks` | No se activó: requiere un endpoint beta que no pude comprobar; el usuario ve la categoría del rechazo |
| Pendiente | `ConfigStore`/`GitHubStore` y el Worker de OAuth (ver más abajo) |

## Web pública (pendiente de decisión tuya)
Acordado: repo `haro-site` aparte, abierto y gratuito, que incluye este repo como submódulo git (`haro/`) y `haro/web` como workspace de npm (sin paquete npm publicado); GitHub App con permiso de contenidos solo en los repos elegidos, Worker de Cloudflare solo para el intercambio de OAuth, token en el navegador, escritura a rama o PR a elección del usuario, gratis. Falta: registrar la GitHub App en tu cuenta y crear el repo `haro-site` con el `GitHubStore`, el Worker y el inicio de sesión. En este repo, los puntos de extensión que lo hacen posible (`ConfigStore` con `LocalStore`, `mountApp`, exportación de `web/src`) los prepara la sesión «haro-site». El modo «cuenta propia» con cifrado de extremo a extremo queda como candidato a funcionalidad premium.
