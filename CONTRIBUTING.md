# Contribuir a haro

Gracias por querer ayudar. Lee antes `CLAUDE.md`: recoge las decisiones de diseño ya tomadas (núcleo en Go sin dependencias, parser sin pérdidas, seguridad de `haro serve`…).

## Preparar el entorno

```sh
make test      # go test ./...
make build     # binario en bin/haro
```

Para la web hace falta Node: `make web`.

## Añadir o mejorar explicaciones

1. Edita `core/knowledge/knowledge.json` (secciones, directivas, opciones, fetches…) o, si la frase depende de los argumentos, `core/describe.go`.
2. Añade o amplía un caso en `core/testdata/` (`*.cfg`; un subdirectorio es un caso de varios archivos).
3. Ejecuta `make golden`, **revisa el diff** de los `.golden.json` y súbelo con el cambio.

Las explicaciones usan markdown mínimo: `**negrita**` y `` `código` ``. No metas HTML.

## Reglas del detector

- Cada regla tiene un identificador estable en `core.Rules`. No lo renombres: lo usan `--disable`, la salida JSON y la integración continua de los usuarios.
- Toda regla nueva lleva su caso en `core/testdata/` y su golden.
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
