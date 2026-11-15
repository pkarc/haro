import type { Completion, CompletionContext, CompletionResult, CompletionSource } from '@codemirror/autocomplete'
import type { Knowledge } from '../core/types'
import { sectionTypeAt } from './syntax'

/**
 * Autocompletado de la primera palabra de cada línea: con sangría, las
 * directivas de la sección bajo el cursor; sin sangría, los tipos de sección.
 * Todo sale de GET /api/v1/knowledge.
 */
export function directiveCompletion(knowledge: Knowledge | null): CompletionSource {
  if (!knowledge) return () => null
  const sectionOptions: Completion[] = Object.entries(knowledge.sections).map(([type, info]) => ({
    label: type, type: 'class', detail: info.title, info: info.description, apply: type + ' ',
  }))
  const bySection = new Map<string, Completion[]>()
  const all: Completion[] = []
  for (const [name, d] of Object.entries(knowledge.directives)) {
    const c: Completion = {
      label: name,
      type: 'keyword',
      detail: knowledge.categories[d.category] ?? d.category,
      info: d.description ? d.description.replace(/[:]$/, '') : undefined,
      apply: name + ' ',
    }
    all.push(c)
    for (const s of d.sections) {
      const list = bySection.get(s) ?? []
      list.push(c)
      bySection.set(s, list)
    }
  }

  return (ctx: CompletionContext): CompletionResult | null => {
    const word = ctx.matchBefore(/[\w.-]*/)
    if (!word || (word.from === word.to && !ctx.explicit)) return null
    const line = ctx.state.doc.lineAt(word.from)
    const before = line.text.slice(0, word.from - line.from)
    if (before.trim() !== '') return null // solo la primera palabra de la línea
    if (before === '') return { from: word.from, options: sectionOptions, validFor: /^[\w-]*$/ }
    const type = sectionTypeAt(n => ctx.state.doc.line(n).text, line.number, knowledge)
    return { from: word.from, options: (type && bySection.get(type)) || all, validFor: /^[\w.-]*$/ }
  }
}
