English | [Español](README.es.md)

# haro · web

haro's React + Vite + TypeScript interface. It talks to the core only through `CoreClient`:

- `ConnectCoreClient`: `haro serve` (native Go, Connect RPC generated with `buf`).
- `WasmCoreClient`: the same core compiled to WebAssembly (`make wasm`, from the root), no server.

It saves and loads files through `ConfigStore`:

- `ServedStore`: the files from `haro serve --file` (saved with the server's guarantees: full validation, atomic write, `.bak` copy).
- `LocalStore`: no origin; the session lives in the browser (localStorage) and you get out of it with import/export.

## Development

```sh
cd web && npm install
npm run dev            # with hot reload; /haro.v1.* calls go to `haro serve` on :7468
npm test               # Vitest
npm run build          # web/dist, the one the binary embeds (make web && make build)
npm run build:wasm     # serverless web in web/dist-wasm (VITE_CORE=wasm; first: make wasm)
npm run build:consumer # example consumer in web/dist-consumer (see below)
npx playwright test    # e2e (PW_CHANNEL=chrome to use the system Chrome)
```

## Using it from another repo (haro-site)

The web app is designed to be consumed **as a git submodule** (no npm package): the consuming repo includes haro as `haro/`, declares `haro/web` as an npm *workspace* (a single install; React is not duplicated) and mounts the application with its own stores and screens. Its own code lives outside the submodule, which is never edited.

```sh
git submodule add https://github.com/pkarc/haro haro
```

```jsonc
// consumer's package.json
{ "private": true, "workspaces": ["haro/web"], "dependencies": { "react": "^19", "react-dom": "^19" } }
```

```ts
// consumer's vite.config.ts
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
export default defineConfig({
  plugins: [react()],
  publicDir: 'haro/web/public',                 // `make -C haro wasm` drops haro.wasm and wasm_exec.js here
  resolve: { dedupe: ['react', 'react-dom'] },  // a single copy of React
})
```

```tsx
// consumer's src/main.tsx
import { mountApp, WasmCoreClient, LocalStore, type ConfigStore } from '../haro/web/src/index'
import '../haro/web/src/styles.css'

class MyStore implements ConfigStore { /* label, canSave, load(), save(files) */ }

mountApp({
  client: new WasmCoreClient(),
  store: new LocalStore(),            // initial store; the gate can replace it
  container: document.getElementById('root')!,
  header: <MyHeader />,               // above the top bar
  actions: <MyButton />,              // in the top bar, before Settings
  gate: ({ enter }) => <MyLogin onRepo={repo => enter(new MyStore(repo))} />,
})
```

`examples/consumer/` is a complete, minimal consumer (in-memory store with version control, a pre-screen, header, action); `e2e/consumer.spec.ts` tests it in the browser and `npm run typecheck:consumer` checks that the public API of `src/index.ts` is enough to mount it.

### The `ConfigStore` contract

| | |
|---|---|
| `label` | short name of the destination ("disk", "owner/repo@main"); shown in the interface |
| `canSave` / `readOnlyHint` | whether saving is possible and, if not, why (shown when pressing "Save") |
| `saveHint` | notice in the save dialog |
| `storageKey` | localStorage key for the session; **different per origin** so files are not mixed |
| `load()` | files in load order, or `null` (local session with no origin) |
| `save(files)` | saves **all or none**. Expected failures are a `SaveResult` with `saved: false` (not exceptions); `conflict: true` means the origin changed since it was loaded: the interface offers to reload and keeps the local version in the history |

### Public surface (`src/index.ts`)

`mountApp`, `App`, `CoreClient`, `ConnectCoreClient`, `WasmCoreClient`, `ConfigStore`, `SaveResult`, `LocalStore`, `ServedStore`, the core types, and for the interface slots: `useApp`, `useAppApi`, `useConfigStore`, `useCoreClient`, `createAppStore`, `Modal` and the dialogs (`confirmBox`, `promptBox`, `openDialog`).

`mountApp` also accepts `gate` (a pre-screen shown until `enter(store?)` is called) and `strict` (StrictMode, on by default). To switch stores once the application is mounted, unmount it or give it another `key` (state is created once per client/store pair).

## Conventions

- The core returns minimal markdown (`**bold**`, `` `code` ``) that is rendered with components (never `dangerouslySetInnerHTML`).
- The types in `src/gen` are generated code (`make proto`, from the root); do not edit them.
