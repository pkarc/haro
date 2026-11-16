import react from '@vitejs/plugin-react'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

// Configuración de un consumidor. En haro-site: el mismo `resolve.dedupe` (una
// sola copia de React aunque haro/web tenga su propio node_modules) y
// `publicDir` apuntando a donde `make wasm` deje haro.wasm y wasm_exec.js.
const here = dirname(fileURLToPath(import.meta.url))
export default defineConfig({
  root: here,
  plugins: [react()],
  publicDir: resolve(here, '../../public'),
  resolve: { dedupe: ['react', 'react-dom'] },
  build: { outDir: resolve(here, '../../dist-consumer'), emptyOutDir: true },
})
