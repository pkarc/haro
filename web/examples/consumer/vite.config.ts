import react from '@vitejs/plugin-react'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

// Configuration of a consumer. In haro-site: the same `resolve.dedupe` (a single
// copy of React even if haro/web has its own node_modules) and `publicDir`
// pointing to where `make wasm` leaves haro.wasm and wasm_exec.js.
const here = dirname(fileURLToPath(import.meta.url))
export default defineConfig({
  root: here,
  plugins: [react()],
  publicDir: resolve(here, '../../public'),
  resolve: { dedupe: ['react', 'react-dom'] },
  build: { outDir: resolve(here, '../../dist-consumer'), emptyOutDir: true },
})
