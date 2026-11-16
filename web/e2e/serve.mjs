// Arranca `haro serve` para las pruebas e2e. Uso: node e2e/serve.mjs example|multi|assist <puerto>
// «assist» sirve example.cfg con --assistant contra la API falsa (puerto 7476; sin --allow-write).
// «multi» trabaja sobre una copia temporal de core/testdata/multiok con --allow-write,
// nunca sobre el original.
import { spawn } from 'node:child_process'
import { cpSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const bin = process.env.HARO_BIN || join(root, 'bin', 'haro')
const [kind, port] = process.argv.slice(2)
let args, tmp, env = process.env
if (kind === 'multi') {
  tmp = mkdtempSync(join(tmpdir(), 'haro-e2e-'))
  cpSync(join(root, 'core/testdata/multiok'), tmp, { recursive: true })
  args = ['serve', '--addr', `127.0.0.1:${port}`, '--file', tmp, '--allow-write']
} else if (kind === 'assist') {
  args = ['serve', '--addr', `127.0.0.1:${port}`, '--file', join(root, 'core/testdata/example.cfg'), '--assistant']
  env = { ...process.env, ANTHROPIC_BASE_URL: 'http://127.0.0.1:7476', ANTHROPIC_API_KEY: 'test' }
} else {
  args = ['serve', '--addr', `127.0.0.1:${port}`, '--file', join(root, 'core/testdata/example.cfg')]
}
const child = spawn(bin, args, { stdio: 'inherit', env })
const stop = () => { child.kill(); if (tmp) rmSync(tmp, { recursive: true, force: true }) }
process.on('SIGTERM', () => { stop(); process.exit(0) })
process.on('SIGINT', () => { stop(); process.exit(0) })
child.on('exit', code => { if (tmp) rmSync(tmp, { recursive: true, force: true }); process.exit(code ?? 0) })
