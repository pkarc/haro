import { useEffect, useRef, useState } from 'react'
import { confirmBox } from '../components/dialogs'
import { DiffBlock } from '../components/DiffBlock'
import { useT } from '../i18n/react'
import { ChatMarkdown } from '../markdown/chat'
import { Rich } from '../markdown/mini'
import { useApp } from '../store'
import { countIssues, proposalStale, proposedText, type ChatMessage } from '../store/chat'
import { baseName } from '../store/files'

export const STARTER_KEYS = ['assistant.starter1', 'assistant.starter2', 'assistant.starter3', 'assistant.starter4', 'assistant.starter5'] as const

/** Explanation when the assistant is not active on the server (or there is no server). */
export function AssistantDisabled() {
  const t = useT()
  return (
    <div className="empty" data-testid="assistant-disabled">
      <div className="spark">✦</div>
      <h3>{t('assistant.title')}</h3>
      <p className="muted"><Rich text={t('assistant.disabled')} /></p>
    </div>
  )
}

function ProposalCard({ mi, m }: { mi: number; m: ChatMessage }) {
  const t = useT()
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
    if (stale && !(await confirmBox(t('chat.staleConfirm'), t('chat.applyAnyway'), true))) return
    applyProposal(mi)
  }

  return (
    <div className={`proposal ${p.status}`} data-testid="proposal" data-status={p.status}>
      <div className="proposal-h">
        <b>{t('chat.proposal')}</b>
        {p.changes.length > 0 && <span className="muted sm">{t.n('chat.files', p.changes.length)}</span>}
        {p.status === 'applied' && <span className="pill ok">{t('chat.applied')}</span>}
        {p.status === 'discarded' && <span className="pill">{t('chat.discarded')}</span>}
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
          {t('chat.after', { errors: t.n('problems.error', errAfter), warnings: t.n('problems.warn', warnAfter) })}
          {analysis && t('chat.now', { errors: t.n('problems.error', countIssues(now, 'error')), warnings: t.n('problems.warn', countIssues(now, 'warn')) })}.
        </p>
      )}
      {p.truncated && <p className="warn sm">{t('chat.truncated')}</p>}
      {p.unmatched > 0 && <p className="warn sm">{t.n('chat.unmatched', p.unmatched)}</p>}
      {stale && <p className="warn sm" data-testid="proposal-stale">{t('chat.stale')}</p>}
      {p.changes.length > 0 && p.status === 'pending' && (
        <div className="row-btns">
          <button className="btn sm primary" disabled={busy} onClick={() => void apply()}>{t('common.apply')}</button>
          <button className="btn sm" onClick={() => discard(mi)}>{t('chat.discard')}</button>
        </div>
      )}
    </div>
  )
}

function Bubble({ m, mi, last }: { m: ChatMessage; mi: number; last: boolean }) {
  const t = useT()
  const retry = useApp(s => s.retryChat)
  const busy = useApp(s => s.chatBusy)
  if (m.role === 'user') return <div className="msg user"><div className="bubble" data-testid="msg-user">{m.content}</div></div>
  const refusal = m.done?.stopReason === 'refusal'
  return (
    <div className={`msg ai${m.error ? ' err' : ''}`} data-testid="msg-assistant">
      <div className="bubble md">
        {m.content
          ? <ChatMarkdown text={m.content} streaming={!!m.pending} />
          : m.pending ? <span className="typing" aria-label={t('chat.typing')}><i /><i /><i /></span> : null}
        {m.stopped && <p className="muted sm">{t('chat.stopped')}</p>}
        {refusal && <p className="iss sev-warn" role="alert">{t('chat.refusal', { category: m.done?.refusalCategory ? t('chat.refusalCategory', { category: m.done.refusalCategory }) : '' })}</p>}
        {m.done?.stopReason === 'max_tokens' && <p className="warn sm" role="alert">{t('chat.maxTokens')}</p>}
        {m.error && (
          <div className="iss sev-error" role="alert" data-testid="chat-error">
            <span>{t('common.error')}</span> {m.error}
            {last && <button className="btn sm" disabled={busy} onClick={() => void retry()}>{t('chat.retry')}</button>}
          </div>
        )}
      </div>
      {m.proposal && <ProposalCard mi={mi} m={m} />}
    </div>
  )
}

export function ChatPanel() {
  const t = useT()
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

  // Follows the end while the response arrives, unless the user scrolled up.
  useEffect(() => {
    const el = log.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [chat])

  const submit = async (value: string) => {
    const msg = value.trim()
    if (!msg || busy) return
    if (!ack) {
      const ok = await confirmBox(t('chat.confirmSend', { model: model || t('chat.modelConfigured') }), t('chat.confirmSendOk'))
      if (!ok) return
      ackChat()
    }
    setText('')
    stick.current = true
    await send(msg)
  }

  const clear = async () => {
    if (busy) stop()
    if (await confirmBox(t('chat.confirmNew'), t('chat.newChat'), true)) newChat()
  }

  return (
    <div className="chat" data-testid="chat-panel">
      <div className="chat-head">
        <b>✦ {t('assistant.title')}</b>
        <button className="btn sm" onClick={() => void clear()} disabled={!chat.length}>{t('chat.newChat')}</button>
      </div>
      <div className="chat-log" ref={log} data-testid="chat-log" onScroll={e => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80 }}>
        {chat.length === 0
          ? (
            <div className="chat-empty">
              <div className="spark">✦</div>
              <h3>{t('assistant.heading')}</h3>
              <p className="muted">{t('assistant.intro')}</p>
              <div className="starters">
                {STARTER_KEYS.map(k => <button key={k} className="chip" onClick={() => void submit(t(k))}>{t(k)}</button>)}
              </div>
            </div>
          )
          : chat.map((m, i) => <Bubble key={i} m={m} mi={i} last={i === chat.length - 1} />)}
      </div>
      <form className="chat-input" onSubmit={e => { e.preventDefault(); void submit(text) }}>
        <textarea
          aria-label={t('chat.inputLabel')} placeholder={t('chat.inputPlaceholder')} rows={2} value={text}
          onChange={e => setText(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void submit(text) } }}
        />
        {busy
          ? <button type="button" className="btn danger" onClick={stop}>{t('chat.stop')}</button>
          : <button type="submit" className="btn primary" disabled={!text.trim()}>{t('chat.send')}</button>}
      </form>
      <p className="chat-note muted sm" data-testid="chat-privacy">
        <Rich text={t('chat.privacy', { model: model ? t('chat.privacyModel', { model }) : '' })} />
      </p>
    </div>
  )
}
