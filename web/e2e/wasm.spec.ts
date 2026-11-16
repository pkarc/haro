import { expect, test } from '@playwright/test'

// The serverless web: the core in WebAssembly inside the browser. Requires
// `make wasm` and `npm run build:wasm` (done by `npm run test:e2e:wasm`).
const BASE = 'http://127.0.0.1:7473/'

test('serverless web: analyzes, edits and adds a template, without calling any server', async ({ page }) => {
  const requests: string[] = []
  page.on('request', r => requests.push(new URL(r.url()).pathname))
  await page.goto(BASE)
  await expect(page.getByTestId('section').first()).toBeVisible()
  expect(await page.getByTestId('section').count()).toBeGreaterThan(1)

  // No disk: there is no save-to-disk button.
  await expect(page.getByTestId('save-button')).toHaveCount(0)

  // Editing in visual mode goes through apply (in WebAssembly).
  const first = page.getByTestId('directive').first()
  await expect(first).toBeVisible()
  await page.getByTestId('directive').filter({ hasText: 'balance roundrobin' }).first().locator('.dir-main').click()
  await page.getByLabel('Edit directive').fill('balance leastconn')
  await page.getByLabel('Edit directive').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'balance leastconn' }).first()).toBeVisible()

  // Technology template.
  await page.getByTestId('template-button').click()
  const dialog = page.getByRole('dialog', { name: 'Add from technology template' })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Technology').selectOption('nextcloud')
  await dialog.getByLabel('Public domain').fill('cloud.example.com')
  await dialog.getByLabel('Nextcloud servers (host:port)').fill('10.0.0.10:80')
  await dialog.getByLabel('TLS certificate (.pem)').fill('/etc/haproxy/certs/cloud.pem')
  await expect(page.getByTestId('template-preview').getByTestId('diff')).toContainText('backend nextcloud')
  await page.keyboard.press('Escape')

  // No request to the API: everything runs in the browser.
  expect(requests.filter(p => p.startsWith('/haro.v1.'))).toEqual([])
  expect(requests.some(p => p.endsWith('haro.wasm'))).toBe(true)
})
