import { defineConfig } from '@playwright/test'

// Requiere `make build` (bin/haro con la web embebida). En local puede usarse el
// Chrome del sistema con PW_CHANNEL=chrome; en CI se instala chromium.
const channel = process.env.PW_CHANNEL || undefined
export default defineConfig({
  testDir: 'e2e',
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  // locale fijo: el idioma por defecto del núcleo sigue al del navegador y los e2e esperan español.
  use: { channel, locale: 'es-ES', trace: 'retain-on-failure' },
  webServer: [
    { command: 'node e2e/serve.mjs example 7471', url: 'http://127.0.0.1:7471/', reuseExistingServer: false },
    { command: 'node e2e/serve.mjs multi 7472', url: 'http://127.0.0.1:7472/', reuseExistingServer: false },
    // Asistente: API de Anthropic falsa (7476) y haro serve --assistant apuntando a ella (7474).
    { command: 'node e2e/fake-anthropic.mjs 7476', port: 7476, reuseExistingServer: false },
    { command: 'node e2e/serve.mjs assist 7474', url: 'http://127.0.0.1:7474/', reuseExistingServer: false },
    // Web sin servidor (núcleo en WebAssembly): requiere `npm run build:wasm` antes.
    { command: 'npm run preview:wasm', url: 'http://127.0.0.1:7473/', reuseExistingServer: false },
    // Consumidor de ejemplo (como haro-site): requiere `npm run build:consumer` antes.
    { command: 'npm run preview:consumer', url: 'http://127.0.0.1:7477/', reuseExistingServer: false },
  ],
  projects: [
    { name: 'desktop', testMatch: /(example|multi|assist)\.spec\.ts/, use: { viewport: { width: 1280, height: 800 } } },
    { name: 'wasm', testMatch: /wasm\.spec\.ts/, use: { viewport: { width: 1280, height: 800 } } },
    { name: 'consumer', testMatch: /consumer\.spec\.ts/, use: { viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', testMatch: /mobile\.spec\.ts/, use: { viewport: { width: 390, height: 844 }, hasTouch: true } },
  ],
})
