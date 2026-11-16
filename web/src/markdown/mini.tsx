import type { ReactNode } from 'react'

// The core returns minimal markdown: **bold** and `code`. It is interpreted by
// hand and rendered with React elements; raw HTML is never inserted.

export type MiniNode =
  | { t: 'text'; v: string }
  | { t: 'code'; v: string }
  | { t: 'bold'; v: MiniNode[] }

const TOKEN = /\*\*(.+?)\*\*|`([^`]+)`/g

export function parseMini(src: string, allowBold = true): MiniNode[] {
  const out: MiniNode[] = []
  let last = 0
  const re = new RegExp(allowBold ? TOKEN : /`([^`]+)`/g)
  for (let m = re.exec(src); m; m = re.exec(src)) {
    if (m.index > last) out.push({ t: 'text', v: src.slice(last, m.index) })
    if (allowBold && m[1] !== undefined) out.push({ t: 'bold', v: parseMini(m[1], false) })
    else out.push({ t: 'code', v: (allowBold ? m[2] : m[1]) ?? '' })
    last = m.index + m[0].length
  }
  if (last < src.length) out.push({ t: 'text', v: src.slice(last) })
  return out
}

/** Strips the formatting and leaves plain text (for titles, tooltips…). */
export function plainMini(src: string): string {
  return src.replace(/\*\*(.+?)\*\*/g, '$1').replace(/`([^`]+)`/g, '$1')
}

function render(nodes: MiniNode[]): ReactNode[] {
  return nodes.map((n, i) => {
    switch (n.t) {
      case 'bold': return <b key={i}>{render(n.v)}</b>
      case 'code': return <code key={i}>{n.v}</code>
      default: return n.v
    }
  })
}

export function Rich({ text }: { text: string }) {
  return <>{render(parseMini(text))}</>
}
