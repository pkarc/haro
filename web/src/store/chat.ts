import type { AssistDone, AssistMessage, AssistProposal, FileChange, SourceFile } from '../core/types'

// Estado y funciones puras de la conversación con el asistente (sin red ni
// store), fáciles de probar. El store las usa en sendChat/applyProposal.

export type ProposalStatus = 'pending' | 'applied' | 'discarded'

/** Propuesta del asistente dentro de un mensaje, con su estado y el texto base con el que se pidió. */
export interface ChatProposal extends AssistProposal {
  status: ProposalStatus
  /** Texto que tenían los archivos propuestos cuando se hizo la petición. */
  base: SourceFile[]
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  ts: number
  /** Solo asistente: sigue llegando (streaming). No se guarda. */
  pending?: boolean
  /** Solo asistente: el usuario detuvo la respuesta. */
  stopped?: boolean
  /** Solo asistente: mensaje de error (la respuesta no llegó entera). */
  error?: string
  proposal?: ChatProposal
  done?: Pick<AssistDone, 'stopReason' | 'refusalCategory'>
}

/** Máximo de mensajes que se envían (el servidor admite 40): conserva el hilo reciente. */
export const MAX_SENT = 20

/**
 * Conversación que se envía al servidor: sin mensajes vacíos ni fallidos,
 * alternando roles (los turnos seguidos del mismo rol se unen) y terminando
 * en un turno del usuario.
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

/** Un mensaje sin terminar (cierre de pestaña a mitad de respuesta) se recupera como interrumpido. */
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

/** Quita el estado transitorio antes de guardar. */
export function persistableChat(chat: ChatMessage[]): ChatMessage[] {
  return chat.map(m => {
    if (!m.pending) return m
    const { pending: _p, ...rest } = m
    return { ...rest, error: rest.error ?? 'La respuesta se interrumpió.' }
  })
}

/** ¿Cambió la configuración desde que se pidió la propuesta (en alguno de los archivos que propone)? */
export function proposalStale(p: ChatProposal, files: SourceFile[]): boolean {
  return p.changes.some(c => {
    const base = p.base.find(b => b.path === c.path)
    const cur = files.find(f => f.path === c.path)
    return !cur || !base || base.text !== cur.text
  })
}

const normalize = (t: string) => t.replace(/\r\n?/g, '\n')

/**
 * Texto con el que quedaría un archivo: el del bloque del asistente, con los
 * saltos de línea normalizados y respetando que el archivo termine (o no) en
 * salto de línea como el actual (el bloque de código no incluye el último).
 */
export function proposedText(current: string, proposed: string): string {
  let t = normalize(proposed)
  const endsNl = normalize(current).endsWith('\n')
  if (endsNl && !t.endsWith('\n')) t += '\n'
  else if (!endsNl && t.endsWith('\n')) t = t.slice(0, -1)
  return t
}

/** Archivos con el texto de la propuesta; lanza si algún archivo propuesto ya no existe. */
export function applyProposalToFiles(files: SourceFile[], changes: FileChange[]): SourceFile[] {
  const out = files.slice()
  for (const c of changes) {
    const i = out.findIndex(f => f.path === c.path)
    if (i < 0) throw new Error(`El archivo «${c.path}» de la propuesta ya no existe en la configuración`)
    out[i] = { path: c.path, text: proposedText(out[i].text, c.text) }
  }
  return out
}

/** Textos de los archivos que la propuesta toca, tal como estaban al pedirla. */
export function snapshotFor(files: SourceFile[], changes: FileChange[]): SourceFile[] {
  return changes.flatMap(c => {
    const f = files.find(x => x.path === c.path)
    return f ? [{ path: f.path, text: f.text }] : []
  })
}

export const countIssues = (issues: { severity: string }[], sev: string) => issues.filter(i => i.severity === sev).length
