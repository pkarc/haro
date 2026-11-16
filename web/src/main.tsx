import { mountApp } from './mount'
import { ConnectCoreClient } from './core/client'
import { LocalStore, ServedStore } from './core/configstore'
import { WasmCoreClient } from './core/wasm'
import './styles.css'

// Arranque por defecto. `VITE_CORE=wasm npm run build` genera la web sin
// servidor (núcleo en WebAssembly, sesión local, ver `make wasm`); por defecto
// habla con `haro serve` y sus archivos (`--file`).
const wasm = import.meta.env.VITE_CORE === 'wasm'
const client = wasm ? new WasmCoreClient() : new ConnectCoreClient()
mountApp({ client, store: wasm ? new LocalStore() : new ServedStore(client), container: document.getElementById('root')! })
