import { expect, test } from '@playwright/test'

const BASE = 'http://127.0.0.1:7472'

// Edita el archivo indicado (en modo visual) y abre el diálogo de guardado.
async function editDirective(page: import('@playwright/test').Page, from: string, to: string) {
  const row = page.getByTestId('directive').filter({ hasText: from }).first()
  await row.locator('.dir-main').click()
  await page.getByLabel('Editar directiva').fill(to)
  await page.getByLabel('Editar directiva').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: to }).first()).toBeVisible()
}

test.describe.configure({ mode: 'serial' })

test('varios archivos: pestañas, editar y guardar', async ({ page, request }) => {
  await page.goto(BASE + '/')
  await expect(page.getByTestId('section').first()).toBeVisible()
  await page.getByRole('tab', { name: 'Código' }).click()
  await expect(page.getByRole('tab', { name: /10-global\.cfg/ })).toBeVisible()
  await expect(page.getByRole('tab', { name: /20-web\.cfg/ })).toBeVisible()
  await expect(page.getByRole('tab', { name: /30-api\.cfg/ })).toBeVisible()
  await page.getByRole('tab', { name: /20-web\.cfg/ }).click()
  await expect(page.locator('.cm-content')).toContainText('frontend web')
  await page.getByRole('tab', { name: 'Visual' }).click()

  await editDirective(page, 'maxconn 4096', 'maxconn 2048')
  await page.getByTestId('save-button').click()
  await expect(page.getByTestId('diff')).toContainText('maxconn 2048')
  await page.getByTestId('confirm-save').click()
  await expect(page.getByText('Guardado', { exact: true })).toBeVisible()
  const disk = await (await request.post(BASE + '/haro.v1.ConfigService/GetConfig', { data: {} })).json()
  expect(disk.files[0].text).toContain('maxconn 2048')
  expect(disk.files[1].text).not.toContain('maxconn 2048')
})

test('un guardado que rompe una referencia entre archivos se rechaza', async ({ page, request }) => {
  await page.goto(BASE + '/')
  await expect(page.getByTestId('section').first()).toBeVisible()
  // 20-web.cfg usa «api», definido en 30-api.cfg; apuntarlo a un backend inexistente rompe la referencia.
  await editDirective(page, 'use_backend api if is_api', 'use_backend inexistente if is_api')
  await page.getByTestId('save-button').click()
  await page.getByTestId('confirm-save').click()
  await expect(page.getByTestId('save-error')).toBeVisible()
  const disk = await (await request.post(BASE + '/haro.v1.ConfigService/GetConfig', { data: {} })).json()
  expect(disk.files[1].text).toContain('use_backend api if is_api')
  expect(disk.files[1].text).not.toContain('inexistente')
})

test('renombrar con referencias entre archivos se guarda de una vez', async ({ page, request }) => {
  await page.goto(BASE + '/')
  await expect(page.getByTestId('section').first()).toBeVisible()
  await page.getByRole('button', { name: 'Renombrar backend api' }).click()
  await page.getByRole('dialog').getByRole('textbox').fill('api2')
  await page.getByRole('dialog').getByRole('button', { name: 'Aceptar' }).click()
  await expect(page.getByTestId('save-button')).toContainText('Guardar (2)')
  await page.getByTestId('save-button').click()
  await page.getByTestId('confirm-save').click()
  await expect(page.getByText('Guardados 2 archivos')).toBeVisible()
  const disk = await (await request.post(BASE + '/haro.v1.ConfigService/GetConfig', { data: {} })).json()
  expect(disk.files[1].text).toContain('use_backend api2 if is_api')
  expect(disk.files[2].text).toContain('backend api2')
})
