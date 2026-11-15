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
- **`haro serve`**: abre una interfaz web local con modo código, modo visual y mapa del tráfico.

## Instalación

```sh
go install github.com/haro/haro/cmd/haro@latest
```

## Uso

```sh
haro lint                               # /etc/haproxy/haproxy.cfg por defecto
haro lint --format json mi.cfg          # salida para CI
haro lint --disable no-global,tls-no-alpn mi.cfg
haro lint --rules                       # lista de reglas
haro explain mi.cfg
cat mi.cfg | haro explain -
haro diff antes.cfg despues.cfg
haro serve --file /etc/haproxy/haproxy.cfg               # solo lectura
haro serve --file /etc/haproxy/haproxy.cfg --allow-write # permite guardar
```

`haro serve` escucha solo en `127.0.0.1:7468` y rechaza peticiones con un `Host` externo. Para guardar hace falta `--allow-write`. Antes de escribir, haro valida con `haproxy -c` si el binario está disponible; si no lo está, usa su propio detector y se niega a guardar si encuentra errores. Siempre deja una copia `.bak` de la versión anterior.

## Arquitectura

```
core/        Núcleo en Go: parser sin pérdidas, serializador, base de
             conocimiento, explicaciones, detector, herencia, topología, diff
core/knowledge/knowledge.json   Explicaciones y reglas como datos
core/testdata/                  Casos .cfg con su resultado esperado (.golden.json)
cmd/haro/    CLI y servidor local (serve)
proto/       Contrato del API (Connect), fuente de verdad de los tipos
web/         Interfaz React + Vite + TypeScript, embebida en el binario
```

- El núcleo no depende de ninguna interfaz. Lo usan el CLI y `haro serve`, y en el futuro lo usará un módulo WASM para la web pública.
- Las explicaciones usan un markdown mínimo (`**negrita**`, `` `código` ``) que la terminal y la web pintan a su manera.
- `core/testdata` es la especificación del proyecto: cada `.cfg` tiene su `.golden.json` con problemas, explicaciones y topología esperados.

## Desarrollo

```sh
make test     # tests
make golden   # regenera los .golden.json tras un cambio intencionado
make web      # compila la interfaz en web/dist (requiere Node)
make build    # binario en bin/haro, con la web embebida
make run      # serve con la configuración de ejemplo
```

Para trabajar en la web con recarga en caliente: `go run ./cmd/haro serve` en una terminal y `cd web && npm run dev` en otra. Vite redirige `/api` al servidor local.

### Añadir o corregir explicaciones

1. Edita `core/knowledge/knowledge.json` (texto) o `core/describe.go` (frases con argumentos).
2. Añade o amplía un caso en `core/testdata/`.
3. `make golden` y revisa el diff de los `.golden.json`.

## Hoja de ruta

- [x] Núcleo en Go con casos golden
- [x] CLI: `lint`, `explain`, `diff`, `serve`
- [x] API local JSON con guardado seguro
- [ ] Generar servidor y cliente Connect desde `proto/` con buf
- [ ] Web: portar el prototipo (CodeMirror 6, modo visual, React Flow, asistente)
- [ ] Interfaz de terminal con Bubble Tea (`haro` sin argumentos)
- [ ] Operaciones de edición en el núcleo (`apply`)
- [ ] Asistente: construcción de contexto en el núcleo, llamada a la API en cada cliente
- [ ] Módulo WASM y web pública
