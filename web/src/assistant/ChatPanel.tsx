import { useEffect, useRef, useState } from 'react'
import { confirmBox } from '../components/dialogs'
import { DiffBlock } from '../components/DiffBlock'
import { ChatMarkdown } from '../markdown/chat'
import { Rich } from '../markdown/mini'
import { useApp } from '../store'
import { countIssues, proposalStale, proposedText, type ChatMessage } from '../store/chat'
import { baseName } from '../store/files'

export const STARTERS = [
  'Añade un backend con 3 servidores web en 10.0.1.10-12:8080 con health check en /health',
  'Limita a 20 peticiones por segundo por IP en https_in',
  'Revisa mi configuración y dime qué mejorarías',
  'Explícame qué hace mi configuración actual',
  'Activa compresión gzip y HTTP/2',
]

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** Explicación cuando el asistente no está activo en el servidor (o no hay servidor). */
export function AssistantDisabled() {
  return (
    <div className="empty" data-testid="assistant-disabled">
      <div className="spark">✦</div>
      <h3>Asistente</h3>
      <p className="muted">
        El asistente con IA se activa iniciando <code>haro serve --assistant</code>. Al usarlo, la configuración se envía a la API de Anthropic
        desde el servidor local (la clave la lee el servidor y nunca llega al navegador).
      </p>
    </div>
  )
}

function ProposalCard({ mi, m }: { mi: number; m: ChatMessage }) {
  const p = m.proposal!
  const files = useApp(s => s.files)
  const analysis = useApp(s => s.analysis)
  const applyProposal = useApp(s => s.applyProposal)
  const discard = useApp(s => s.discardProposal)
  const busy = useApp(s => s.chatBusy)
  const stale = p.status === 'pending' && proposalStale(p, files)
  const now = analysis?.issues ?? []
  const errAfter = countIssues(p.issuesAfter, 'error'), warnAfter = countIssues(p.issuesAfter, 'warn')

  const apply = async () => {
    if (stale && !(await confirmBox('La configuración ha cambiado desde que el asistente hizo esta propuesta. Aplicarla sustituirá el texto actual de esos archivos y se perderán esos cambios (podrás deshacer). ¿Aplicar de todos modos?', 'Aplicar de todos modos', true))) return
    applyProposal(mi)
  }

  return (
    <div className={`proposal ${p.status}`} data-testid="proposal" data-status={p.status}>
      <div className="proposal-h">
        <b>Propuesta</b>
        {p.changes.length > 0 && <span className="muted sm">{plural(p.changes.length, 'archivo', 'archivos')}</span>}
        {p.status === 'applied' && <span className="pill ok">Aplicada</span>}
        {p.status === 'discarded' && <span className="pill">Descartada</span>}
      </div>
      {p.status === 'pending' && p.changes.map(c => {
        const cur = files.find(f => f.path === c.path)
        return (
          <div key={c.path} className="proposal-file">
            {(p.changes.length > 1 || files.length > 1) && <h5>{baseName(c.path)}</h5>}
            <DiffBlock before={cur?.text ?? ''} after={proposedText(cur?.text ?? '', c.text)} />
          </div>
        )
      })}
      {p.changes.length > 0 && p.status === 'pending' && (
        <p className={`proposal-iss sm ${errAfter ? 'sev-error-t' : 'muted'}`} data-testid="proposal-issues">
          Tras aplicarla: {plural(errAfter, 'error', 'errores')} y {plural(warnAfter, 'aviso', 'avisos')}
          {analysis && <> (ahora: {plural(countIssues(now, 'error'), 'error', 'errores')} y {plural(countIssues(now, 'warn'), 'aviso', 'avisos')})</>}.
        </p>
      )}
      {p.truncated && <p className="warn sm">Un bloque de configuración quedó cortado y se descartó: pide al asistente que lo repita.</p>}
      {p.unmatched > 0 && <p className="warn sm">{plural(p.unmatched, 'bloque de configuración no se pudo', 'bloques de configuración no se pudieron')} asociar a un archivo y se descartó.</p>}
      {stale && <p className="warn sm" data-testid="proposal-stale">La configuración cambió desde que se hizo esta propuesta: al aplicarla se sustituye el texto actual de esos archivos.</p>}
      {p.changes.length > 0 && p.status === 'pending' && (
        <div className="row-btns">
          <button className="btn sm primary" disabled={busy} onClick={() => void apply()}>Aplicar</button>
          <button className="btn sm" onClick={() => discard(mi)}>Descartar</button>
        </div>
      )}
    </div>
  )
}

function Bubble({ m, mi, last }: { m: ChatMessage; mi: number; last: boolean }) {
  const retry = useApp(s => s.retryChat)
  const busy = useApp(s => s.chatBusy)
  if (m.role === 'user') return <div className="msg user"><div className="bubble" data-testid="msg-user">{m.content}</div></div>
  const refusal = m.done?.stopReason === 'refusal'
  return (
    <div className={`msg ai${m.error ? ' err' : ''}`} data-testid="msg-assistant">
      <div className="bubble md">
        {m.content
          ? <ChatMarkdown text={m.content} streaming={!!m.pending} />
          : m.pending ? <span className="typing" aria-label="El asistente está escribiendo"><i /><i /><i /></span> : null}
        {m.stopped && <p className="muted sm">(detenido)</p>}
        {refusal && <p className="iss sev-warn" role="alert">El modelo declinó esta petición{m.done?.refusalCategory ? ` (categoría: ${m.done.refusalCategory})` : ''}. Reformúlala o pide otra cosa.</p>}
        {m.done?.stopReason === 'max_tokens' && <p className="warn sm" role="alert">La respuesta se cortó por el límite de longitud: pide al asistente que continúe o que lo haga por partes.</p>}
        {m.error && (
          <div className="iss sev-error" role="alert" data-testid="chat-error">
            <span>Error</span> {m.error}
            {last && <button className="btn sm" disabled={busy} onClick={() => void retry()}>Reintentar</button>}
          </div>
        )}
      </div>
      {m.proposal && <ProposalCard mi={mi} m={m} />}
    </div>
  )
}

export function ChatPanel() {
  const chat = useApp(s => s.chat)
  const busy = useApp(s => s.chatBusy)
  const ack = useApp(s => s.chatAck)
  const model = useApp(s => s.capabilities?.assistantModel)
  const send = useApp(s => s.sendChat)
  const stop = useApp(s => s.stopChat)
  const newChat = useApp(s => s.newChat)
  const ackChat = useApp(s => s.ackChat)
  const [text, setText] = useState('')
  const log = useRef<HTMLDivElement>(null)
  const stick = useRef(true)

  // Sigue el final mientras llega la respuesta, salvo que el usuario haya subido.
  useEffect(() => {
    const el = log.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [chat])

  const submit = async (value: string) => {
    const t = value.trim()
    if (!t || busy) return
    if (!ack) {
      const ok = await confirmBox(`Al usar el asistente, la configuración (con sus contraseñas y direcciones) se enviará a la API de Anthropic con el modelo ${model || 'configurado'}, desde el servidor local. La clave de la API nunca llega al navegador. ¿Continuar?`, 'Enviar a Anthropic')
      if (!ok) return
      ackChat()
    }
    setText('')
    stick.current = true
    await send(t)
  }

  const clear = async () => {
    if (busy) stop()
    if (await confirmBox('¿Empezar una conversación nueva? Se borra el historial del chat; la configuración no se toca.', 'Nueva conversación', true)) newChat()
  }

  return (
    <div className="chat" data-testid="chat-panel">
      <div className="chat-head">
        <b>✦ Asistente</b>
        <button className="btn sm" onClick={() => void clear()} disabled={!chat.length}>Nueva conversación</button>
      </div>
      <div className="chat-log" ref={log} data-testid="chat-log" onScroll={e => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80 }}>
        {chat.length === 0
          ? (
            <div className="chat-empty">
              <div className="spark">✦</div>
              <h3>Asistente de HAProxy</h3>
              <p className="muted">Describe lo que necesitas en lenguaje natural. Conoce tu configuración activa y te propondrá cambios que podrás revisar como diff antes de aplicarlos.</p>
              <div className="starters">
                {STARTERS.map(s => <button key={s} className="chip" onClick={() => void submit(s)}>{s}</button>)}
              </div>
            </div>
          )
          : chat.map((m, i) => <Bubble key={i} m={m} mi={i} last={i === chat.length - 1} />)}
      </div>
      <form className="chat-input" onSubmit={e => { e.preventDefault(); void submit(text) }}>
        <textarea
          aria-label="Mensaje para el asistente" placeholder="Pide un cambio o haz una pregunta…" rows={2} value={text}
          onChange={e => setText(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void submit(text) } }}
        />
        {busy
          ? <button type="button" className="btn danger" onClick={stop}>Detener</button>
          : <button type="submit" className="btn primary" disabled={!text.trim()}>Enviar</button>}
      </form>
      <p className="chat-note muted sm" data-testid="chat-privacy">
        <Rich text={`La configuración se envía a la API de Anthropic${model ? ` (modelo \`${model}\`)` : ''} desde el servidor local.`} />
      </p>
    </div>
  )
}
