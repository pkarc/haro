import { expect, test } from '@playwright/test'

test('móvil: pestañas inferiores Código / Visual / Asistente', async ({ page }) => {
  await page.goto('http://127.0.0.1:7471/')
  const tabs = page.getByRole('tablist', { name: 'Secciones' })
  await expect(tabs).toBeVisible()
  // el selector de modo del escritorio no se muestra
  await expect(page.getByRole('tablist', { name: 'Modo de edición' })).toBeHidden()

  await tabs.getByRole('tab', { name: /Visual/ }).click()
  await expect(page.getByTestId('section').first()).toBeVisible()

  await tabs.getByRole('tab', { name: /Código/ }).click()
  await expect(page.locator('.cm-editor')).toBeVisible()
  await expect(page.locator('.cm-content')).toContainText('frontend http_in')

  await tabs.getByRole('tab', { name: /Asistente/ }).click()
  await expect(page.getByTestId('assistant-placeholder')).toContainText('Próximamente')
  await expect(page.locator('.cm-editor')).toHaveCount(0)

  await tabs.getByRole('tab', { name: /Visual/ }).click()
  await expect(page.getByTestId('section').first()).toBeVisible()
  // sin desbordamiento horizontal
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})
