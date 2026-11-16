// API falsa de Anthropic para las pruebas e2e del asistente. Uso: node e2e/fake-anthropic.mjs <puerto>
// Emula POST /v1/messages con streaming SSE (mismo formato que la API real).
// Nunca habla con la red: solo escucha en 127.0.0.1.
//   - petición con «FALLA401»: responde 401 (credenciales rechazadas).
//   - petición con «LENTO»: responde despacio y sin terminar (para probar «Detener»).
//   - cualquier otra: explica y propone subir `maxconn` de 4096 a 8192 en example.cfg.
import { createServer } from 'node:http'
import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const example = readFileSync(join(root, 'core/testdata/example.cfg'), 'utf8').replace(/\n$/, '')
const port = Number(process.argv[2] || 7476)

const sse = events => events.map(e => `event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`).join('')
const sleep = ms => new Promise(r => setTimeout(r, ms))
const start = { type: 'message_start', message: { id: 'msg_fake', type: 'message', role: 'assistant', model: 'claude-opus-5-5', content: [], stop_reason: null, stop_sequence: null, usage: { input_tokens: 10, output_tokens: 1 } } }
const textStart = { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }
const delta = text => ({ type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text } })
const end = reason => [{ type: 'content_block_stop', index: 0 }, { type: 'message_delta', delta: { stop_reason: reason, stop_sequence: null }, usage: { output_tokens: 20 } }, { type: 'message_stop' }]

const proposal = () => 'Subo **maxconn** a `8192` en la sección `global`.\n\n```haproxy\n' + example.replace('maxconn 4096', 'maxconn 8192') + '\n```\n\nRevisa el diff antes de aplicar.\n'

createServer(async (req, res) => {
  if (req.method !== 'POST' || req.url?.split('?')[0] !== '/v1/messages') { res.writeHead(404).end(); return }
  let body = ''
  for await (const c of req) body += c
  const msgs = JSON.parse(body).messages ?? []
  const last = msgs[msgs.length - 1]
  const asked = typeof last?.content === 'string' ? last.content : (last?.content ?? []).map(b => b.text ?? '').join('')
  const request = asked.split('</haro_context>').pop() // lo que escribió el usuario, sin el contexto

  if (request.includes('FALLA401')) {
    res.writeHead(401, { 'content-type': 'application/json' })
    res.end(JSON.stringify({ type: 'error', error: { type: 'authentication_error', message: 'invalid x-api-key' } }))
    return
  }
  res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
  res.write(sse([start, textStart]))
  const slow = request.includes('LENTO')
  const text = slow ? 'Pensando en la respuesta '.repeat(60) : proposal()
  const chunks = slow ? text.match(/.{1,25}/g) : text.match(/(?:[^\n]*\n){1,8}|[^\n]+$/g)
  let gone = false
  res.on('close', () => { gone = true })
  for (const c of chunks) {
    if (gone) return
    res.write(sse([delta(c)]))
    await sleep(slow ? 250 : 100)
  }
  res.end(sse(end('end_turn')))
}).listen(port, '127.0.0.1', () => console.log(`fake-anthropic en 127.0.0.1:${port}`))
