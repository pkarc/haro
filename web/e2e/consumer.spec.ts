import { expect, test } from '@playwright/test'
import { editDirectiveInCode } from './helpers'

// An external consumer (examples/consumer) mounts the application using only
// the public API of src/index: its own ConfigStore, a previous screen, a header
// and an action button. Requires `npm run build:consumer` (and `make wasm`).
const BASE = 'http://127.0.0.1:7477/'

type StoreProbe = { saved: { path: string; text: string }[][]; remoteChange(text: string): void }
const probe = <T,>(page: import('@playwright/test').Page, fn: (s: StoreProbe) => T) =>
  page.evaluate(`(${fn.toString()})(window.__store)`) as Promise<T>

test('the consumer mounts the app with its store, its gate, its header and its action', async ({ page }) => {
  await page.goto(BASE)
  // The previous screen comes first and the app does not exist yet.
  await expect(page.getByTestId('gate')).toBeVisible()
  await expect(page.getByTestId('section')).toHaveCount(0)
  await page.getByTestId('gate-enter').click()

  // It enters with the store chosen by the gate: its content, its header and its button.
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByTestId('section').first()).toContainText('api')
  await expect(page.getByTestId('consumer-header')).toBeVisible()
  await expect(page.getByTestId('consumer-action')).toBeVisible()

  // Editing and saving goes through the consumer's store, with the full text.
  await editDirectiveInCode(page, 'balance roundrobin', 'balance leastconn')
  await expect(page.getByTestId('save-button')).toContainText('Save (1)')
  await page.getByTestId('save-button').click()
  await expect(page.getByRole('dialog')).toContainText('memory/demo@main')
  await expect(page.getByRole('dialog')).toContainText('Saved to the main branch')
  await page.getByTestId('confirm-save').click()
  await expect(page.getByText('Saved', { exact: true })).toBeVisible()
  const saved = await probe(page, s => s.saved)
  expect(saved).toHaveLength(1)
  expect(saved[0][0].text).toContain('balance leastconn')
})

test('a store version conflict is shown and resolved by reloading', async ({ page }) => {
  await page.goto(BASE)
  await page.getByTestId('gate-enter').click()
  await expect(page.getByTestId('section').first()).toBeVisible()
  await editDirectiveInCode(page, 'balance roundrobin', 'balance source')

  // Somebody else changes the «repository» before saving.
  await probe(page, s => s.remoteChange('backend api\n    balance first\n    server a1 10.0.0.1:80 check\n    server a2 10.0.0.2:80 check\n'))
  await page.getByTestId('save-button').click()
  await page.getByTestId('confirm-save').click()
  await expect(page.getByTestId('save-error')).toContainText('Conflict')
  expect(await probe(page, s => s.saved.length)).toBe(0)

  // Reloading brings the remote and leaves the local state in the history.
  await page.getByTestId('reload-store').click()
  await expect(page.getByTestId('directive').filter({ hasText: 'balance first' }).first()).toBeVisible()
  await expect(page.getByTestId('directive').filter({ hasText: 'server a2' }).first()).toBeVisible()
})
