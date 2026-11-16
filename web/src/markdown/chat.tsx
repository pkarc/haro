import type { ReactNode } from 'react'
import { Rich } from './mini'

// Markdown de las respuestas del asistente: párrafos, listas, títulos, bloques
// de código y los inline de mini.tsx (**negrita** y `código`). Se interpreta a
// mano y se pinta con elementos React: nunca se inserta HTML crudo.
//
// Los bloques de configuración completa (```haproxy …) NO se pintan en el chat:
// el servidor los convierte en la propuesta (tarjeta con diff por archivo).

export type ChatBlock =
  | { t: 'p'; v: string }
  | { t: 'h'; v: string }
  | { t: 'ul' | 'ol'; items: string[] }
  | { t: 'code'; lang: string; v: string; open: boolean }
  | { t: 'config'; lines: number; open: boolean }

/** Lenguajes de bloque que el núcleo considera configuración (core/assist.go, fenceLangs). */
export const CONFIG_LANGS = new Set(['', 'haproxy', 'cfg', 'conf', 'haproxy.cfg'])

const FENCE = /^```(.*)$/
const BULLET = /^\s*[-*+]\s+(.*)$/
const NUMBERED = /^\s*\d+[.)]\s+(.*)$/
const HEADING = /^#{1,6}\s+(.*)$/

export function parseChat(src: string): ChatBlock[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const out: ChatBlock[] = []
  let para: string[] = []
  const flush = () => { if (para.length) { out.push({ t: 'p', v: para.join('\n') }); para = [] } }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const f = FENCE.exec(line.trimEnd())
    if (f) {
      flush()
      const info = f[1].trim().split(/\s+/)
      const first = info[0] ?? ''
      const lang = first.startsWith('path=') ? '' : first.toLowerCase()
      let j = i + 1
      while (j < lines.length && lines[j].trimEnd() !== '```') j++
      const body = lines.slice(i + 1, Math.min(j, lines.length))
      const open = j >= lines.length
      if (CONFIG_LANGS.has(lang)) out.push({ t: 'config', lines: body.length, open })
      else out.push({ t: 'code', lang, v: body.join('\n'), open })
      i = j
      continue
    }
    const h = HEADING.exec(line)
    if (h) { flush(); out.push({ t: 'h', v: h[1] }); continue }
    const b = BULLET.exec(line)
    const n = b ? null : NUMBERED.exec(line)
    if (b || n) {
      flush()
      const kind = b ? 'ul' : 'ol'
      const last = out[out.length - 1]
      const item = (b ?? n)![1]
      if (last && last.t === kind) last.items.push(item)
      else out.push({ t: kind, items: [item] })
      continue
    }
    if (!line.trim()) { flush(); continue }
    para.push(line)
  }
  flush()
  return out
}

export function ChatMarkdown({ text, streaming = false }: { text: string; streaming?: boolean }) {
  const nodes: ReactNode[] = parseChat(text).map((b, i) => {
    switch (b.t) {
      case 'p': return <p key={i}><Rich text={b.v} /></p>
      case 'h': return <p key={i} className="chat-h"><b><Rich text={b.v} /></b></p>
      case 'ul': return <ul key={i}>{b.items.map((it, k) => <li key={k}><Rich text={it} /></li>)}</ul>
      case 'ol': return <ol key={i}>{b.items.map((it, k) => <li key={k}><Rich text={it} /></li>)}</ol>
      case 'code': return <pre key={i} className="chat-code" data-lang={b.lang || undefined}><code>{b.v}</code></pre>
      case 'config': return (
        <div key={i} className="chat-cfg" data-testid="config-block">
          {b.open && streaming ? 'Escribiendo la configuración propuesta…' : b.open ? 'Configuración cortada (se descarta)' : `Configuración propuesta (${b.lines} líneas): revísala en la tarjeta «Propuesta»`}
        </div>
      )
    }
  })
  return <>{nodes}</>
}
