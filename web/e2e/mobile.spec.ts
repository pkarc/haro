import { expect, test } from '@playwright/test'

test('mobile: bottom tabs Code / Visual / Assistant', async ({ page }) => {
  await page.goto('http://127.0.0.1:7471/')
  const tabs = page.getByRole('tablist', { name: 'Sections' })
  await expect(tabs).toBeVisible()
  // the desktop mode selector is not shown
  await expect(page.getByRole('tablist', { name: 'Edit mode' })).toBeHidden()

  await tabs.getByRole('tab', { name: /Visual/ }).click()
  await expect(page.getByTestId('section').first()).toBeVisible()

  await tabs.getByRole('tab', { name: /Code/ }).click()
  await expect(page.locator('.cm-editor')).toBeVisible()
  await expect(page.locator('.cm-content')).toContainText('frontend http_in')

  await tabs.getByRole('tab', { name: /Assistant/ }).click()
  await expect(page.getByTestId('assistant-disabled')).toContainText('haro serve --assistant')
  await expect(page.getByTestId('assistant-disabled')).toContainText('Anthropic API')
  await expect(page.locator('.cm-editor')).toHaveCount(0)

  await tabs.getByRole('tab', { name: /Visual/ }).click()
  await expect(page.getByTestId('section').first()).toBeVisible()
  // no horizontal overflow
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('mobile with --assistant: the Assistant tab opens the chat without overflowing', async ({ page }) => {
  await page.goto('http://127.0.0.1:7474/')
  await page.getByRole('tablist', { name: 'Sections' }).getByRole('tab', { name: /Assistant/ }).click()
  await expect(page.getByTestId('chat-panel')).toBeVisible()
  await expect(page.getByLabel('Message for the assistant')).toBeVisible()
  await expect(page.getByTestId('assistant-disabled')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})
