import { HighlightStyle, StreamLanguage, syntaxHighlighting, type StreamParser } from '@codemirror/language'
import { Tag } from '@lezer/highlight'
import type { Extension } from '@codemirror/state'
import type { Knowledge } from '../core/types'
import { INDENTABLE, SUBKEYWORDS, tokenClass } from './syntax'

interface LState { idx: number; kw: string; indented: boolean; sec: boolean }

// Colores de los tokens: los mismos que el prototipo (clases .t-* de hlLine).
const COLORS: Record<string, { color: string; fontStyle?: string; fontWeight?: string }> = {
  cm: { color: '#64748b', fontStyle: 'italic' },
  name: { color: '#f8fafc', fontWeight: '600' },
  kw: { color: '#7dd3fc' },
  kw2: { color: '#c4b5fd' },
  cond: { color: '#f472b6', fontWeight: '500' },
  op: { color: '#f472b6' },
  str: { color: '#86efac' },
  var: { color: '#fcd34d' },
  flag: { color: '#fda4af' },
  num: { color: '#fdba74' },
  addr: { color: '#5eead4' },
  path: { color: '#a5b4fc' },
  arg: { color: '#cbd5e1' },
}

/** Lenguaje HAProxy para CodeMirror: StreamLanguage + estilo propio. */
export function haproxyLanguage(knowledge: Knowledge | null): Extension {
  const sections = knowledge?.sections ?? {}
  const names = [...Object.keys(COLORS), ...Object.keys(sections).map(t => `sec-${t}`)]
  const tags: Record<string, Tag> = Object.fromEntries(names.map(n => [n, Tag.define()]))

  const parser: StreamParser<LState> = {
    name: 'haproxy',
    startState: () => ({ idx: 0, kw: '', indented: false, sec: false }),
    copyState: s => ({ ...s }),
    token(stream, st) {
      if (stream.sol()) {
        st.idx = 0; st.kw = ''; st.sec = false
        st.indented = /^[ \t]/.test(stream.string)
      }
      if (stream.eatSpace()) return null
      if (stream.peek() === '#') { stream.skipToEnd(); return 'cm' }
      let q: string | null = null
      while (!stream.eol()) {
        const c = stream.peek()
        if (q) {
          if (c === '\\') { stream.next(); stream.next(); continue }
          if (c === q) q = null
          stream.next(); continue
        }
        if (c === '"' || c === "'") { q = c; stream.next(); continue }
        if (c === '\\') { stream.next(); stream.next(); continue }
        if (c === ' ' || c === '\t') break
        stream.next()
      }
      const word = stream.current()
      const i = st.idx++
      if (i === 0) {
        st.kw = word.toLowerCase()
        st.sec = st.kw in sections && (!st.indented || INDENTABLE.has(st.kw))
        return st.sec ? `sec-${st.kw}` : 'kw'
      }
      if (i === 1 && st.sec) return 'name'
      if (i === 1 && SUBKEYWORDS.has(st.kw)) return 'kw2'
      return tokenClass(word)
    },
    tokenTable: tags,
  }

  const style = HighlightStyle.define([
    ...Object.entries(COLORS).map(([n, s]) => ({ tag: tags[n], ...s })),
    ...Object.entries(sections).map(([t, info]) => ({ tag: tags[`sec-${t}`], color: info.color, fontWeight: '600' })),
  ])
  return [StreamLanguage.define(parser), syntaxHighlighting(style)]
}
