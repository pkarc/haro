# haro

**haro** (*HAProxy readout*) lee, explica y valida configuraciones de HAProxy.

```
$ haro explain --section api_servers
backend api_servers  Grupo de servidores
  modo http (heredado de defaults)

    49 balance leastconn
       [Servidores] Reparto de carga: al servidor con menos conexiones activas (ideal para sesiones largas)
    50 server api1 10.0.0.21:9000 check inter 2s fall 3 rise 2
       [Servidores] Servidor api1 en 10.0.0.21:9000 · health checks activos · check cada 2 s · …
```

> haro es un proyecto independiente. No está afiliado ni respaldado por HAProxy Technologies. HAProxy® es una marca registrada de HAProxy Technologies.

## Qué hace

- **`haro explain`**: explica cada sección y directiva en lenguaje humano, incluida la herencia de `defaults`.
- **`haro lint`**: detecta problemas (backends inexistentes, ACLs sin definir, timeouts que faltan, modos http/tcp incompatibles…). Devuelve código 1 si hay errores, así que sirve en CI.
- **`haro diff`**: compara dos configuraciones.
- **`haro` / `haro tui`**: interfaz de terminal: secciones con sus explicaciones, problemas navegables (`p`), mapa del tráfico (`m`), edición en tu `$EDITOR` en la línea exacta (`e`) y recarga automática al cambiar los archivos. Sin terminal interactivo (un pipe) `haro` muestra la ayuda.
- **`haro mcp`**: servidor MCP por stdio para Claude Code, Claude Desktop y otros clientes (ver más abajo).
- **`haro serve`**: abre una interfaz web local con modo código, modo visual y mapa del tráfico.

## Instalación

```sh
go install github.com/pkarc/haro/cmd/haro@latest
```

`go install` compila sin la web (la interfaz de `haro serve` se reduce a una página provisional, porque `web/dist` no se versiona). Los binarios de las releases, Homebrew y la imagen de Docker sí la incluyen. Para compilar con la web desde el código: `make web build`.

## Uso

```sh
haro                                    # interfaz de terminal (en un terminal)
haro tui haproxy.cfg conf.d/            # con rutas, como haproxy -f
haro lint                               # /etc/haproxy/haproxy.cfg por defecto
haro lint haproxy.cfg conf.d/           # varios archivos, como haproxy -f
haro lint --format json mi.cfg          # salida para CI (también sarif y markdown)
haro lint --disable no-global,tls-no-alpn mi.cfg
haro lint --haproxy-version 2.8 mi.cfg   # avisa de directivas que requieren una versión posterior (newer-than-target)
haro lint --rules                       # lista de reglas
haro explain mi.cfg
haro explain --lang en mi.cfg           # explicaciones en inglés (también en lint, tui y mcp)
cat mi.cfg | haro explain -
haro diff antes.cfg despues.cfg
haro template list                      # plantillas por tecnología (Nextcloud, PostgreSQL…)
haro template show nextcloud            # sus parámetros
haro template add nextcloud --param domain=cloud.example.com --param servers=10.0.0.10:80 \
    --param cert=/etc/haproxy/certs/cloud.pem haproxy.cfg   # muestra el diff; con --write guarda
haro serve --file /etc/haproxy/haproxy.cfg               # solo lectura
haro serve --file /etc/haproxy/haproxy.cfg --allow-write # permite guardar
haro serve --file haproxy.cfg --file conf.d/             # varios archivos
haro serve --file haproxy.cfg --assistant                # con el asistente con IA (ver más abajo)
haro tui --assistant haproxy.cfg                         # TUI con el asistente (tecla a)
haro assist "Revisa mi configuración" haproxy.cfg        # una petición desde la línea de comandos
```

### Sin servidor: el núcleo en WebAssembly

El mismo núcleo compila a WebAssembly (`make wasm` → `web/public/wasm/`) y la web puede usarlo en vez de `haro serve`: `cd web && VITE_CORE=wasm npm run build`. La configuración nunca sale del navegador. No hay disco ni `haproxy -c` en ese modo (solo el detector). El módulo con Go estándar pesa unos 6 MB (1,6 MB con gzip); `make wasm-tiny` usa TinyGo en Docker y lo deja en unos 1,8 MB (620 KB con gzip). `node wasm/smoke.mjs [dir]` comprueba cualquiera de los dos.

### En CI y en tu repositorio

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
          paths: haproxy/            # archivos o directorios, como haproxy -f
```

La Action instala haro, ejecuta `haro lint`, sube el informe SARIF a *code scanning* y deja (y actualiza) un comentario en el pull request. Salidas de `haro lint`: `--format text|json|sarif|markdown`.

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/pkarc/haro
    rev: v0.1.0
    hooks:
      - id: haro-lint
```

```sh
brew install pkarc/tap/haro            # Homebrew (macOS y Linux)
docker run --rm -v /etc/haproxy:/cfg:ro ghcr.io/pkarc/haro lint /cfg
```

También hay binarios, `.deb` y `.rpm` en las [releases](https://github.com/pkarc/haro/releases). La imagen de Docker es mínima (sin `haproxy`): sirve para `lint`, `explain` y `mcp`; para validar con `haproxy -c` usa el binario junto a HAProxy.

### Servidor MCP

`haro mcp` expone el análisis de haro como herramientas MCP: `haro_lint`, `haro_explain`, `haro_analyze`, `haro_diff`, `haro_check` (`haproxy -c`), `haro_apply` (ediciones estructurales), `haro_template_list` y `haro_template_render` (plantillas por tecnología, solo simulan), más los recursos `haro://knowledge` y `haro://rules`. Habla las dos versiones del protocolo (el handshake `initialize` de 2025-11-25 y anteriores, y la versión sin sesión de 2026-07-28).

```sh
# Claude Code
claude mcp add haro -- haro mcp --root /etc/haproxy
```

```jsonc
// Claude Desktop: claude_desktop_config.json
{ "mcpServers": { "haro": { "command": "haro", "args": ["mcp", "--root", "/etc/haproxy"] } } }
```

Seguridad: con `--root` (repetible) solo se leen y escriben rutas dentro de esos directorios (se resuelven los enlaces simbólicos). Sin `--root`, el cliente puede leer cualquier ruta que pueda leer el proceso. `haro_apply` solo escribe con `--allow-write` y `write: true`, con las mismas garantías que `haro serve`: valida la configuración completa, guarda todo o nada y deja copia `.bak`. En [`skills/haro`](skills/haro/SKILL.md) hay una skill de Claude con el flujo recomendado.

### Plantillas por tecnología

Cada plantilla añade los frontends y backends necesarios para publicar un servicio detrás de HAProxy, con los ajustes que esa tecnología necesita: **Proxmox VE** (TLS al backend y WebSocket de noVNC), **Home Assistant** (WebSocket, `X-Forwarded-*`), **Nextcloud** (HSTS, `/.well-known/carddav|caldav`, timeout para subidas grandes), **Jellyfin**, **Gitea** (web y, opcional, SSH por TCP), **Grafana**, **Vaultwarden**, **PostgreSQL** (`pgsql-check`), **Redis** (`tcp-check` con PING y, opcional, solo el master), **Elasticsearch** (varios nodos, `http-check` a `/_cluster/health`) y **Kubernetes API** (TCP 6443, varios control planes). Las web llevan el `bind` del 80 con redirección a HTTPS y el del 443 con `ssl crt … alpn h2,http/1.1`.

```sh
haro template show proxmox              # parámetros, tipos y valores por defecto
haro template add nextcloud --param domain=cloud.example.com --param servers=10.0.0.10:80 \
    --param cert=/etc/haproxy/certs/cloud.pem /etc/haproxy/haproxy.cfg
haro template add postgresql --param bind=10.0.0.5:5432 --param servers=10.0.0.10:5432,10.0.0.11:5432 --write haproxy.cfg
```

`add` imprime el diff unificado y **no escribe**; con `--write` guarda con las mismas garantías que `haro serve` (valida la configuración completa con `haproxy -c`, escritura atómica y copia `.bak`). Las listas aceptan varios `--param` con la misma clave o valores separados por comas. La plantilla reutiliza el `global` y el `defaults` que ya existan (y solo añade los que falten), elige nombres sin colisiones (`nextcloud_2`…) y, si ya hay un frontend con TLS en el 443, se une a él con una ACL por dominio en vez de abrir otro `bind` en el mismo puerto. En la web, el botón «Añadir desde plantilla de tecnología» del modo visual abre un formulario con el diff antes de aceptar. Para Claude, `haro_template_list` y `haro_template_render` (simulan y devuelven el diff y las ops, que se aplican con `haro_apply`). En la API: `CoreService.ListTemplates` y `RenderTemplate` (los parámetros son un mapa de `google.protobuf.Value`).

### Asistente con IA

Chatea sobre tu configuración en lenguaje natural («añade un backend con tres servidores», «limita a 20 peticiones por segundo por IP», «qué mejorarías»). El asistente conoce la configuración vigente y sus problemas, responde en streaming y, si propone cambios, los muestra como **diff por archivo antes de aplicar nada**, con los errores y avisos que quedarían tras aplicarlos.

- **Es opt-in.** Sin `--assistant` no se llama a ningún servicio externo y no aparece nada en la interfaz (en la web, la pestaña móvil «Asistente» solo explica cómo activarlo).
- **Qué se envía.** La configuración completa (con sus contraseñas, direcciones y certificados referenciados por ruta) y la conversación se envían a la **API de Anthropic** desde el proceso local. La web y la TUI lo avisan de forma visible; la web pide confirmación la primera vez que se usa en cada sesión.
- **La clave** la lee el SDK de Anthropic en el servidor local: `ANTHROPIC_API_KEY` o un perfil de `ant auth login`. Nunca llega al navegador ni al historial del chat. `ANTHROPIC_BASE_URL` redirige las llamadas (útil para pruebas con una API falsa).
- **Modelo.** Por defecto `claude-opus-5-5`; cámbialo con `--assistant-model` o `HARO_ASSISTANT_MODEL`.
- **Web.** Con `haro serve --assistant`, el botón «✦ Asistente» abre el chat a un lado (en móvil, la pestaña inferior). El historial se guarda en el navegador junto al resto del estado; «Nueva conversación» lo borra. **Aplicar** guarda antes una versión en el historial («Antes de aplicar propuesta del asistente») y sustituye el texto de esos archivos como una única acción que se deshace con «Deshacer»; si la configuración cambió desde que se hizo la propuesta, pide confirmación. **Detener** corta la respuesta. Guardar en disco sigue siendo aparte y requiere `--allow-write`.
- **TUI.** Con `haro tui --assistant`, la tecla `a` abre una línea de petición; la respuesta llega en streaming, `Esc` la cancela (o vuelve a las secciones), y al terminar muestra el diff y «¿Aplicar y guardar? [y/N]». Con `y` guarda con las mismas garantías que `haro serve --allow-write` (valida la configuración completa, escritura atómica y copia `.bak`); si la validación rechaza la propuesta, muestra el motivo. La conversación dura lo que la sesión. Sin `--assistant`, `a` avisa de que está desactivado.
- **CLI.** `haro assist "petición" [archivos]` imprime la respuesta y el diff propuesto; con `--write` guarda (pide confirmación salvo `--yes`).
- **Límites.** Hasta 40 mensajes y 200 KB por conversación (la web envía los 20 últimos); respuestas de hasta 32 000 tokens (si se corta, lo indica); si la respuesta trae un bloque de configuración cortado o no asignable a un archivo, se descarta y se avisa. El asistente puede equivocarse: revisa el diff y valida con `haproxy -c`. Cada petición consume tu cuota de la API de Anthropic. La versión sin servidor (WebAssembly) no incluye el asistente, para que la clave nunca esté en el navegador.

### Configuraciones en varios archivos

haro carga las rutas igual que `haproxy -f`: en el orden indicado y, para cada directorio, sus archivos `.cfg` no ocultos en orden lexicográfico (sin entrar en subdirectorios). El análisis trata el conjunto como una sola configuración:

- cada archivo empieza fuera de cualquier sección, porque una sección no puede repartirse entre archivos;
- la herencia de `defaults`, las referencias a backends y los nombres duplicados sí cruzan archivos;
- cada problema indica su archivo y línea (`conf.d/20-web.cfg:4`).

Al guardar desde `haro serve`, se valida la configuración completa con el cambio aplicado, no solo el archivo editado. `ConfigService.PutConfig` acepta `files` (lista) para guardar varios archivos a la vez (todo o nada), necesario cuando un cambio, como renombrar un backend, toca dos archivos.

`haro serve` escucha solo en `127.0.0.1:7468` y rechaza peticiones con un `Host` externo. Para guardar hace falta `--allow-write`. Antes de escribir, haro valida con `haproxy -c` si el binario está disponible; si no lo está, usa su propio detector y se niega a guardar si encuentra errores. Siempre deja una copia `.bak` de la versión anterior.

## Arquitectura

```
core/        Núcleo en Go: parser sin pérdidas, serializador, base de
             conocimiento, explicaciones, detector, herencia, topología, diff
core/knowledge/knowledge.json   Estructura de la base de conocimiento (secciones, directivas…)
core/knowledge/text.es.json     Textos en español (misma forma y claves que text.en.json)
core/knowledge/text.en.json     Textos en inglés
core/testdata/                  Casos .cfg con su resultado esperado (.golden.json en español,
                                .golden.en.json en inglés); cada subdirectorio es un caso de varios archivos
scripts/fetch-corpus.sh         Descarga configuraciones reales para tests de no-regresión
cmd/haro/    CLI y servidor local (serve)
proto/       Contrato del API (Connect), fuente de verdad de los tipos
web/         Interfaz React + Vite + TypeScript, embebida en el binario
```

- El núcleo no depende de ninguna interfaz. Lo usan el CLI y `haro serve`, y en el futuro lo usará un módulo WASM para la web pública.
- Las explicaciones usan un markdown mínimo (`**negrita**`, `` `código` ``) que la terminal y la web pintan a su manera.
- `core/testdata` es la especificación del proyecto: cada `.cfg` tiene su `.golden.json` (español) y su `.golden.en.json` (inglés) con problemas, explicaciones y topología esperados.

### Idioma

Las explicaciones y los mensajes del detector existen en español e inglés. `haro lint`, `haro explain`, `haro tui` y `haro mcp` aceptan `--lang es|en`; por defecto se usa `LC_ALL`, `LC_MESSAGES` o `LANG` (inglés si empiezan por `en`, español en cualquier otro caso). `haro lint --rules` y los formatos SARIF y markdown salen en ese idioma. Las herramientas del servidor MCP aceptan además un argumento `lang`. En la API, `AnalyzeRequest` y `GetKnowledgeRequest` llevan un campo `lang` (vacío = español), y en la web el idioma se elige en Ajustes. Solo cambia el texto que produce el núcleo: los mensajes de los comandos y la interfaz siguen en español.

## Desarrollo

```sh
make test     # tests
make golden   # regenera los .golden.json tras un cambio intencionado
make web      # compila la interfaz en web/dist (requiere Node)
make proto    # regenera gen/ y web/src/gen desde proto/ (requiere buf)
make build    # binario en bin/haro, con la web embebida
make run      # serve con la configuración de ejemplo
```

Para trabajar en la web con recarga en caliente: `go run ./cmd/haro serve` en una terminal y `cd web && npm run dev` en otra. Vite redirige `/api` al servidor local.

### Editar desde código: `core.Apply`

Todas las interfaces editan con operaciones (`add_section`, `rename_section`, `move_directive`…) que `core.Apply` aplica sobre una copia y devuelve como los archivos que cambiaron. Los archivos que la operación no menciona vuelven byte a byte. Se exponen en `CoreService.Apply` (`{files, ops}`); guardar sigue siendo `ConfigService.PutConfig`. Las operaciones están documentadas en `core/edit.go`.

### Corpus de configuraciones reales

```sh
scripts/fetch-corpus.sh                 # descarga HariSekhon/HAProxy-configs (MIT) en core/testdata/corpus
go test ./core -run TestCorpus -v       # reescritura sin pérdidas + cobertura de la base de conocimiento
```

El corpus no se incluye en el repo. El test se omite si no está descargado.

### Añadir o corregir explicaciones

1. Estructura (a qué secciones y categoría pertenece una directiva…) en `core/knowledge/knowledge.json`.
2. Texto en **los dos** `core/knowledge/text.es.json` y `text.en.json`, con las mismas claves (un test lo comprueba).
3. Si la frase depende de los argumentos, `core/describe.go`, escrita completa en los dos idiomas (`x.t("español", "English")`, o `x.f(plantillaES, plantillaEN, ...)` si el orden de las palabras cambia).
4. Añade o amplía un caso en `core/testdata/`.
5. `make golden` y revisa el diff de los `.golden.json` y de los `.golden.en.json`.

## Hoja de ruta

El plan detallado está en [`docs/PLAN.md`](docs/PLAN.md).

- [x] Núcleo en Go con casos golden
- [x] CLI: `lint`, `explain`, `diff`, `serve`
- [x] API local (Connect RPC) con guardado seguro
- [x] Configuraciones en varios archivos y directorios
- [x] Test opcional con corpus de configuraciones reales
- [ ] Ampliar la base de conocimiento con el catálogo de client-native (Apache-2.0)
- [x] Plantillas por tecnología (Proxmox, Nextcloud, PostgreSQL, Redis, Elasticsearch, Kubernetes…)
- [x] Servidor y cliente Connect generados desde `proto/` con buf (`make proto`)
- [x] Web: modo código (CodeMirror 6), modo visual, mapa (React Flow) y panel de problemas
- [x] Web y TUI: asistente con IA (`--assistant`)
- [x] Interfaz de terminal con Bubble Tea (`haro` sin argumentos)
- [x] Operaciones de edición en el núcleo (`apply`)
- [x] Servidor MCP (`haro mcp`)
- [x] Asistente: contexto y propuesta en el núcleo, llamada a la API en el servidor local
- [x] Núcleo en WebAssembly (`make wasm`, `WasmCoreClient`)
- [ ] Web pública (repo aparte)

## Licencia y créditos

MIT (ver [`LICENSE`](LICENSE)). El catálogo de nombres de directivas se derivó de `haproxytech/config-parser` (Apache-2.0) y el corpus opcional de pruebas es `HariSekhon/HAProxy-configs` (MIT); detalles en [`NOTICE`](NOTICE).
