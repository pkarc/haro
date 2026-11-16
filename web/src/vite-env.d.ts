/// <reference types="vite/client" />
interface ImportMetaEnv {
  /** `wasm` to build the serverless web (core in WebAssembly). */
  readonly VITE_CORE?: 'wasm'
}
