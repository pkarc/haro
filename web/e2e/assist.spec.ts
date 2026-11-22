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

// ---- directives proposed for one section, from a description ----

const section = (page: Page, name = 'web_servers') =>
  page.getByTestId('section').filter({ has: page.getByRole('button', { name: `Rename backend ${name}` }) })

async function describeFor(page: Page, text: string, confirm = false, name = 'web_servers') {
  const box = page.getByLabel(`Describe the directive to add to backend ${name}`)
  await box.fill(text)
  await box.press('Enter')
  if (confirm) {
    const dlg = page.getByRole('dialog')
    await expect(dlg).toContainText('Anthropic API')
    await expect(dlg).toContainText('this section')
    await dlg.getByRole('button', { name: 'Send to Anthropic' }).click()
  }
}

async function openSections(page: Page) {
  await page.goto(ASSIST)
  await expect(page.getByTestId('section').first()).toBeVisible()
}

test('describe a directive: it shows as proposed, Approve turns it into a real row and into editor text', async ({ page }) => {
  await openSections(page)
  const sec = section(page)
  // the description field replaces the plain input, which stays one click away
  await expect(page.getByLabel('Add directive to backend web_servers')).toHaveCount(0)
  await expect(sec.getByRole('button', { name: 'Write the directive manually' })).toBeVisible()
  const before = await fullText(page)

  await describeFor(page, 'a timeout for idle servers', true)
  const row = sec.getByTestId('proposed')
  await expect(row).toHaveCount(1)
  await expect(row).toContainText('Proposed')
  await expect(row).toContainText('timeout server 30s')
  await expect(row).toContainText('idle')
  await expect(row.getByRole('button', { name: 'Approve' })).toBeVisible()
  await expect(row.getByRole('button', { name: 'Reject' })).toBeVisible()
  // not part of the code, not a real directive row yet
  expect(await fullText(page)).toBe(before)
  await expect(sec.getByTestId('directive').filter({ hasText: 'timeout server 30s' })).toHaveCount(0)

  await row.getByRole('button', { name: 'Approve' }).click()
  await expect(sec.getByTestId('proposed')).toHaveCount(0)
  await expect(sec.getByTestId('directive').filter({ hasText: 'timeout server 30s' })).toHaveCount(1)
  expect(await fullText(page)).toContain('    timeout server 30s')
  await expect(page.getByRole('status').filter({ hasText: '1 directive added' })).toBeVisible()
  // undo leaves the configuration as it was
  await page.getByRole('button', { name: 'Undo', exact: true }).last().click()
  expect(await fullText(page)).toBe(before)
})

test('the privacy notice is asked once (shared with the chat) and declining sends nothing', async ({ page }) => {
  await openSections(page)
  const box = page.getByLabel('Describe the directive to add to backend web_servers')
  await box.fill('something')
  await box.press('Enter')
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()
  await expect(section(page).getByTestId('proposed')).toHaveCount(0)
  await expect(section(page).getByTestId('proposal-pending')).toHaveCount(0)
  await expect(box).toHaveValue('something')
  await box.press('Enter')
  await page.getByRole('dialog').getByRole('button', { name: 'Send to Anthropic' }).click()
  await expect(section(page).getByTestId('proposed')).toBeVisible()
  // second time: no dialog
  await section(page).getByRole('button', { name: 'Reject', exact: true }).click()
  await describeFor(page, 'again')
  await expect(section(page).getByTestId('proposed')).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('Reject drops the proposal without touching the configuration', async ({ page }) => {
  await openSections(page)
  const before = await fullText(page)
  await describeFor(page, 'log requests', true)
  await expect(section(page).getByTestId('proposed')).toContainText('option httplog')
  await section(page).getByRole('button', { name: 'Reject', exact: true }).click()
  await expect(section(page).getByTestId('proposed')).toHaveCount(0)
  expect(await fullText(page)).toBe(before)
})

test('several proposals: Approve all applies them together, Reject all drops them', async ({ page }) => {
  await openSections(page)
  const sec = section(page)
  const before = await fullText(page)
  await describeFor(page, 'MANY things', true)
  await expect(sec.getByTestId('proposed')).toHaveCount(5)
  await expect(sec.getByTestId('section-proposed')).toContainText('5 proposed')
  await sec.getByRole('button', { name: 'Reject all' }).click()
  await expect(sec.getByTestId('proposed')).toHaveCount(0)
  expect(await fullText(page)).toBe(before)

  await describeFor(page, 'MANY things')
  await expect(sec.getByTestId('proposed')).toHaveCount(5)
  await sec.getByRole('button', { name: 'Approve all' }).click()
  await expect(sec.getByTestId('proposed')).toHaveCount(0)
  const text = await fullText(page)
  for (const d of ['timeout connect 5s', 'timeout client 30s', 'timeout server 30s', 'option http-server-close', 'retries 3']) {
    await expect(sec.getByTestId('directive').filter({ hasText: d })).toHaveCount(1)
    expect(text).toContain('    ' + d)
  }
  await expect(page.getByRole('status').filter({ hasText: '5 directives added' })).toBeVisible()
  // a single undo step reverts all of them
  await page.getByRole('button', { name: 'Undo', exact: true }).last().click()
  expect(await fullText(page)).toBe(before)
})

test('an unparseable answer shows a notice and no directives', async ({ page }) => {
  await openSections(page)
  await describeFor(page, 'BAD answer', true)
  const note = section(page).getByTestId('proposal-note')
  await expect(note).toContainText('did not propose any directive')
  await expect(section(page).getByTestId('proposed')).toHaveCount(0)
  await note.getByRole('button', { name: 'Dismiss' }).click()
  await expect(note).toHaveCount(0)
})

test('rejected credentials (401) show a readable error with retry', async ({ page }) => {
  await openSections(page)
  await describeFor(page, 'FAIL401', true)
  const err = section(page).getByTestId('proposal-error')
  await expect(err).toContainText(/credentials|credenciales/)
  await expect(err).toContainText('ANTHROPIC_API_KEY')
  await expect(err.getByRole('button', { name: 'Retry' })).toBeVisible()
  await err.getByRole('button', { name: 'Retry' }).click()
  await expect(err).toContainText('ANTHROPIC_API_KEY') // same answer again, still readable
})

test('waiting shows an indicator and Cancel stops it', async ({ page }) => {
  await openSections(page)
  await describeFor(page, 'SLOW one', true)
  const wait = section(page).getByTestId('proposal-pending')
  await expect(wait).toContainText('Waiting for the assistant')
  await wait.getByRole('button', { name: 'Cancel' }).click()
  await expect(wait).toHaveCount(0)
  await page.waitForTimeout(3300) // the late answer must not show up
  await expect(section(page).getByTestId('proposed')).toHaveCount(0)
})

test('a directive the section already has is not proposed again and the note says so', async ({ page }) => {
  await openSections(page)
  await describeFor(page, 'DUP it', true)
  const note = section(page).getByTestId('proposal-note')
  await expect(note).toContainText('already has')
  await expect(section(page).getByTestId('proposed')).toHaveCount(0)
})

test('«Write the directive manually» switches to the plain input with autocompletion, and back', async ({ page }) => {
  await openSections(page)
  const sec = section(page)
  await sec.getByRole('button', { name: 'Write the directive manually' }).click()
  const plain = page.getByLabel('Add directive to backend web_servers')
  await plain.fill('server web3 10.0.0.13:8080 check')
  await plain.press('Enter')
  await expect(sec.getByTestId('directive').filter({ hasText: 'web3' })).toHaveCount(1)
  await sec.getByRole('button', { name: 'Describe it to the assistant instead' }).click()
  const box = page.getByLabel('Describe the directive to add to backend web_servers')
  await expect(box).toBeVisible()
  // the description field has the same height as the plain input after switching back
  const h = async (l: typeof box) => Math.round((await l.boundingBox())!.height)
  expect(await h(box)).toBe(34)
  await box.fill('one\ntwo\nthree')
  expect(await h(box)).toBeGreaterThan(34)
  await box.fill('')
  expect(await h(box)).toBe(34)
})

test('Shift+Enter makes a new line instead of sending', async ({ page }) => {
  await openSections(page)
  const box = page.getByLabel('Describe the directive to add to backend web_servers')
  await box.fill('first')
  await box.press('Shift+Enter')
  await box.type('second')
  await expect(box).toHaveValue('first\nsecond')
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('with the assistant open at 1280px it overlays the page as a drawer and can be closed', async ({ page }) => {
  await openChat(page)
  const split = page.getByTestId('split')
  const w = (await split.boundingBox())!.width
  expect(w).toBeGreaterThan(1100) // not squeezed by a third column
  await page.getByRole('button', { name: 'Close assistant' }).click()
  await expect(page.getByTestId('chat-panel')).toHaveCount(0)
})

test('without --assistant the plain directive input is kept and there is no description field', async ({ page }) => {
  await page.goto(PLAIN)
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByLabel('Add directive to backend web_servers')).toBeVisible()
  await expect(page.getByLabel(/Describe the directive to add/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Write the directive manually' })).toHaveCount(0)
})
