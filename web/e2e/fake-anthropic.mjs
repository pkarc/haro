// Fake Anthropic API for the assistant e2e tests. Usage: node e2e/fake-anthropic.mjs <port>
// Emulates POST /v1/messages with SSE streaming (same format as the real API).
// It never talks to the network: it only listens on 127.0.0.1.
//   - request with «FAIL401»: answers 401 (credentials rejected).
//   - request with «SLOW»: answers slowly and never finishes (to test «Stop»).
//   - non-streaming «propose directives» call (the user message holds a <section> block; the
//     instruction follows </haro_context>) answers a JSON object, by instruction:
//     «FAIL401» -> 401; «SLOW» -> 3 s delay; «BAD» -> unparseable text; «MANY» -> 5 directives;
//     «DUP» -> a directive the section already has; «timeout» -> `timeout server 30s`;
//     anything else -> `option httplog`.
//   - anything else: explains and proposes raising `maxconn` from 4096 to 8192 in example.cfg.
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

const proposal = () => 'Raising **maxconn** to `8192` in the `global` section.\n\n```haproxy\n' + example.replace('maxconn 4096', 'maxconn 8192') + '\n```\n\nReview the diff before applying.\n'

// First directive of the section in the context (lines look like «12: backend app» / «13:     balance roundrobin»).
const firstDirective = ctx => {
  const body = ctx.split('<section ')[1]?.split('</section>')[0] ?? ''
  const lines = body.split('\n').slice(2) // skip the tag line and the header
  for (const l of lines) {
    const m = /^\d+: +(\S.*)$/.exec(l)
    if (m && !m[1].startsWith('#')) return m[1]
  }
  return 'option httplog'
}

async function propose(request, ctx, res, stream) {
  if (request.includes('SLOW')) await sleep(3000)
  let text
  if (request.includes('BAD')) text = 'Sorry, I cannot help with that.'
  else {
    let directives
    if (request.includes('MANY')) directives = ['timeout connect 5s', 'timeout client 30s', 'timeout server 30s', 'option http-server-close', 'retries 3'].map(t => ({ text: t, explanation: `Sets \`${t}\`.` }))
    else if (request.includes('DUP')) directives = [{ text: firstDirective(ctx), explanation: 'Already in the section.' }]
    else if (request.includes('timeout')) directives = [{ text: 'timeout server 30s', explanation: 'Closes **idle** server connections after `30s`.' }]
    else directives = [{ text: 'option httplog', explanation: 'Logs HTTP requests.' }]
    text = '```json\n' + JSON.stringify({ directives, note: '' }) + '\n```'
  }
  if (stream) {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
    res.end(sse([start, textStart, delta(text), ...end('end_turn')]))
    return
  }
  res.writeHead(200, { 'content-type': 'application/json' })
  res.end(JSON.stringify({ id: 'msg_fake', type: 'message', role: 'assistant', model: 'claude-opus-5-5', content: [{ type: 'text', text }], stop_reason: 'end_turn', stop_sequence: null, usage: { input_tokens: 10, output_tokens: 20 } }))
}

createServer(async (req, res) => {
  if (req.method !== 'POST' || req.url?.split('?')[0] !== '/v1/messages') { res.writeHead(404).end(); return }
  let body = ''
  for await (const c of req) body += c
  const parsed = JSON.parse(body)
  const msgs = parsed.messages ?? []
  const last = msgs[msgs.length - 1]
  const asked = typeof last?.content === 'string' ? last.content : (last?.content ?? []).map(b => b.text ?? '').join('')
  const request = asked.split('</haro_context>').pop() // what the user typed, without the context

  if (request.includes('FAIL401')) {
    res.writeHead(401, { 'content-type': 'application/json' })
    res.end(JSON.stringify({ type: 'error', error: { type: 'authentication_error', message: 'invalid x-api-key' } }))
    return
  }
  if (asked.includes('<section index=')) { await propose(request, asked, res, parsed.stream === true); return }
  res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
  res.write(sse([start, textStart]))
  const slow = request.includes('SLOW')
  const text = slow ? 'Thinking about the answer '.repeat(60) : proposal()
  const chunks = slow ? text.match(/.{1,25}/g) : text.match(/(?:[^\n]*\n){1,8}|[^\n]+$/g)
  let gone = false
  res.on('close', () => { gone = true })
  for (const c of chunks) {
    if (gone) return
    res.write(sse([delta(c)]))
    await sleep(slow ? 250 : 100)
  }
  res.end(sse(end('end_turn')))
}).listen(port, '127.0.0.1', () => console.log(`fake-anthropic on 127.0.0.1:${port}`))
