import type { Issue, ProposedDirective, Section } from '../core/types'

// Transient state of the directives proposed by the assistant for a section
// (from a natural-language description). It is never persisted nor part of the
// undo history: a proposal only becomes text when the user approves it.

export interface ProposedItem {
  /** Local id (unique per store). */
  id: number
  text: string
  explanation: string
  issuesAfter: Issue[]
  known: boolean
  /** Line index within the section the proposal was made against; -1 = at the end. */
  at: number
}

export type SectionProposal =
  | { status: 'pending'; instruction: string }
  | { status: 'error'; instruction: string; message: string }
  | { status: 'ready'; instruction: string; items: ProposedItem[]; note: string; hash: string }

/** Proposals by unique section key (see uniqueSectionKeys). */
export type Proposals = Record<string, SectionProposal>

/** Cheap fingerprint of a section's text (header and lines), to notice that it changed after proposing. */
export function sectionHash(s: Section): string {
  const raw = [s.type, s.name ?? '', ...s.lines.map(l => l.text)].join('\n')
  let h = 5381
  for (let i = 0; i < raw.length; i++) h = ((h << 5) + h + raw.charCodeAt(i)) | 0
  return `${raw.length}:${(h >>> 0).toString(36)}`
}

export const toItems = (list: ProposedDirective[], nextId: () => number): ProposedItem[] =>
  list.map(d => ({ id: nextId(), text: d.text, explanation: d.explanation, issuesAfter: d.issuesAfter ?? [], known: d.known, at: d.at }))

const firstWord = (text: string) => text.trim().split(/\s+/)[0] ?? ''

/**
 * Insertion index for `item` in the CURRENT section. If the section did not change
 * since proposing, the suggested one is kept; otherwise it is recomputed (after the
 * last directive with the same keyword, or at the end) and a stale index is never trusted.
 */
export function effectiveAt(item: ProposedItem, s: Section, changed: boolean): number {
  if (!changed) return item.at >= 0 && item.at <= s.lines.length ? item.at : -1
  const kw = firstWord(item.text)
  let last = -1
  s.lines.forEach((l, i) => { if (l.kind === 'directive' && l.keyword === kw) last = i })
  return last >= 0 ? last + 1 : -1
}

/**
 * Orders insertions so that one batch of add_directive ops is valid: those at the
 * end first (in order), then the positioned ones from the highest index to the
 * lowest (ties in reverse, so the earlier proposal ends up first).
 */
export function batchOrder<T extends { at: number }>(list: T[]): T[] {
  const end = list.filter(x => x.at < 0)
  const pos = list.map((x, i) => ({ x, i })).filter(e => e.x.at >= 0).sort((a, b) => b.x.at - a.x.at || b.i - a.i).map(e => e.x)
  return [...end, ...pos]
}

/** After applying insertions at `applied`, the remaining suggested positions move down. */
export function shiftAt(at: number, applied: number[]): number {
  if (at < 0) return at
  return at + applied.filter(a => a >= 0 && a <= at).length
}

/** Index in the section's rendered rows (blank lines are not shown) where a proposed row goes; rows.length = at the end. */
export function rowPosition(at: number, rowLineIndexes: number[]): number {
  if (at < 0) return rowLineIndexes.length
  const i = rowLineIndexes.findIndex(li => li >= at)
  return i < 0 ? rowLineIndexes.length : i
}
