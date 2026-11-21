import { Fragment, useEffect, useRef, useState } from 'react'
import type { Analysis, Knowledge, Section } from '../core/types'
import { ensureAssistAck } from '../assistant/ack'
import { confirmBox, promptBox } from '../components/dialogs'
import { useT } from '../i18n/react'
import { useApp, useAppApi } from '../store'
import { rowPosition, sectionHash } from '../store/proposals'
import { lineKey, SEVERITY_KEY, type AnalysisIndex } from '../store/selectors'
import { DirectiveRow } from './DirectiveRow'
import { ProposedRow } from './ProposedRow'
import { opAddDirective, opMoveDirective, opMoveSection, opRemoveDirective, opRemoveSection, opRenameSection, sectionKey } from './ops'

const norm = (x: string) => x.trim().replace(/\s+/g, ' ')

interface Props {
  section: Section
  /** Unique key of the section (stable when lines are renumbered); keys the assistant's proposals. */
  skey: string
  index: number
  total: number
  analysis: Analysis
  idx: AnalysisIndex
  knowledge: Knowledge
  showFile: boolean
  fileLabel: string
  /** The editor cursor is inside this section. */
  current: boolean
  /** Cursor line when it is in this section's file, to mark the matching row. */
  cursorLine: number | null
}

export function SectionCard({ section: s, skey, index: si, total, analysis, idx, knowledge, showFile, fileLabel, current, cursorLine }: Props) {
  const t = useT()
  const applyOps = useApp(a => a.applyOps)
  const collapsed = useApp(a => !!a.ui.collapsed[sectionKey(s)])
  const toggle = useApp(a => a.toggleCollapsed)
  const revealInCode = useApp(a => a.revealInCode)
  const api = useAppApi()
  const canAssist = useApp(a => !!a.capabilities?.canAssist)
  const proposal = useApp(a => a.proposals[skey])
  const approved = useApp(a => a.approved)
  const fresh = useApp(a => a.analysisRev === a.rev)
  const propose = useApp(a => a.proposeForSection)
  const cancelProposal = useApp(a => a.cancelProposal)
  const approveProposal = useApp(a => a.approveProposal)
  const approveAll = useApp(a => a.approveAll)
  const rejectProposal = useApp(a => a.rejectProposal)
  const rejectAll = useApp(a => a.rejectAll)
  const [adding, setAdding] = useState('')
  const [desc, setDesc] = useState('')
  const [manual, setManual] = useState(false)
  const [busy, setBusy] = useState(false)
  const art = useRef<HTMLElement>(null)
  const area = useRef<HTMLTextAreaElement>(null)
  const flashed = useRef(0)

  const info = knowledge.sections[s.type]
  const eff = analysis.effective[si]
  const items = s.lines.map((l, li) => ({ l, li })).filter(x => x.l.kind !== 'blank')
  const nDirs = items.filter(x => x.l.kind === 'directive').length
  const headIssues = idx.issues.get(lineKey(s.file, s.line)) ?? []
  const proxy = s.type === 'frontend' || s.type === 'backend' || s.type === 'listen'
  const timeouts = eff ? Object.keys(eff.timeouts).sort() : []
  const placeholder = knowledge.templates[s.type]?.lines.find(x => !x.includes('{{'))

  // Proposed directives go at their suggested line (or at the end) among the visible rows.
  const ready = proposal?.status === 'ready' ? proposal : null
  const waiting = proposal?.status === 'pending'
  const proposed = ready ? ready.items : []
  const stale = !!ready && fresh && sectionHash(s) !== ready.hash
  const rowLines = items.map(x => x.li)
  const proposedAt = (at: number) => rowPosition(stale || at > s.lines.length ? -1 : at, rowLines)
  const nProposed = proposed.length

  const grow = (el: HTMLTextAreaElement | null) => {
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 96)}px`
  }
  useEffect(() => grow(area.current), [desc])

  const ask = async () => {
    const text = desc.trim()
    if (!text || waiting) return
    if (!(await ensureAssistAck(api, t, 'propose.confirmSend'))) return
    setDesc('')
    void propose(skey, text)
  }
  const run = async (fn: () => Promise<boolean>) => {
    setBusy(true)
    try { await fn() } finally { setBusy(false) }
  }

  // The approved directive is now a normal row: highlight it once the analysis that contains it arrives.
  useEffect(() => {
    if (!approved || approved.key !== skey || approved.nonce === flashed.current || !fresh) return
    flashed.current = approved.nonce
    const want = new Set(approved.texts.map(norm))
    art.current?.querySelectorAll<HTMLElement>('[data-testid=directive]').forEach(r => {
      if (!want.has(norm(r.dataset.raw ?? ''))) return
      r.classList.add('flash')
      setTimeout(() => r.classList.remove('flash'), 1400)
    })
  }, [approved, fresh, skey])

  const rename = async () => {
    const r = await promptBox(t('section.rename', { type: s.type }), t('section.renameNew'), s.name ?? '', { label: t('section.renameRefs'), value: true })
    if (r && r.text !== (s.name ?? '')) await applyOps([opRenameSection(si, r.text, r.check)])
  }
  const remove = async () => {
    if (await confirmBox(t('section.removeConfirm', { type: s.type, name: s.name ? ` “${s.name}”` : '', n: nDirs }), t('common.delete'), true)) await applyOps([opRemoveSection(si)])
  }
  return (
    <article
      ref={art}
      className={`sec ${collapsed ? 'collapsed' : ''} ${current ? 'current' : ''} ${headIssues.some(i => i.severity === 'error') ? 'has-error' : ''}`}
      style={{ '--c': info?.color } as React.CSSProperties}
      data-testid="section" data-section={si} data-file={s.file} data-line={s.line}
    >
      <header className="sec-h">
        <button className="icon tog" aria-label={collapsed ? t('section.expand') : t('section.collapse')} aria-expanded={!collapsed} onClick={() => toggle(sectionKey(s))}>▾</button>
        <span className="badge">{s.type}</span>
        {info?.named !== 'none' && (
          <button className="sec-name" title={t('section.renameTitle')} aria-label={`${t('section.rename', { type: s.type })} ${s.name ?? ''}`} onClick={rename}>{s.name || <i>{t('section.unnamed')}</i>}</button>
        )}
        <span className="sec-title">{info?.title}</span>
        <span className="sec-count" title={t('section.directives')}>{nDirs}</span>
        {(nProposed > 0 || waiting) && <span className="sec-prop" data-testid="section-proposed">{waiting ? '…' : t.n('propose.pendingCount', nProposed)}</span>}
        {showFile && <span className="sec-file" title={s.file >= 0 ? analysis.doc.files?.[s.file] : ''}>{fileLabel}</span>}
        <span className="sec-act">
          <button className="icon" aria-label={t('section.up')} title={t('section.up')} disabled={si === 0} onClick={() => applyOps([opMoveSection(si, si - 1)])}>↑</button>
          <button className="icon" aria-label={t('section.down')} title={t('section.down')} disabled={si === total - 1} onClick={() => applyOps([opMoveSection(si, si + 1)])}>↓</button>
          <button className="icon danger" aria-label={t('section.remove')} title={t('section.remove')} onClick={remove}>✕</button>
        </span>
      </header>
      <div className="sec-b">
        <p className="sec-desc">{info?.description}{s.comment && <> <span className="t-cm">{s.comment}</span></>}</p>
        {proxy && eff && (
          <div className="pills">
            <span className="pill">{t('section.mode', { mode: eff.mode })}{eff.modeFrom === 'defaults' ? ` · ${t('section.inherited')}` : eff.modeFrom === 'implicit' ? ` · ${t('section.implicit')}` : ''}</span>
            {timeouts.length > 0 && <span className="pill" title={t('section.timeoutsTitle')}>{t('section.timeouts', { list: timeouts.join(', ') })}</span>}
          </div>
        )}
        {headIssues.map((i, k) => <div key={k} className={`iss sev-${i.severity}`}><span>{t(SEVERITY_KEY[i.severity])}</span> {i.message}</div>)}
        <div className="dirs">
          {[...items, null].map((it, n) => (
            <Fragment key={it ? `${it.li}:${it.l.line}` : 'end'}>
              {proposed.map(p => ({ p, pos: proposedAt(p.at) })).filter(x => x.pos === n).map(({ p }) => (
                <ProposedRow
                  key={`p${p.id}`} item={p} stale={stale} busy={busy}
                  onApprove={() => void run(() => approveProposal(skey, p.id))}
                  onReject={() => rejectProposal(skey, p.id)}
                />
              ))}
              {it && (() => { const { l, li } = it; return (
            <DirectiveRow
              line={l}
              explanation={idx.explanations.get(lineKey(l.file, l.line))}
              issues={idx.issues.get(lineKey(l.file, l.line)) ?? []}
              categories={knowledge.categories}
              current={cursorLine !== null && l.line === cursorLine}
              canUp={n > 0}
              canDown={n < items.length - 1}
              onReveal={() => revealInCode(l.file, l.line)}
              onMove={d => applyOps([opMoveDirective(si, li, d)])}
              onRemove={() => applyOps([opRemoveDirective(si, li)])}
            />
              ) })()}
            </Fragment>
          ))}
          {!items.length && !nProposed && <div className="muted sm pad">{t('section.none')}</div>}
        </div>
        {waiting && (
          <div className="prop-wait" role="status" data-testid="proposal-pending">
            <span className="dot wait" aria-hidden="true" />
            <span>{t('propose.waiting')}</span>
            <q className="muted">{proposal.instruction}</q>
            <button type="button" className="btn sm" onClick={() => cancelProposal(skey)}>{t('common.cancel')}</button>
          </div>
        )}
        {proposal?.status === 'error' && (
          <div className="iss sev-error prop-err" role="alert" data-testid="proposal-error">
            <span>{t('common.error')}</span> {t('propose.errorTitle')}: {proposal.message}
            <span className="prop-btns">
              <button type="button" className="btn sm" onClick={() => void propose(skey, proposal.instruction)}>{t('chat.retry')}</button>
              <button type="button" className="btn sm" onClick={() => cancelProposal(skey)}>{t('propose.dismiss')}</button>
            </span>
          </div>
        )}
        {ready && (ready.note || !nProposed) && (
          <div className={`iss ${nProposed ? 'sev-info' : 'sev-warn'} prop-err`} role="status" data-testid="proposal-note">
            {!nProposed && <>{t('propose.none')} </>}
            {ready.note && <><span>{t('propose.note')}</span> {ready.note}</>}
            {!nProposed && <span className="prop-btns"><button type="button" className="btn sm" onClick={() => rejectAll(skey)}>{t('propose.dismiss')}</button></span>}
          </div>
        )}
        {nProposed > 1 && (
          <div className="prop-all" role="group" aria-label={t('propose.group')}>
            <button type="button" className="btn sm primary" disabled={busy} onClick={() => void run(() => approveAll(skey))}>{t('propose.approveAll')}</button>
            <button type="button" className="btn sm" disabled={busy} onClick={() => rejectAll(skey)}>{t('propose.rejectAll')}</button>
          </div>
        )}
        {canAssist && !manual ? (
          <>
            <form className="add-dir assist" onSubmit={e => { e.preventDefault(); void ask() }}>
              <textarea
                ref={area} rows={1} value={desc} onChange={e => setDesc(e.target.value)} disabled={waiting}
                aria-label={t('propose.label', { type: s.type, name: s.name ?? '' })}
                placeholder={t('propose.placeholder')}
                onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void ask() } }}
              />
              <button className="btn sm primary" type="submit" disabled={waiting || !desc.trim()}>{t('propose.submit')}</button>
            </form>
            <button type="button" className="link add-alt" onClick={() => setManual(true)}>{t('propose.manual')}</button>
          </>
        ) : (
          <>
        <form className="add-dir" onSubmit={async e => {
          e.preventDefault()
          if (!adding.trim()) return
          if (await applyOps([opAddDirective(si, adding.trim())])) setAdding('')
        }}>
          <input
            list={`dl-${s.type}`} value={adding} onChange={e => setAdding(e.target.value)}
            aria-label={t('section.addLabel', { type: s.type, name: s.name ?? '' })}
            placeholder={placeholder ? t('section.addPlaceholderEx', { example: placeholder }) : t('section.addPlaceholder')}
            spellCheck={false} autoCapitalize="off" autoComplete="off"
          />
          <button className="btn sm" type="submit">{t('common.add')}</button>
        </form>
            {canAssist && <button type="button" className="link add-alt" onClick={() => setManual(false)}>{t('propose.describe')}</button>}
          </>
        )}
      </div>
    </article>
  )
}
