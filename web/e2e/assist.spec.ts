import { expect, test, type Page } from '@playwright/test'

// Asistente con la API de Anthropic falsa (e2e/fake-anthropic.mjs) y haro serve
// --assistant sobre example.cfg, sin --allow-write: nada se escribe en disco.
const ASSIST = 'http://127.0.0.1:7474/'
const PLAIN = 'http://127.0.0.1:7471/' // sin --assistant

// Texto completo del archivo activo (CodeMirror solo pinta lo visible): se lee de Exportar.
async function fullText(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Exportar' }).click()
  const text = await page.getByLabel('Contenido a exportar').inputValue()
  await page.getByRole('dialog').getByRole('button', { name: 'Cerrar', exact: true }).last().click()
  return text
}

async function openChat(page: Page) {
  await page.goto(ASSIST)
  await expect(page.getByTestId('section').first()).toBeVisible()
  await page.getByTestId('assistant-toggle').click()
  await expect(page.getByTestId('chat-panel')).toBeVisible()
}

async function ask(page: Page, text: string, confirm = false) {
  await page.getByLabel('Mensaje para el asistente').fill(text)
  await page.getByLabel('Mensaje para el asistente').press('Enter')
  if (confirm) {
    const dlg = page.getByRole('dialog')
    await expect(dlg).toContainText('API de Anthropic')
    await expect(dlg).toContainText('claude-opus-5-5')
    await dlg.getByRole('button', { name: 'Enviar a Anthropic' }).click()
  }
}

test.beforeEach(async ({ page }) => { await page.addInitScript(() => { if (!sessionStorage.getItem('e2e-clean')) { localStorage.clear(); sessionStorage.setItem('e2e-clean', '1') } }) })

test('preguntar, ver el streaming y la propuesta con diff, aplicar y deshacer', async ({ page }) => {
  await openChat(page)
  await expect(page.getByTestId('chat-privacy')).toContainText('API de Anthropic')
  const before = await fullText(page)
  expect(before).toContain('maxconn 4096')

  await ask(page, 'Sube maxconn a 8192', true)
  await expect(page.getByTestId('msg-user')).toContainText('Sube maxconn a 8192')
  // streaming: el texto llega por trozos y la respuesta sigue en curso
  await expect(page.getByTestId('msg-assistant')).toContainText('maxconn')
  await expect(page.getByRole('button', { name: 'Detener' })).toBeVisible()
  // el bloque de configuración nunca se pinta entero en el chat
  await expect(page.getByTestId('chat-log')).not.toContainText('frontend http_in')

  const card = page.getByTestId('proposal')
  await expect(card).toBeVisible()
  await expect(page.getByRole('button', { name: 'Enviar', exact: true })).toBeVisible()
  await expect(card.locator('.dl.da')).toContainText('maxconn 8192')
  await expect(card.locator('.dl.dd')).toContainText('maxconn 4096')
  await expect(page.getByTestId('proposal-issues')).toContainText('Tras aplicarla: 0 errores')
  await expect(page.getByTestId('chat-log')).toContainText('Revisa el diff antes de aplicar')

  await card.getByRole('button', { name: 'Aplicar' }).click()
  await expect(card).toHaveAttribute('data-status', 'applied')
  expect(await fullText(page)).toBe(before.replace('maxconn 4096', 'maxconn 8192'))
  // versión previa en el historial
  await page.getByRole('button', { name: 'Historial' }).click()
  await expect(page.getByTestId('version').first()).toContainText('Antes de aplicar propuesta del asistente')
  await page.getByRole('dialog').getByRole('button', { name: 'Cerrar', exact: true }).last().click()

  await page.getByRole('button', { name: 'Deshacer', exact: true }).last().click()
  expect(await fullText(page)).toBe(before)
})

test('el historial del chat sobrevive a la recarga y «Nueva conversación» lo borra', async ({ page }) => {
  await openChat(page)
  await ask(page, 'Sube maxconn', true)
  await expect(page.getByTestId('proposal')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Enviar', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByTestId('chat-panel')).toBeVisible()
  await expect(page.getByTestId('msg-user')).toContainText('Sube maxconn')
  await expect(page.getByTestId('proposal')).toBeVisible()
  await page.getByRole('button', { name: 'Nueva conversación' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Nueva conversación' }).click()
  await expect(page.getByTestId('msg-user')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Revisa mi configuración/ })).toBeVisible()
})

test('Detener corta la respuesta y conserva lo recibido', async ({ page }) => {
  await openChat(page)
  await ask(page, 'LENTO por favor', true)
  await expect(page.getByTestId('msg-assistant')).toContainText('Pensando')
  await page.getByRole('button', { name: 'Detener' }).click()
  await expect(page.getByTestId('msg-assistant')).toContainText('(detenido)')
  await expect(page.getByTestId('msg-assistant')).toContainText('Pensando')
  await expect(page.getByRole('button', { name: 'Enviar', exact: true })).toBeVisible()
})

test('con credenciales rechazadas (401) muestra un error legible con reintento', async ({ page }) => {
  await openChat(page)
  await ask(page, 'FALLA401', true)
  const err = page.getByTestId('chat-error')
  await expect(err).toContainText('rechazó las credenciales')
  await expect(err).toContainText('ANTHROPIC_API_KEY')
  await expect(err.getByRole('button', { name: 'Reintentar' })).toBeVisible()
})

test('una propuesta obsoleta pide confirmación antes de aplicarse', async ({ page }) => {
  await openChat(page)
  await ask(page, 'Sube maxconn', true)
  const card = page.getByTestId('proposal')
  await expect(card).toBeVisible()
  await expect(page.getByRole('button', { name: 'Enviar', exact: true })).toBeVisible()
  // el usuario edita la configuración después de pedir la propuesta
  const edited = (await fullText(page)).replace('maxconn 4096', 'maxconn 4096\n    nbthread 2')
  await page.getByRole('button', { name: 'Importar' }).click()
  await page.getByRole('dialog').getByLabel('Texto a importar').fill(edited)
  await page.getByRole('dialog').getByRole('button', { name: 'Reemplazar' }).click()
  await expect(page.getByTestId('proposal-stale')).toBeVisible()
  await card.getByRole('button', { name: 'Aplicar' }).click()
  const dlg = page.getByRole('dialog')
  await expect(dlg).toContainText('ha cambiado')
  await dlg.getByRole('button', { name: 'Cancelar' }).click()
  await expect(card).toHaveAttribute('data-status', 'pending')
  await card.getByRole('button', { name: 'Aplicar' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Aplicar de todos modos' }).click()
  await expect(card).toHaveAttribute('data-status', 'applied')
  expect(await fullText(page)).not.toContain('nbthread')
})

test('descartar la propuesta no toca la configuración', async ({ page }) => {
  await openChat(page)
  const before = await fullText(page)
  await ask(page, 'Sube maxconn', true)
  const card = page.getByTestId('proposal')
  await expect(page.getByRole('button', { name: 'Enviar', exact: true })).toBeVisible()
  await card.getByRole('button', { name: 'Descartar' }).click()
  await expect(card).toHaveAttribute('data-status', 'discarded')
  expect(await fullText(page)).toBe(before)
})

test('sin --assistant no hay botón ni panel del asistente', async ({ page }) => {
  await page.goto(PLAIN)
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByTestId('assistant-toggle')).toHaveCount(0)
  await expect(page.getByTestId('chat-panel')).toHaveCount(0)
})
