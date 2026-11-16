import { expect, test } from '@playwright/test'

// Un consumidor externo (examples/consumer) monta la aplicación solo con la
// API pública de src/index: su propio ConfigStore, una pantalla previa, una
// cabecera y un botón de acción. Requiere `npm run build:consumer` (y `make wasm`).
const BASE = 'http://127.0.0.1:7477/'

type StoreProbe = { saved: { path: string; text: string }[][]; remoteChange(text: string): void }
const probe = <T,>(page: import('@playwright/test').Page, fn: (s: StoreProbe) => T) =>
  page.evaluate(`(${fn.toString()})(window.__store)`) as Promise<T>

test('el consumidor monta la app con su almacén, su gate, su cabecera y su acción', async ({ page }) => {
  await page.goto(BASE)
  // La pantalla previa va primero y la app aún no existe.
  await expect(page.getByTestId('gate')).toBeVisible()
  await expect(page.getByTestId('section')).toHaveCount(0)
  await page.getByTestId('gate-enter').click()

  // Entra con el almacén que eligió el gate: su contenido, su cabecera y su botón.
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByTestId('section').first()).toContainText('api')
  await expect(page.getByTestId('consumer-header')).toBeVisible()
  await expect(page.getByTestId('consumer-action')).toBeVisible()

  // Editar y guardar pasa por el almacén del consumidor, con el texto completo.
  await page.getByTestId('directive').filter({ hasText: 'balance roundrobin' }).first().locator('.dir-main').click()
  await page.getByLabel('Editar directiva').fill('balance leastconn')
  await page.getByLabel('Editar directiva').press('Enter')
  await expect(page.getByTestId('save-button')).toContainText('Guardar (1)')
  await page.getByTestId('save-button').click()
  await expect(page.getByRole('dialog')).toContainText('memoria/demo@main')
  await expect(page.getByRole('dialog')).toContainText('Se guarda en la rama main')
  await page.getByTestId('confirm-save').click()
  await expect(page.getByText('Guardado', { exact: true })).toBeVisible()
  const saved = await probe(page, s => s.saved)
  expect(saved).toHaveLength(1)
  expect(saved[0][0].text).toContain('balance leastconn')
})

test('un conflicto de versión del almacén se ve y se resuelve recargando', async ({ page }) => {
  await page.goto(BASE)
  await page.getByTestId('gate-enter').click()
  await expect(page.getByTestId('section').first()).toBeVisible()
  await page.getByTestId('directive').filter({ hasText: 'balance roundrobin' }).first().locator('.dir-main').click()
  await page.getByLabel('Editar directiva').fill('balance source')
  await page.getByLabel('Editar directiva').press('Enter')

  // Alguien más cambia el «repositorio» antes de guardar.
  await probe(page, s => s.remoteChange('backend api\n    balance first\n    server a1 10.0.0.1:80 check\n    server a2 10.0.0.2:80 check\n'))
  await page.getByTestId('save-button').click()
  await page.getByTestId('confirm-save').click()
  await expect(page.getByTestId('save-error')).toContainText('Conflicto')
  expect(await probe(page, s => s.saved.length)).toBe(0)

  // Recargar trae lo remoto y deja lo local en el historial.
  await page.getByTestId('reload-store').click()
  await expect(page.getByTestId('directive').filter({ hasText: 'balance first' }).first()).toBeVisible()
  await expect(page.getByTestId('directive').filter({ hasText: 'server a2' }).first()).toBeVisible()
})
