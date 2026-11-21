import type { Op, Section } from '../core/types'

// Operation builders for core.Apply. Section indexes are those of the unified
// view (analysis.doc.sections).

export const opUpdateDirective = (section: number, lineIndex: number, text: string): Op => ({ op: 'update_directive', section, lineIndex, text })
export const opRemoveDirective = (section: number, lineIndex: number): Op => ({ op: 'remove_directive', section, lineIndex })
export const opMoveDirective = (section: number, lineIndex: number, delta: number): Op => ({ op: 'move_directive', section, lineIndex, delta })
/** `at` = line index within the section; omitted (or negative) = at the end. */
export const opAddDirective = (section: number, text: string, at?: number): Op => ({ op: 'add_directive', section, text, ...(at !== undefined && at >= 0 ? { at } : {}) })
export const opRemoveSection = (section: number): Op => ({ op: 'remove_section', section })
export const opMoveSection = (section: number, to: number): Op => ({ op: 'move_section', section, to })
export const opRenameSection = (section: number, name: string, updateRefs: boolean): Op => ({ op: 'rename_section', section, name, updateRefs })
export const opAddSection = (type: string, name: string, file?: number): Op => ({
  op: 'add_section', type, template: true, ...(name ? { name } : {}), ...(file !== undefined ? { file } : {}),
})

export const sectionKey = (s: Section) => `${s.file}:${s.type}:${s.name ?? ''}`

/** Keys that stay unique when sections repeat (e.g. two unnamed `defaults`): `file:type:name#n`. */
export const uniqueSectionKeys = (sections: Section[]): string[] => {
  const seen = new Map<string, number>()
  return sections.map(s => { const k = sectionKey(s), n = seen.get(k) ?? 0; seen.set(k, n + 1); return `${k}#${n}` })
}
