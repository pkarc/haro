import { ConnectCoreClient, type CoreClient } from '../core/client'
import { WasmCoreClient } from '../core/wasm'
import { createAppStore } from './store'

// `VITE_CORE=wasm npm run build` genera la web sin servidor (núcleo en
// WebAssembly dentro del navegador, ver `make wasm`); por defecto habla con
// `haro serve`.
export const coreClient: CoreClient = import.meta.env.VITE_CORE === 'wasm' ? new WasmCoreClient() : new ConnectCoreClient()
export const useApp = createAppStore(coreClient)
