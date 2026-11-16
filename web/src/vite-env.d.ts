/// <reference types="vite/client" />
interface ImportMetaEnv {
  /** `wasm` para compilar la web sin servidor (núcleo en WebAssembly). */
  readonly VITE_CORE?: 'wasm'
}
