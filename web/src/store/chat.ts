import type { AssistDone, AssistMessage, AssistProposal, FileChange, Lang, SourceFile } from '../core/types'
import { translate } from '../i18n'

// State and pure functions of the conversation with the assistant (no network
// or store), easy to test. The store uses them in sendChat/applyProposal.

export type ProposalStatus = 'pending' | 'applied' | 'discarded'

/** Assistant proposal inside a message, with its status and the base text it was requested with. */
export interface ChatProposal extends AssistProposal {
  status: ProposalStatus
  /** Text the proposed files had when the request was made. */
  base: SourceFile[]
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  ts: number
  /** Assistant only: still arriving (streaming). Not saved. */
  pending?: boolean
  /** Assistant only: the user stopped the response. */
  stopped?: boolean
  /** Assistant only: error message (the response did not arrive in full). */
  error?: string
  proposal?: ChatProposal
  done?: Pick<AssistDone, 'stopReason' | 'refusalCategory'>
}

/** Maximum number of messages sent (the server accepts 40): keeps the recent thread. */
export const MAX_SENT = 20

/**
 * Conversation sent to the server: no empty or failed messages, alternating
 * roles (consecutive turns of the same role are merged) and ending in a user
 * turn.
 */
export function buildMessages(chat: ChatMessage[]): AssistMessage[] {
  const out: AssistMessage[] = []
  for (const m of chat) {
    if (m.pending || m.error || !m.content.trim()) continue
    const last = out[out.length - 1]
    if (last && last.role === m.role) last.content += '\n\n' + m.content
    else out.push({ role: m.role, content: m.content })
  }
  while (out.length && out[out.length - 1].role !== 'user') out.pop()
  const tail = out.slice(-MAX_SENT)
  while (tail.length && tail[0].role !== 'user') tail.shift()
  return tail
}

/** An unfinished message (tab closed mid-response) is recovered as interrupted. */
export function sanitizeChat(raw: unknown): ChatMessage[] {
  if (!Array.isArray(raw)) return []
  const out: ChatMessage[] = []
  for (const m of raw as Partial<ChatMessage>[]) {
    if (!m || (m.role !== 'user' && m.role !== 'assistant') || typeof m.content !== 'string') continue
    const msg: ChatMessage = { role: m.role, content: m.content, ts: typeof m.ts === 'number' ? m.ts : 0 }
    if (m.error) msg.error = String(m.error)
    if (m.stopped) msg.stopped = true
    if (m.done) msg.done = m.done
    if (m.proposal && Array.isArray(m.proposal.changes) && Array.isArray(m.proposal.base)) msg.proposal = m.proposal
    out.push(msg)
  }
  return out
}

/** Removes the transient state before saving. */
export function persistableChat(chat: ChatMessage[], lang: Lang = 'en'): ChatMessage[] {
  return chat.map(m => {
    if (!m.pending) return m
    const { pending: _p, ...rest } = m
    return { ...rest, error: rest.error ?? translate(lang, 'state.interrupted') }
  })
}

/** Did the configuration change since the proposal was requested (in any of the files it proposes)? */
export function proposalStale(p: ChatProposal, files: SourceFile[]): boolean {
  return p.changes.some(c => {
    const base = p.base.find(b => b.path === c.path)
    const cur = files.find(f => f.path === c.path)
    return !cur || !base || base.text !== cur.text
  })
}

const normalize = (t: string) => t.replace(/\r\n?/g, '\n')

/**
 * Text a file would end up with: that of the assistant's block, with line
 * breaks normalized and respecting whether the file ends (or not) in a newline
 * like the current one (the code block does not include the last one).
 */
export function proposedText(current: string, proposed: string): string {
  let t = normalize(proposed)
  const endsNl = normalize(current).endsWith('\n')
  if (endsNl && !t.endsWith('\n')) t += '\n'
  else if (!endsNl && t.endsWith('\n')) t = t.slice(0, -1)
  return t
}

/** Files with the proposal's text; throws if a proposed file no longer exists. */
export function applyProposalToFiles(files: SourceFile[], changes: FileChange[], lang: Lang = 'en'): SourceFile[] {
  const out = files.slice()
  for (const c of changes) {
    const i = out.findIndex(f => f.path === c.path)
    if (i < 0) throw new Error(translate(lang, 'state.proposalFileGone', { path: c.path }))
    out[i] = { path: c.path, text: proposedText(out[i].text, c.text) }
  }
  return out
}

/** Texts of the files the proposal touches, as they were when it was requested. */
export function snapshotFor(files: SourceFile[], changes: FileChange[]): SourceFile[] {
  return changes.flatMap(c => {
    const f = files.find(x => x.path === c.path)
    return f ? [{ path: f.path, text: f.text }] : []
  })
}

export const countIssues = (issues: { severity: string }[], sev: string) => issues.filter(i => i.severity === sev).length
