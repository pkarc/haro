import { expect, type Page } from '@playwright/test'

/**
 * Edits a directive the way the UI now works: click its row in the visual view
 * (the cursor lands on that line in the code editor, which gets focus), then type
 * the replacement in the editor. The whole line is selected, so the indentation
 * of the original is kept explicitly.
 */
export async function editDirectiveInCode(page: Page, from: string, to: string) {
  const row = page.getByTestId('directive').filter({ hasText: from }).first()
  await row.locator('.dir-main').click()
  const target = page.locator('.cm-line.cm-haro-target')
  await expect(target).toHaveCount(1)
  await expect(target).toContainText(from)
  const raw = (await target.textContent()) ?? ''
  const indent = /^\s*/.exec(raw)![0]
  await page.keyboard.type(indent + to)
  await expect(page.getByTestId('directive').filter({ hasText: to }).first()).toBeVisible()
}
