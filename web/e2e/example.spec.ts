import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const BASE = 'http://127.0.0.1:7471/'
const original = readFileSync(join(resolve(dirname(fileURLToPath(import.meta.url)), '..', '..'), 'core/testdata/example.cfg'), 'utf8')

// Full text of the active file: CodeMirror only renders the visible lines, so it
// is read from the Export dialog (the same text the code mode shows).
async function fullText(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Export' }).click()
  const text = await page.getByLabel('Content to export').inputValue()
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).last().click()
  return text
}

test.beforeEach(async ({ page }) => {
  await page.goto(BASE)
  await expect(page.getByTestId('section').first()).toBeVisible()
})

test('loads the example: sections, map and problems', async ({ page }) => {
  await expect(page.getByTestId('section')).toHaveCount(8)
  await expect(page.getByTestId('map-entry')).toHaveCount(3)
  await expect(page.getByTestId('map-backend')).toHaveCount(3)
  await expect(page.getByTestId('problems-button')).toContainText('1 suggestion')
  await page.getByTestId('problems-button').click()
  await expect(page.getByTestId('problem')).toHaveCount(1)
  await expect(page.getByTestId('problem')).toContainText('placeholder')
  // explanation from the core in the card
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first()).toContainText('4096')
})

test('the code shows the problem as a diagnostic and the line explanation', async ({ page }) => {
  // CodeMirror only renders the visible lines: jump to the problem with «Go» in the panel.
  await page.getByRole('tab', { name: 'Code' }).click()
  await page.getByTestId('problems-button').click()
  await page.getByTestId('problem').getByRole('button', { name: 'Go' }).click()
  await expect(page.locator('.cm-editor')).toBeVisible()
  await expect(page.locator('.cm-lint-marker').first()).toBeVisible()
  await page.locator('.cm-line', { hasText: 'stats auth' }).click()
  await expect(page.getByTestId('code-status')).toContainText('Suggestion')
  await expect(page.getByTestId('code-status')).toContainText('stats')
  await page.getByTestId('code-editor').locator('.cm-line', { hasText: 'stats uri' }).click()
  await expect(page.getByTestId('code-status')).toContainText('/stats')
})

test('map: clicking a node goes to its section', async ({ page }) => {
  await page.getByTestId('map-backend').filter({ hasText: 'static_servers' }).click()
  const sec = page.getByTestId('section').filter({ hasText: 'static_servers' }).first()
  await expect(sec).toHaveClass(/./)
  await expect(page.locator('.flash')).toHaveCount(1)
})

test('editing in visual mode changes the text minimally', async ({ page }) => {
  // 1) change a directive
  await page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first().locator('.dir-main').click()
  await page.getByLabel('Edit directive').fill('maxconn 8192')
  await page.getByLabel('Edit directive').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 8192' })).toHaveCount(1)
  // 2) add a server
  await page.getByLabel('Add directive to backend web_servers').fill('server web3 10.0.0.13:8080 check')
  await page.getByLabel('Add directive to backend web_servers').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'web3' })).toHaveCount(1)
  // 3) rename a backend with references
  await page.getByRole('button', { name: 'Rename backend api_servers' }).click()
  await page.getByRole('dialog').getByRole('textbox').fill('api_v2')
  await page.getByRole('dialog').getByRole('button', { name: 'OK', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Rename backend api_v2' })).toBeVisible()

  await page.getByRole('tab', { name: 'Code' }).click()
  await expect(page.locator('.cm-content')).toContainText('maxconn 8192')
  const text = await fullText(page)
  const expected = original.split('\n')
    .map(l => l === '    maxconn 4096' ? '    maxconn 8192' : l.replace('api_servers', 'api_v2'))
  const at = expected.indexOf('    server web2 10.0.0.12:8080 check cookie web2')
  expected.splice(at + 1, 0, '    server web3 10.0.0.13:8080 check')
  expect(text).toBe(expected.join('\n'))
})

test('undo and redo from the buttons and with Ctrl+Z', async ({ page }) => {
  await page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first().locator('.dir-main').click()
  await page.getByLabel('Edit directive').fill('maxconn 1')
  await page.getByLabel('Edit directive').press('Enter')
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 1' })).toHaveCount(1)
  await page.getByRole('button', { name: 'Undo' }).click()
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' })).toHaveCount(1)
  await page.getByRole('button', { name: 'Redo' }).click()
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 1' })).toHaveCount(1)
  await page.locator('body').press('Control+z')
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' })).toHaveCount(1)
})

test('without --allow-write, saving warns that there is no permission', async ({ page }) => {
  await page.getByTestId('save-button').click()
  await expect(page.getByRole('alert').filter({ hasText: '--allow-write' })).toBeVisible()
})

test('add a section from a template', async ({ page }) => {
  await page.getByRole('button', { name: '+ resolvers' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Add', exact: true }).click()
  await expect(page.getByTestId('section')).toHaveCount(9)
})

test('language: switching to Spanish changes the interface and re-analyzes with Spanish texts', async ({ page }) => {
  const maxconn = page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first()
  await expect(maxconn).toContainText('Maximum simultaneous connections for the whole process')
  await expect(page.getByRole('button', { name: 'Export' })).toBeVisible()
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByLabel('Language').selectOption('es')
  // The dialog itself switches language at once.
  const dialog = page.getByRole('dialog', { name: 'Ajustes' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Cerrar', exact: true }).first().click()
  // Interface...
  await expect(page.getByRole('button', { name: 'Exportar' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Código' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Export', exact: true })).toHaveCount(0)
  // ...and the texts produced by the core (explanations, detector messages, knowledge).
  await expect(maxconn).toContainText('Máximo de conexiones simultáneas')
  await expect(maxconn).not.toContainText('Maximum')
  await page.getByTestId('problems-button').click()
  await expect(page.getByTestId('problems-button')).toContainText('1 sugerencia')
  await expect(page.getByTestId('problem')).toContainText('parece de ejemplo')
  await expect(page.getByTestId('section').first()).toContainText('Ajustes de todo el proceso HAProxy')
  // The choice survives a reload (and the document language follows it).
  await page.reload()
  await expect(page.getByRole('button', { name: 'Exportar' })).toBeVisible()
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first()).toContainText('Máximo de conexiones simultáneas')
  await expect(page.locator('html')).toHaveAttribute('lang', 'es')
  // And back to English.
  await page.getByRole('button', { name: 'Ajustes' }).click()
  await page.getByLabel('Idioma').selectOption('en')
  await page.getByRole('dialog', { name: 'Settings' }).getByRole('button', { name: 'Close', exact: true }).first().click()
  await expect(page.getByRole('button', { name: 'Export' })).toBeVisible()
  await expect(page.getByTestId('directive').filter({ hasText: 'maxconn 4096' }).first()).toContainText('Maximum simultaneous connections')
})

test('adding Nextcloud from a technology template changes the text minimally', async ({ page }) => {
  await page.getByTestId('template-button').click()
  const dialog = page.getByRole('dialog', { name: 'Add from technology template' })
  await expect(dialog).toBeVisible()
  const add = dialog.getByRole('button', { name: 'Add to the configuration' })
  await expect(add).toBeDisabled()
  await dialog.getByLabel('Technology').selectOption('nextcloud')
  // Without the required ones, the preview asks for them to be filled in.
  await expect(page.getByTestId('template-preview')).toContainText('Fill in')
  await dialog.getByLabel('Public domain').fill('cloud.example.com')
  await dialog.getByLabel('Nextcloud servers (host:port)').fill('10.0.0.10:80')
  await dialog.getByLabel('TLS certificate (.pem)').fill('/etc/haproxy/certs/cloud.pem')
  // An invalid parameter shows the core's error and does not allow confirming.
  await dialog.getByLabel('Nextcloud servers (host:port)').fill('10.0.0.10')
  await expect(page.getByRole('alert').filter({ hasText: 'is not a valid server' })).toBeVisible()
  await expect(add).toBeDisabled()
  await dialog.getByLabel('Nextcloud servers (host:port)').fill('10.0.0.10:80')
  // Diff from the core before accepting.
  const diff = page.getByTestId('template-preview').getByTestId('diff')
  await expect(diff).toContainText('use_backend nextcloud if host_nextcloud')
  await expect(diff.locator('[data-op="+"]').filter({ hasText: 'server nc1 10.0.0.10:80 check' })).toHaveCount(1)
  await add.click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('section')).toHaveCount(9)
  await expect(page.getByRole('status').filter({ hasText: 'added' })).toBeVisible()

  const backend = [
    'backend nextcloud',
    '    # Nextcloud: generated with the haro template',
    '    mode http',
    '    option forwardfor',
    '    http-request set-header X-Forwarded-Proto https',
    '    # CardDAV and CalDAV: automatic client discovery',
    '    http-request redirect location /remote.php/dav/ code 301 if { path /.well-known/carddav /.well-known/caldav }',
    '    http-response set-header Strict-Transport-Security "max-age=15552000; includeSubDomains"',
    '    timeout server 10m',
    '    option httpchk',
    '    http-check send meth GET uri /status.php ver HTTP/1.1 hdr Host cloud.example.com',
    '    http-check expect status 200',
    '    server nc1 10.0.0.10:80 check',
  ].join('\n')
  const cert = /crt (\S+) alpn/.exec(original)![1] // the certificate the example already uses
  const expected = original
    .replace(`crt ${cert} alpn`, `crt ${cert} crt /etc/haproxy/certs/cloud.pem alpn`)
    .replace('    default_backend web_servers\n', '    acl host_nextcloud hdr(host),field(1,:) -i cloud.example.com\n    use_backend nextcloud if host_nextcloud\n    default_backend web_servers\n')
    .replace(/\n*$/, '\n\n' + backend + '\n')
  expect(await fullText(page)).toBe(expected)

  // The previous version is kept and «Undo» brings the original back.
  await page.getByRole('button', { name: 'Undo' }).first().click()
  await expect(page.getByTestId('section')).toHaveCount(8)
  expect(await fullText(page)).toBe(original)
})
