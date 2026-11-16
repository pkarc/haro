import { expect, test } from '@playwright/test'

// La web sin servidor: el núcleo en WebAssembly dentro del navegador. Requiere
// `make wasm` y `npm run build:wasm` (lo hace `npm run test:e2e:wasm`).
const BASE = 'http://127.0.0.1:7473/'

test('web sin servidor: analiza, edita y añade una plantilla, sin llamar a ningún servidor', async ({ page }) => {
  const requests: string[] = []
  page.on('request', r => requests.push(new URL(r.url()).pathname))
  await page.goto(BASE)
  await expect(page.getByTestId('section').first()).toBeVisible()
  expect(await page.getByTestId('section').count()).toBeGreaterThan(1)

  // Sin disco: no hay botón de guardar en disco.
  await expect(page.getByTestId('save-button')).toHaveCount(0)

  // Editar en modo visual pasa por apply (en WebAssembly).
  const first = page.getByTestId('directive').first()
  await expect(first).toBeVisible()
  await page.getByTestId('directive').filter({ hasText: 'balance roundrobin' }).first().locator('.dir-main').click()
  await page.getByLabel('Editar directiva').fill('balance leastconn')
  await page.getByLabel('Editar directiva').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'balance leastconn' }).first()).toBeVisible()

  // Plantilla de tecnología.
  await page.getByTestId('template-button').click()
  const dialog = page.getByRole('dialog', { name: 'Añadir desde plantilla de tecnología' })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Tecnología').selectOption('nextcloud')
  await dialog.getByLabel('Dominio público').fill('cloud.example.com')
  await dialog.getByLabel('Servidores de Nextcloud (host:puerto)').fill('10.0.0.10:80')
  await dialog.getByLabel('Certificado TLS (.pem)').fill('/etc/haproxy/certs/cloud.pem')
  await expect(page.getByTestId('template-preview').getByTestId('diff')).toContainText('backend nextcloud')
  await page.keyboard.press('Escape')

  // Ninguna petición a la API: todo corre en el navegador.
  expect(requests.filter(p => p.startsWith('/haro.v1.'))).toEqual([])
  expect(requests.some(p => p.endsWith('haro.wasm'))).toBe(true)
})
