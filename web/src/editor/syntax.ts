import type { Knowledge } from '../core/types'

// Presentation helpers for the editor (highlighting and indentation). The real
// interpretation of the configuration is always done by the core.

// Sections HAProxy accepts indented; the rest of the headers only in column 0.
export const INDENTABLE = new Set(['global', 'defaults', 'frontend', 'backend', 'listen'])

// Directives whose second token is a subkeyword (for coloring only).
export const SUBKEYWORDS = new Set([
  'timeout', 'option', 'no', 'stats', 'http-request', 'http-response', 'http-after-response',
  'tcp-request', 'tcp-response', 'http-check', 'tcp-check', 'balance', 'mode', 'compression',
  'capture', 'stick', 'hash-type', 'http-reuse',
])

/** Is `line` a section header? Returns its type. */
export function headerType(line: string, knowledge: Knowledge | null): string | null {
  const m = /^([ \t]*)(\S+)/.exec(line)
  if (!m || !knowledge) return null
  const kw = m[2].toLowerCase()
  if (!(kw in knowledge.sections)) return null
  return !m[1] || INDENTABLE.has(kw) ? kw : null
}

/** Type of the section `lineNo` (1-based) belongs to, searching upwards. */
export function sectionTypeAt(lines: (n: number) => string, lineNo: number, knowledge: Knowledge | null): string | null {
  for (let n = lineNo; n >= 1; n--) {
    const t = headerType(lines(n), knowledge)
    if (t) return t
  }
  return null
}

export function tokenClass(word: string): string {
  if (word === 'if' || word === 'unless') return 'cond'
  if (/^(!?\{|\}|!|\|\||or)$/.test(word)) return 'op'
  if (/^["']/.test(word)) return 'str'
  if (/%\[/.test(word)) return 'var'
  if (/^-[a-zA-Z]/.test(word)) return 'flag'
  if (/^\d+(\.\d+)?(us|ms|s|m|h|d|k|K|M|g|G)?$/.test(word)) return 'num'
  if (/^(\[[0-9a-f:]*\]|[\w.\-*]*):\d+(-\d+)?$/i.test(word) || /^\d{1,3}(\.\d{1,3}){3}(\/\d+)?$/.test(word) || /^:::\d+$/.test(word)) return 'addr'
  if (/^(\/|unix@|ipv[46]@)/.test(word)) return 'path'
  return 'arg'
}
