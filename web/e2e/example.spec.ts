import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const BASE = 'http://127.0.0.1:7471/'
const original = readFileSync(join(resolve(dirname(fileURLToPath(import.meta.url)), '..', '..'), 'core/testdata/example.cfg'), 'utf8')

// Texto completo del archivo activo: CodeMirror solo pinta las líneas visibles,
// así que se lee del diálogo Exportar (mismo texto que muestra el modo código).
async function fullText(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Exportar' }).click()
  const text = await page.getByLabel('Contenido a exportar').inputValue()
  await page.getByRole('dialog').getByRole('button', { name: 'Cerrar', exact: true }).last().click()
  return text
}

test.beforeEach(async ({ page }) => {
  await page.goto(BASE)
  await expect(page.getByTestId('section').first()).toBeVisible()
})

test('carga el ejemplo: secciones, mapa y problemas', async ({ page }) => {
  await expect(page.getByTestId('section')).toHaveCount(8)
  await expect(page.getByTestId('map-entry')).toHaveCount(3)
  await expect(page.getByTestId('map-backend')).toHaveCount(3)
  await expect(page.getByTestId('problems-button')).toContainText('1 sugerencia')
  await page.getByTestId('problems-button').click()
  await expect(page.getByTestId('problem')).toHaveCount(1)
  await expect(page.getByTestId('problem')).toContainText('contraseña')
  // explicación del núcleo en la tarjeta
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first()).toContainText('4096')
})

test('el código muestra el problema como diagnóstico y la explicación de la línea', async ({ page }) => {
  // CodeMirror solo pinta las líneas visibles: se salta al problema con «Ir» del panel.
  await page.getByRole('tab', { name: 'Código' }).click()
  await page.getByTestId('problems-button').click()
  await page.getByTestId('problem').getByRole('button', { name: 'Ir' }).click()
  await expect(page.locator('.cm-editor')).toBeVisible()
  await expect(page.locator('.cm-lint-marker').first()).toBeVisible()
  await page.locator('.cm-line', { hasText: 'stats auth' }).click()
  await expect(page.getByTestId('code-status')).toContainText('Sugerencia')
  await expect(page.getByTestId('code-status')).toContainText('stats')
  await page.getByTestId('code-editor').locator('.cm-line', { hasText: 'stats uri' }).click()
  await expect(page.getByTestId('code-status')).toContainText('/stats')
})

test('mapa: clic en un nodo lleva a su sección', async ({ page }) => {
  await page.getByTestId('map-backend').filter({ hasText: 'static_servers' }).click()
  const sec = page.getByTestId('section').filter({ hasText: 'static_servers' }).first()
  await expect(sec).toHaveClass(/./)
  await expect(page.locator('.flash')).toHaveCount(1)
})

test('editar en modo visual cambia el texto de forma mínima', async ({ page }) => {
  // 1) cambiar una directiva
  await page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first().locator('.dir-main').click()
  await page.getByLabel('Editar directiva').fill('maxconn 8192')
  await page.getByLabel('Editar directiva').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 8192' })).toHaveCount(1)
  // 2) añadir un servidor
  await page.getByLabel('Añadir directiva a backend web_servers').fill('server web3 10.0.0.13:8080 check')
  await page.getByLabel('Añadir directiva a backend web_servers').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'web3' })).toHaveCount(1)
  // 3) renombrar un backend con referencias
  await page.getByRole('button', { name: 'Renombrar backend api_servers' }).click()
  await page.getByRole('dialog').getByRole('textbox').fill('api_v2')
  await page.getByRole('dialog').getByRole('button', { name: 'Aceptar' }).click()
  await expect(page.getByRole('button', { name: 'Renombrar backend api_v2' })).toBeVisible()

  await page.getByRole('tab', { name: 'Código' }).click()
  await expect(page.locator('.cm-content')).toContainText('maxconn 8192')
  const text = await fullText(page)
  const expected = original.split('\n')
    .map(l => l === '    maxconn 4096' ? '    maxconn 8192' : l.replace('api_servers', 'api_v2'))
  const at = expected.indexOf('    server web2 10.0.0.12:8080 check cookie web2')
  expected.splice(at + 1, 0, '    server web3 10.0.0.13:8080 check')
  expect(text).toBe(expected.join('\n'))
})

test('deshacer y rehacer desde los botones y con Ctrl+Z', async ({ page }) => {
  await page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first().locator('.dir-main').click()
  await page.getByLabel('Editar directiva').fill('maxconn 1')
  await page.getByLabel('Editar directiva').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 1' })).toHaveCount(1)
  await page.getByRole('button', { name: 'Deshacer' }).click()
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' })).toHaveCount(1)
  await page.getByRole('button', { name: 'Rehacer' }).click()
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 1' })).toHaveCount(1)
  await page.locator('body').press('Control+z')
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' })).toHaveCount(1)
})

test('sin --allow-write, guardar avisa de que no hay permiso', async ({ page }) => {
  await page.getByTestId('save-button').click()
  await expect(page.getByRole('alert').filter({ hasText: '--allow-write' })).toBeVisible()
})

test('añadir sección desde plantilla', async ({ page }) => {
  await page.getByRole('button', { name: '+ resolvers' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Añadir' }).click()
  await expect(page.getByTestId('section')).toHaveCount(9)
})
