// Smoke test of the WASM module in Node: loads the .wasm with its wasm_exec.js
// and exercises the main calls.
//   node wasm/smoke.mjs [directory with haro.wasm and wasm_exec.js]
import { readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import assert from 'node:assert/strict'
import vm from 'node:vm'

const dir = resolve(process.argv[2] ?? 'web/public/wasm')
const code = readFileSync(join(dir, 'wasm_exec.js'), 'utf8')
vm.runInThisContext(code)
const go = new globalThis.Go()
const { instance } = await WebAssembly.instantiate(readFileSync(join(dir, 'haro.wasm')), go.importObject)
go.run(instance)
await new Promise(r => setTimeout(r, 50))

const call = (method, req) => JSON.parse(globalThis.haroCore.call(method, JSON.stringify(req)))
const cfg = 'frontend web\n    bind *:80\n    default_backend nope\n\nbackend app\n    server a1 10.0.0.1:80 check\n'

const a = call('analyze', { files: [{ path: 'a.cfg', text: cfg }], lang: 'en' }).result
assert.ok(a.issues.some(i => i.rule === 'backend-missing'), 'analyze must detect backend-missing')
assert.match(a.issues.find(i => i.rule === 'backend-missing').message, /does not exist/)
assert.equal(a.doc.sections.length, 2)

// Default language is English; Spanish only when requested.
const def = call('analyze', { text: cfg }).result
assert.match(def.issues.find(i => i.rule === 'backend-missing').message, /does not exist/)
const es = call('analyze', { text: cfg, lang: 'es' }).result
assert.match(es.issues.find(i => i.rule === 'backend-missing').message, /no existe/)

const ap = call('apply', { files: [{ path: 'a.cfg', text: cfg }], ops: [{ op: 'add_directive', section: 1, text: 'balance leastconn' }] }).result
assert.equal(ap.changes.length, 1)
assert.match(ap.files[0].text, /    balance leastconn\n/)

const bad = call('apply', { text: cfg, ops: [{ op: 'add_directive', section: 0, text: 'backend x' }] })
assert.equal(bad.error.code, 'invalid_argument')

const d = call('diff', { before: 'a\nb\n', after: 'a\nc\n' }).result
assert.ok(d.ops.some(o => o.op === '+' && o.text === 'c'))

const k = call('knowledge', { lang: 'en' }).result
assert.ok(Object.keys(k.directives).length > 100)

const t = call('listTemplates', { lang: 'en' }).result
assert.ok(t.templates.some(x => x.id === 'nextcloud'))
const r = call('renderTemplate', { id: 'nextcloud', lang: 'en', files: [{ path: 'a.cfg', text: cfg }], params: { domain: 'cloud.example.com', servers: ['10.0.0.10:80'], cert: '/etc/haproxy/certs/c.pem' } })
assert.ok(r.result, JSON.stringify(r))
assert.match(r.result.files[0].text, /backend nextcloud/)
assert.equal(call('renderTemplate', { id: 'nextcloud', params: {} }).error.code, 'invalid_argument')

assert.equal(call('nada', {}).error.code, 'unimplemented')
console.log('wasm smoke OK')
process.exit(0)
