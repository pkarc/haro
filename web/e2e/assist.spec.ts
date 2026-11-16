import { expect, test, type Page } from '@playwright/test'

// Assistant with the fake Anthropic API (e2e/fake-anthropic.mjs) and haro serve
// --assistant over example.cfg, without --allow-write: nothing is written to disk.
const ASSIST = 'http://127.0.0.1:7474/'
const PLAIN = 'http://127.0.0.1:7471/' // without --assistant

// Full text of the active file (CodeMirror only renders what is visible): read from Export.
async function fullText(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Export' }).click()
  const text = await page.getByLabel('Content to export').inputValue()
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).last().click()
  return text
}

async function openChat(page: Page) {
  await page.goto(ASSIST)
  await expect(page.getByTestId('section').first()).toBeVisible()
  await page.getByTestId('assistant-toggle').click()
  await expect(page.getByTestId('chat-panel')).toBeVisible()
}

async function ask(page: Page, text: string, confirm = false) {
  await page.getByLabel('Message for the assistant').fill(text)
  await page.getByLabel('Message for the assistant').press('Enter')
  if (confirm) {
    const dlg = page.getByRole('dialog')
    await expect(dlg).toContainText('Anthropic API')
    await expect(dlg).toContainText('claude-opus-5-5')
    await dlg.getByRole('button', { name: 'Send to Anthropic' }).click()
  }
}

test.beforeEach(async ({ page }) => { await page.addInitScript(() => { if (!sessionStorage.getItem('e2e-clean')) { localStorage.clear(); sessionStorage.setItem('e2e-clean', '1') } }) })

test('ask, see the streaming and the proposal with a diff, apply and undo', async ({ page }) => {
  await openChat(page)
  await expect(page.getByTestId('chat-privacy')).toContainText('Anthropic API')
  const before = await fullText(page)
  expect(before).toContain('maxconn 4096')

  await ask(page, 'Raise maxconn to 8192', true)
  await expect(page.getByTestId('msg-user')).toContainText('Raise maxconn to 8192')
  // streaming: the text arrives in chunks and the response is still in progress
  await expect(page.getByTestId('msg-assistant')).toContainText('maxconn')
  await expect(page.getByRole('button', { name: 'Stop' })).toBeVisible()
  // the configuration block is never rendered in full in the chat
  await expect(page.getByTestId('chat-log')).not.toContainText('frontend http_in')

  const card = page.getByTestId('proposal')
  await expect(card).toBeVisible()
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible()
  await expect(card.locator('.dl.da')).toContainText('maxconn 8192')
  await expect(card.locator('.dl.dd')).toContainText('maxconn 4096')
  await expect(page.getByTestId('proposal-issues')).toContainText('After applying: 0 errors')
  await expect(page.getByTestId('chat-log')).toContainText('Review the diff before applying')

  await card.getByRole('button', { name: 'Apply', exact: true }).click()
  await expect(card).toHaveAttribute('data-status', 'applied')
  expect(await fullText(page)).toBe(before.replace('maxconn 4096', 'maxconn 8192'))
  // previous version in the history
  await page.getByRole('button', { name: 'History' }).click()
  await expect(page.getByTestId('version').first()).toContainText('Before applying assistant proposal')
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).last().click()

  await page.getByRole('button', { name: 'Undo', exact: true }).last().click()
  expect(await fullText(page)).toBe(before)
})

test('the chat history survives a reload and «New conversation» clears it', async ({ page }) => {
  await openChat(page)
  await ask(page, 'Raise maxconn', true)
  await expect(page.getByTestId('proposal')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByTestId('chat-panel')).toBeVisible()
  await expect(page.getByTestId('msg-user')).toContainText('Raise maxconn')
  await expect(page.getByTestId('proposal')).toBeVisible()
  await page.getByRole('button', { name: 'New conversation' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'New conversation' }).click()
  await expect(page.getByTestId('msg-user')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Review my configuration/ })).toBeVisible()
})

test('Stop cuts the response and keeps what was received', async ({ page }) => {
  await openChat(page)
  await ask(page, 'SLOW please', true)
  await expect(page.getByTestId('msg-assistant')).toContainText('Thinking')
  await page.getByRole('button', { name: 'Stop' }).click()
  await expect(page.getByTestId('msg-assistant')).toContainText('(stopped)')
  await expect(page.getByTestId('msg-assistant')).toContainText('Thinking')
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible()
})

test('with rejected credentials (401) it shows a readable error with retry', async ({ page }) => {
  await openChat(page)
  await ask(page, 'FAIL401', true)
  const err = page.getByTestId('chat-error')
  await expect(err).toContainText(/credentials|credenciales/)
  await expect(err).toContainText('ANTHROPIC_API_KEY')
  await expect(err.getByRole('button', { name: 'Retry' })).toBeVisible()
})

test('a stale proposal asks for confirmation before being applied', async ({ page }) => {
  await openChat(page)
  await ask(page, 'Raise maxconn', true)
  const card = page.getByTestId('proposal')
  await expect(card).toBeVisible()
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible()
  // the user edits the configuration after requesting the proposal
  const edited = (await fullText(page)).replace('maxconn 4096', 'maxconn 4096\n    nbthread 2')
  await page.getByRole('button', { name: 'Import' }).click()
  await page.getByRole('dialog').getByLabel('Text to import').fill(edited)
  await page.getByRole('dialog').getByRole('button', { name: 'Replace' }).click()
  await expect(page.getByTestId('proposal-stale')).toBeVisible()
  await card.getByRole('button', { name: 'Apply', exact: true }).click()
  const dlg = page.getByRole('dialog')
  await expect(dlg).toContainText('has changed')
  await dlg.getByRole('button', { name: 'Cancel' }).click()
  await expect(card).toHaveAttribute('data-status', 'pending')
  await card.getByRole('button', { name: 'Apply', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Apply anyway' }).click()
  await expect(card).toHaveAttribute('data-status', 'applied')
  expect(await fullText(page)).not.toContain('nbthread')
})

test('discarding the proposal does not touch the configuration', async ({ page }) => {
  await openChat(page)
  const before = await fullText(page)
  await ask(page, 'Raise maxconn', true)
  const card = page.getByTestId('proposal')
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible()
  await card.getByRole('button', { name: 'Discard' }).click()
  await expect(card).toHaveAttribute('data-status', 'discarded')
  expect(await fullText(page)).toBe(before)
})

test('without --assistant there is no assistant button or panel', async ({ page }) => {
  await page.goto(PLAIN)
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByTestId('assistant-toggle')).toHaveCount(0)
  await expect(page.getByTestId('chat-panel')).toHaveCount(0)
})
