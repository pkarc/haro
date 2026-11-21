import { expect, test } from '@playwright/test'
import { editDirectiveInCode as editDirective } from './helpers'

const BASE = 'http://127.0.0.1:7472'

test.describe.configure({ mode: 'serial' })

test('several files: tabs, edit and save', async ({ page, request }) => {
  await page.goto(BASE + '/')
  await expect(page.getByTestId('section').first()).toBeVisible()
  await expect(page.getByRole('tab', { name: /10-global\.cfg/ })).toBeVisible()
  await expect(page.getByRole('tab', { name: /20-web\.cfg/ })).toBeVisible()
  await expect(page.getByRole('tab', { name: /30-api\.cfg/ })).toBeVisible()
  await page.getByRole('tab', { name: /20-web\.cfg/ }).click()
  await expect(page.locator('.cm-content')).toContainText('frontend web')

  await editDirective(page, 'maxconn 4096', 'maxconn 2048')
  await page.getByTestId('save-button').click()
  await expect(page.getByTestId('diff')).toContainText('maxconn 2048')
  await page.getByTestId('confirm-save').click()
  await expect(page.getByText('Saved', { exact: true })).toBeVisible()
  const disk = await (await request.post(BASE + '/haro.v1.ConfigService/GetConfig', { data: {} })).json()
  expect(disk.files[0].text).toContain('maxconn 2048')
  expect(disk.files[1].text).not.toContain('maxconn 2048')
})

test('a save that breaks a reference between files is rejected', async ({ page, request }) => {
  await page.goto(BASE + '/')
  await expect(page.getByTestId('section').first()).toBeVisible()
  // 20-web.cfg uses «api», defined in 30-api.cfg; pointing it at a missing backend breaks the reference.
  await editDirective(page, 'use_backend api if is_api', 'use_backend missing if is_api')
  await page.getByTestId('save-button').click()
  await page.getByTestId('confirm-save').click()
  await expect(page.getByTestId('save-error')).toBeVisible()
  const disk = await (await request.post(BASE + '/haro.v1.ConfigService/GetConfig', { data: {} })).json()
  expect(disk.files[1].text).toContain('use_backend api if is_api')
  expect(disk.files[1].text).not.toContain('missing')
})

test('renaming with references across files is saved at once', async ({ page, request }) => {
  await page.goto(BASE + '/')
  await expect(page.getByTestId('section').first()).toBeVisible()
  await page.getByRole('button', { name: 'Rename backend api' }).click()
  await page.getByRole('dialog').getByRole('textbox').fill('api2')
  await page.getByRole('dialog').getByRole('button', { name: 'OK', exact: true }).click()
  await expect(page.getByTestId('save-button')).toContainText('Save (2)')
  await page.getByTestId('save-button').click()
  await page.getByTestId('confirm-save').click()
  await expect(page.getByText('Saved 2 files')).toBeVisible()
  const disk = await (await request.post(BASE + '/haro.v1.ConfigService/GetConfig', { data: {} })).json()
  expect(disk.files[1].text).toContain('use_backend api2 if is_api')
  expect(disk.files[2].text).toContain('backend api2')
})
