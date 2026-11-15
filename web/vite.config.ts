import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// En desarrollo, /api se redirige a `haro serve` (puerto 7468).
export default defineConfig({
  plugins: [react()],
  server: { proxy: { '/api': 'http://127.0.0.1:7468' } },
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1000 },
  test: { include: ['src/**/*.test.{ts,tsx}'], environment: 'node' },
})
