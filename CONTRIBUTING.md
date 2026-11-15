# Contribuir a haro

Gracias por querer ayudar. Lee antes `CLAUDE.md`: recoge las decisiones de diseño ya tomadas (núcleo en Go sin dependencias, parser sin pérdidas, seguridad de `haro serve`…).

## Preparar el entorno

```sh
make test      # go test ./...
make build     # binario en bin/haro
```

Para la web hace falta Node: `make web`.

## Añadir o mejorar explicaciones

Las explicaciones existen en español e inglés. Un cambio completo toca:

1. **Estructura** en `core/knowledge/knowledge.json`: secciones donde es válida la directiva (`sections`), categoría (`category`), colores, `booleanFetches`, `builtinAcls`…
2. **Texto** en `core/knowledge/text.es.json` y `core/knowledge/text.en.json`, con exactamente las mismas claves y ningún valor vacío (`TestKnowledgeTextsSameShape` lo comprueba). El inglés debe sonar natural y técnico, no ser una traducción literal.
3. **Frases con argumentos** en `core/describe.go`, escritas completas en los dos idiomas con el contexto `x tr`: `x.t("Cambia la ruta a ", "Sets the path to ")`. Cuando el orden de las palabras cambia, usa dos plantillas completas con `x.f(plantillaES, plantillaEN, args...)` en lugar de concatenar. El idioma viaja siempre en `x`; nunca lo guardes en una variable global (el servidor atiende varios idiomas a la vez).
4. Un caso en `core/testdata/` (`*.cfg`; un subdirectorio es un caso de varios archivos). `coverage.cfg` recorre casi todas las frases.
5. `make golden`, y **revisa el diff** de los `.golden.json` (español) y de los `.golden.en.json` (inglés). Los golden en español no deberían cambiar si no tocaste el español. Lee los de inglés a mano: `TestNoLanguageLeftovers` caza restos de español, pero no sustituye a una lectura.

Las explicaciones usan markdown mínimo: `**negrita**` y `` `código` ``. No metas HTML.

Los errores de edición (`core/edit.go`, `ApplyError`) y los textos de interfaz (CLI, TUI, web) no se traducen.

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
