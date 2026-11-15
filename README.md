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
haro lint --rules                       # lista de reglas
haro explain mi.cfg
haro explain --lang en mi.cfg           # explicaciones en inglés (también en lint, tui y mcp)
cat mi.cfg | haro explain -
haro diff antes.cfg despues.cfg
haro serve --file /etc/haproxy/haproxy.cfg               # solo lectura
haro serve --file /etc/haproxy/haproxy.cfg --allow-write # permite guardar
haro serve --file haproxy.cfg --file conf.d/             # varios archivos
```

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

`haro mcp` expone el análisis de haro como herramientas MCP: `haro_lint`, `haro_explain`, `haro_analyze`, `haro_diff`, `haro_check` (`haproxy -c`) y `haro_apply` (ediciones estructurales), más los recursos `haro://knowledge` y `haro://rules`. Habla las dos versiones del protocolo (el handshake `initialize` de 2025-11-25 y anteriores, y la versión sin sesión de 2026-07-28).

```sh
# Claude Code
claude mcp add haro -- haro mcp --root /etc/haproxy
```

```jsonc
// Claude Desktop: claude_desktop_config.json
{ "mcpServers": { "haro": { "command": "haro", "args": ["mcp", "--root", "/etc/haproxy"] } } }
```

Seguridad: con `--root` (repetible) solo se leen y escriben rutas dentro de esos directorios (se resuelven los enlaces simbólicos). Sin `--root`, el cliente puede leer cualquier ruta que pueda leer el proceso. `haro_apply` solo escribe con `--allow-write` y `write: true`, con las mismas garantías que `haro serve`: valida la configuración completa, guarda todo o nada y deja copia `.bak`. En [`skills/haro`](skills/haro/SKILL.md) hay una skill de Claude con el flujo recomendado.

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
- [ ] Plantillas de backend por tecnología (PostgreSQL, Redis, Elasticsearch…)
- [x] Servidor y cliente Connect generados desde `proto/` con buf (`make proto`)
- [x] Web: modo código (CodeMirror 6), modo visual, mapa (React Flow) y panel de problemas
- [ ] Web: asistente
- [x] Interfaz de terminal con Bubble Tea (`haro` sin argumentos)
- [x] Operaciones de edición en el núcleo (`apply`)
- [x] Servidor MCP (`haro mcp`)
- [ ] Asistente: construcción de contexto en el núcleo, llamada a la API en cada cliente
- [ ] Módulo WASM y web pública
