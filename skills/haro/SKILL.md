---
name: haro
description: Revisa, explica y edita configuraciones de HAProxy (haproxy.cfg) usando las herramientas deterministas del servidor MCP haro. Úsala siempre que el usuario pida revisar, entender, validar o modificar un haproxy.cfg o una carpeta conf.d de HAProxy.
---

# Trabajar con configuraciones de HAProxy usando haro

Requiere el servidor MCP de haro (`claude mcp add haro -- haro mcp --root /etc/haproxy`).
No adivines qué hace una configuración ni qué falla: pregúntaselo a haro.

## Flujo

1. **Entender**: `haro_explain` con `paths` (o `text`) para ver qué hace cada sección y directiva, incluida la herencia de `defaults`. Para un backend o frontend concreto, usa `section`.
2. **Revisar**: `haro_lint`. Cita siempre `archivo:línea` y la regla (`[backend-missing]`) al hablar de un problema. Ordena por gravedad: errores, avisos, sugerencias.
3. **Proponer cambios**: usa `haro_apply` sin `write` para simular. Devuelve el diff y los problemas que quedarían. Prefiere operaciones estructurales (`rename_section` con `updateRefs`, `add_directive`…) a reescribir el archivo entero: tocan solo lo necesario.
4. **Validar**: `haro_check` (`haproxy -c`) es la validación definitiva; el detector de `haro_lint` es orientativo. Si `haproxy` no está instalado, dilo.
5. **Escribir**: solo si el usuario lo pide, con `haro_apply` y `write: true` (requiere que el servidor se lanzara con `--allow-write`). Se valida y se deja una copia `.bak`. Enseña antes el diff.

## Buenas prácticas de HAProxy a vigilar

- Todo `frontend`/`listen` necesita `timeout client`; todo `backend`/`listen`, `timeout server` y `timeout connect` (en `defaults` o en la sección).
- No mezcles `mode http` y `mode tcp` entre un frontend y su backend.
- `default_backend`/`use_backend` deben apuntar a backends que existan, también entre archivos de `conf.d`.
- En `bind ... ssl`, declara `alpn h2,http/1.1` si quieres HTTP/2; no uses `verify none` en `server` sin saber por qué.
- Cambia las contraseñas de ejemplo (`stats auth`, `userlist`) y no expongas la página de estadísticas sin restricción.
- Con `haro_apply`, los índices de sección son los de la vista unificada de todos los archivos (los que muestra `haro_analyze`); vuelve a consultar los índices tras añadir o borrar secciones.
