import type { Op, Section } from '../core/types'

// Constructores de operaciones para core.Apply. Los índices de sección son
// los de la vista unificada (analysis.doc.sections).

export const opUpdateDirective = (section: number, lineIndex: number, text: string): Op => ({ op: 'update_directive', section, lineIndex, text })
export const opRemoveDirective = (section: number, lineIndex: number): Op => ({ op: 'remove_directive', section, lineIndex })
export const opMoveDirective = (section: number, lineIndex: number, delta: number): Op => ({ op: 'move_directive', section, lineIndex, delta })
export const opAddDirective = (section: number, text: string): Op => ({ op: 'add_directive', section, text })
export const opRemoveSection = (section: number): Op => ({ op: 'remove_section', section })
export const opMoveSection = (section: number, to: number): Op => ({ op: 'move_section', section, to })
export const opRenameSection = (section: number, name: string, updateRefs: boolean): Op => ({ op: 'rename_section', section, name, updateRefs })
export const opAddSection = (type: string, name: string, file?: number): Op => ({
  op: 'add_section', type, template: true, ...(name ? { name } : {}), ...(file !== undefined ? { file } : {}),
})

export const sectionKey = (s: Section) => `${s.file}:${s.type}:${s.name ?? ''}`
