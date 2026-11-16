# haro · web

Interfaz React + Vite + TypeScript de haro. Habla con el núcleo solo a través de `CoreClient`:

- `ConnectCoreClient`: `haro serve` (Go nativo, Connect RPC generado con `buf`).
- `WasmCoreClient`: el mismo núcleo compilado a WebAssembly (`make wasm`, desde la raíz), sin servidor.

Y guarda y carga los archivos a través de `ConfigStore`:

- `ServedStore`: los archivos de `haro serve --file` (se guardan con las garantías del servidor: validación completa, atómico, copia `.bak`).
- `LocalStore`: sin origen; la sesión vive en el navegador (localStorage) y se sale con importar/exportar.

## Desarrollo

```sh
cd web && npm install
npm run dev            # con recarga; las llamadas /haro.v1.* van a `haro serve` en :7468
npm test               # Vitest
npm run build          # web/dist, la que embebe el binario (make web && make build)
npm run build:wasm     # web sin servidor en web/dist-wasm (VITE_CORE=wasm; antes: make wasm)
npm run build:consumer # consumidor de ejemplo en web/dist-consumer (ver abajo)
npx playwright test    # e2e (PW_CHANNEL=chrome para usar el Chrome del sistema)
```

## Usarla desde otro repo (haro-site)

La web está pensada para consumirse **como submódulo git** (sin paquete npm): el repo consumidor incluye haro como `haro/`, declara `haro/web` como *workspace* de npm (una sola instalación; React no se duplica) y monta la aplicación con sus propios almacenes y pantallas. Lo suyo vive fuera del submódulo, que no se edita.

```sh
git submodule add https://github.com/pkarc/haro haro
```

```jsonc
// package.json del consumidor
{ "private": true, "workspaces": ["haro/web"], "dependencies": { "react": "^19", "react-dom": "^19" } }
```

```ts
// vite.config.ts del consumidor
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
export default defineConfig({
  plugins: [react()],
  publicDir: 'haro/web/public',                 // aquí deja `make -C haro wasm` haro.wasm y wasm_exec.js
  resolve: { dedupe: ['react', 'react-dom'] },  // una sola copia de React
})
```

```tsx
// src/main.tsx del consumidor
import { mountApp, WasmCoreClient, LocalStore, type ConfigStore } from '../haro/web/src/index'
import '../haro/web/src/styles.css'

class MiAlmacen implements ConfigStore { /* label, canSave, load(), save(files) */ }

mountApp({
  client: new WasmCoreClient(),
  store: new LocalStore(),            // almacén inicial; el gate puede sustituirlo
  container: document.getElementById('root')!,
  header: <MiCabecera />,             // sobre la barra superior
  actions: <MiBoton />,               // en la barra superior, antes de Ajustes
  gate: ({ enter }) => <MiLogin onRepo={repo => enter(new MiAlmacen(repo))} />,
})
```

`examples/consumer/` es un consumidor completo y mínimo (almacén en memoria con control de versión, pantalla previa, cabecera, acción); `e2e/consumer.spec.ts` lo prueba en el navegador y `npm run typecheck:consumer` comprueba que la API pública de `src/index.ts` alcanza para montarlo.

### Contrato de `ConfigStore`

| | |
|---|---|
| `label` | nombre corto del destino («disco», «owner/repo@main»); sale en la interfaz |
| `canSave` / `readOnlyHint` | si se puede guardar y, si no, por qué (se enseña al pulsar «Guardar») |
| `saveHint` | aviso del diálogo de guardado |
| `storageKey` | clave del localStorage de la sesión; **distinta por origen** para no mezclar archivos |
| `load()` | archivos en orden de carga, o `null` (sesión local sin origen) |
| `save(files)` | guarda **todos o ninguno**. Los fallos esperables son un `SaveResult` con `saved: false` (no excepciones); `conflict: true` indica que el origen cambió desde que se cargó: la interfaz ofrece recargar y deja lo local en el historial |

### Superficie pública (`src/index.ts`)

`mountApp`, `App`, `CoreClient`, `ConnectCoreClient`, `WasmCoreClient`, `ConfigStore`, `SaveResult`, `LocalStore`, `ServedStore`, los tipos del núcleo, y para los huecos de la interfaz: `useApp`, `useAppApi`, `useConfigStore`, `useCoreClient`, `createAppStore`, `Modal` y los diálogos (`confirmBox`, `promptBox`, `openDialog`).

`mountApp` acepta además `gate` (pantalla previa hasta llamar a `enter(store?)`) y `strict` (StrictMode, por defecto sí). Para cambiar de almacén con la aplicación ya montada, desmóntala o dale otra `key` (el estado se crea una vez por pareja cliente/almacén).

## Convenciones

- Textos de interfaz en español; el núcleo devuelve markdown mínimo (`**negrita**`, `` `código` ``) que se pinta con componentes (nunca `dangerouslySetInnerHTML`).
- Los tipos de `src/gen` son código generado (`make proto`, desde la raíz); no se editan.
