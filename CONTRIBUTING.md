# Contribuir a haro

Gracias por querer ayudar. Lee antes `CLAUDE.md`: recoge las decisiones de diseño ya tomadas (núcleo en Go sin dependencias, parser sin pérdidas, seguridad de `haro serve`…).

## Preparar el entorno

```sh
make test      # go test ./...
make build     # binario en bin/haro
```

Para la web hace falta Node: `make web`. Los e2e (`cd web && PW_CHANNEL=chrome npx playwright test`, tras `make web && make build` y `npm run build:wasm`) incluyen el asistente contra una API de Anthropic falsa (`web/e2e/fake-anthropic.mjs`): nunca llames a la API real en pruebas.

## Añadir o mejorar explicaciones

Las explicaciones existen en español e inglés. Un cambio completo toca:

1. **Estructura** en `core/knowledge/knowledge.json`: secciones donde es válida la directiva (`sections`), categoría (`category`), colores, `booleanFetches`, `builtinAcls`…
2. **Texto** en `core/knowledge/text.es.json` y `core/knowledge/text.en.json`, con exactamente las mismas claves y ningún valor vacío (`TestKnowledgeTextsSameShape` lo comprueba). El inglés debe sonar natural y técnico, no ser una traducción literal.
3. **Frases con argumentos** en `core/describe.go`, escritas completas en los dos idiomas con el contexto `x tr`: `x.t("Cambia la ruta a ", "Sets the path to ")`. Cuando el orden de las palabras cambia, usa dos plantillas completas con `x.f(plantillaES, plantillaEN, args...)` en lugar de concatenar. El idioma viaja siempre en `x`; nunca lo guardes en una variable global (el servidor atiende varios idiomas a la vez).
4. Un caso en `core/testdata/` (`*.cfg`; un subdirectorio es un caso de varios archivos). `coverage.cfg` recorre casi todas las frases.
5. `make golden`, y **revisa el diff** de los `.golden.json` (español) y de los `.golden.en.json` (inglés). Los golden en español no deberían cambiar si no tocaste el español. Lee los de inglés a mano: `TestNoLanguageLeftovers` caza restos de español, pero no sustituye a una lectura.

Las explicaciones usan markdown mínimo: `**negrita**` y `` `código` ``. No metas HTML.

Los errores de edición (`core/edit.go`, `ApplyError`) y los textos de interfaz (CLI, TUI, web) no se traducen.

## Añadir una plantilla de tecnología

Las plantillas por tecnología (`haro template`, Fase 8.1) son datos: un JSON por tecnología en `core/knowledge/templates/<id>.json` (el nombre del archivo es el `id`), que `core/techtemplates.go` interpreta y devuelve como operaciones de edición (`add_section` + `add_directive`) para `core.Apply`. No se escribe código para añadir una.

```json
{
  "id": "miapp", "kind": "web",
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

- `kind` `web`: el motor añade los parámetros `domain` y `cert` y genera los frontends (puerto 80 con redirección a HTTPS y puerto 443 con `ssl crt … alpn h2,http/1.1`) apuntando al backend sin `suffix`; si ya hay un frontend TLS en el 443 se une a él con una ACL por dominio. El JSON solo describe el backend y lo extra, y **toda la lógica específica de la tecnología va en el backend** (cabeceras, redirecciones, `timeout tunnel`, comprobaciones), porque es lo único que se puede aplicar igual cuando el frontend se comparte. `kind` `tcp`: tú describes todas las secciones (frontend con `suffix` `_in` y backend). Un frontend y un backend no pueden llamarse igual (HAProxy 3.3+).
- Tipos de parámetro: `string` (validadores opcionales `validate`: `domain`, `time`, `bind`; por defecto, un token sin espacios, comillas, `#` ni llaves), `int` (puerto 1–65535), `bool` y `list` (elementos `host:puerto`). `required` y `default` son excluyentes; `dependsOn` apunta a un `bool` declarado antes y oculta el parámetro (en la web) e ignora su valor si está desactivado.
- Líneas: una cadena, o un objeto con `text`, `if` (`clave`, `!clave` o varias con `&`; además de los parámetros, `logging` indica que hay destino de logs, para fijar `option tcplog`), `each` (+ `where`: `first`/`rest`) o `comment`. Marcadores: `{{clave}}`, `{{base}}` (nombre sin colisiones) y, dentro de `each`, `{{item}}`, `{{host}}`, `{{port}}`, `{{n}}`. Todo texto visible lleva `es` y `en`. El motor valida la plantilla al cargar (un marcador desconocido hace fallar el arranque y los tests).
- No pongas `bind` públicos (`*`) a servicios internos (bases de datos, cachés): pide `bind` como parámetro obligatorio, para que el detector (`internal-port-public`) no avise. No uses directivas que falten en la LTS más antigua soportada (2.8): si hace falta una más nueva, ponla tras un parámetro opt-in.
- Pruebas: añade el caso a `typicalParams` en `core/techtemplates_test.go` (parámetros típicos; el resultado debe pasar el detector **sin ningún aviso ni sugerencia**), ejecuta `go test ./core -run TechTemplates -update` y **revisa** los `core/testdata/templates/<id>.golden.cfg` y `.golden.en.cfg`. `TestHAProxyAcceptsTechTemplates` (ver «Validar con HAProxy») comprueba que `haproxy -c -dW` la acepta sin avisos.
- Los textos de las plantillas son propios; si te inspiras en una configuración publicada, cítala en el mensaje del commit.

## Reglas del detector

- Cada regla tiene un identificador estable en `core.Rules`. No lo renombres: lo usan `--disable`, la salida JSON y la integración continua de los usuarios.
- Toda regla nueva lleva su caso en `core/testdata/` y sus golden, en español y en inglés: descripción en `rulesES` y `rulesEN` (`core/lint.go`) y el mensaje en los dos idiomas en la llamada a `add`.
- El detector es orientativo. La validación definitiva es `haproxy -c`.

## Validar con HAProxy

Los tests `HAProxy*` de `core` contrastan los casos con `haproxy -c`. Se omiten si no hay binario. Para ejecutarlos:

```sh
HARO_HAPROXY=/ruta/a/haproxy go test ./core -run HAProxy -v
# o con Docker, con un envoltorio que monte /tmp:
#   exec docker run --rm -v /tmp:/tmp:ro haproxy:lts haproxy "$@"
```

`HARO_REQUIRE_HAPROXY=1` hace que fallen en vez de omitirse (así corren en CI).

## Corpus de configuraciones reales

```sh
scripts/fetch-corpus.sh && go test ./core -run TestCorpus -v
```

Sirve para comprobar que el parser no pierde nada y para ver qué directivas aún no se explican.

## Estilo

- Código y nombres en inglés; comentarios, mensajes y documentación en español.
- `gofmt` y `go vet` limpios. El núcleo (`core/`) no puede depender de paquetes externos: debe compilar a WASM.
- Un commit por cambio lógico, con mensaje en español.

## Versiones de HAProxy (`since`)

Una directiva puede llevar `"since": "2.4"` en `knowledge.json`: la versión de HAProxy en que apareció. Con `--haproxy-version`, el detector avisa (regla `newer-than-target`) de lo que requiera una versión posterior. Pon `since` solo si estás seguro de la versión; si dudas, déjalo vacío.
