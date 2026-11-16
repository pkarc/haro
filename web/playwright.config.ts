import { defineConfig } from '@playwright/test'

// Requires `make build` (bin/haro with the embedded web). Locally the system
// Chrome can be used with PW_CHANNEL=chrome; CI installs chromium.
const channel = process.env.PW_CHANNEL || undefined
export default defineConfig({
  testDir: 'e2e',
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  // fixed locale: the default language follows the browser's and the e2e tests expect English.
  use: { channel, locale: 'en-US', trace: 'retain-on-failure' },
  webServer: [
    { command: 'node e2e/serve.mjs example 7471', url: 'http://127.0.0.1:7471/', reuseExistingServer: false },
    { command: 'node e2e/serve.mjs multi 7472', url: 'http://127.0.0.1:7472/', reuseExistingServer: false },
    // Assistant: fake Anthropic API (7476) and haro serve --assistant pointing to it (7474).
    { command: 'node e2e/fake-anthropic.mjs 7476', port: 7476, reuseExistingServer: false },
    { command: 'node e2e/serve.mjs assist 7474', url: 'http://127.0.0.1:7474/', reuseExistingServer: false },
    // Serverless web (core in WebAssembly): requires `npm run build:wasm` first.
    { command: 'npm run preview:wasm', url: 'http://127.0.0.1:7473/', reuseExistingServer: false },
    // Example consumer (like haro-site): requires `npm run build:consumer` first.
    { command: 'npm run preview:consumer', url: 'http://127.0.0.1:7477/', reuseExistingServer: false },
  ],
  projects: [
    { name: 'desktop', testMatch: /(example|multi|assist)\.spec\.ts/, use: { viewport: { width: 1280, height: 800 } } },
    { name: 'wasm', testMatch: /wasm\.spec\.ts/, use: { viewport: { width: 1280, height: 800 } } },
    { name: 'consumer', testMatch: /consumer\.spec\.ts/, use: { viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', testMatch: /mobile\.spec\.ts/, use: { viewport: { width: 390, height: 844 }, hasTouch: true } },
  ],
})
