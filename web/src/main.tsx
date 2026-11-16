import { mountApp } from './mount'
import { ConnectCoreClient } from './core/client'
import { LocalStore, ServedStore } from './core/configstore'
import { WasmCoreClient } from './core/wasm'
import './styles.css'

// Default startup. `VITE_CORE=wasm npm run build` builds the serverless web
// (core in WebAssembly, local session, see `make wasm`); by default it talks to
// `haro serve` and its files (`--file`).
const wasm = import.meta.env.VITE_CORE === 'wasm'
const client = wasm ? new WasmCoreClient() : new ConnectCoreClient()
mountApp({ client, store: wasm ? new LocalStore() : new ServedStore(client), container: document.getElementById('root')! })
