import { useState } from 'react'
import type { Analysis, Knowledge, Section } from '../core/types'
import { confirmBox, promptBox } from '../components/dialogs'
import { useT } from '../i18n/react'
import { useApp } from '../store'
import { lineKey, SEVERITY_KEY, type AnalysisIndex } from '../store/selectors'
import { DirectiveRow } from './DirectiveRow'
import { opAddDirective, opMoveDirective, opMoveSection, opRemoveDirective, opRemoveSection, opRenameSection, sectionKey } from './ops'

interface Props {
  section: Section
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

export function SectionCard({ section: s, index: si, total, analysis, idx, knowledge, showFile, fileLabel, current, cursorLine }: Props) {
  const t = useT()
  const applyOps = useApp(a => a.applyOps)
  const collapsed = useApp(a => !!a.ui.collapsed[sectionKey(s)])
  const toggle = useApp(a => a.toggleCollapsed)
  const revealInCode = useApp(a => a.revealInCode)
  const [adding, setAdding] = useState('')

  const info = knowledge.sections[s.type]
  const eff = analysis.effective[si]
  const items = s.lines.map((l, li) => ({ l, li })).filter(x => x.l.kind !== 'blank')
  const nDirs = items.filter(x => x.l.kind === 'directive').length
  const headIssues = idx.issues.get(lineKey(s.file, s.line)) ?? []
  const proxy = s.type === 'frontend' || s.type === 'backend' || s.type === 'listen'
  const timeouts = eff ? Object.keys(eff.timeouts).sort() : []
  const placeholder = knowledge.templates[s.type]?.lines.find(x => !x.includes('{{'))

  const rename = async () => {
    const r = await promptBox(t('section.rename', { type: s.type }), t('section.renameNew'), s.name ?? '', { label: t('section.renameRefs'), value: true })
    if (r && r.text !== (s.name ?? '')) await applyOps([opRenameSection(si, r.text, r.check)])
  }
  const remove = async () => {
    if (await confirmBox(t('section.removeConfirm', { type: s.type, name: s.name ? ` “${s.name}”` : '', n: nDirs }), t('common.delete'), true)) await applyOps([opRemoveSection(si)])
  }
  return (
    <article
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
          {items.map(({ l, li }, n) => (
            <DirectiveRow
              key={`${li}:${l.line}`}
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
          ))}
          {!items.length && <div className="muted sm pad">{t('section.none')}</div>}
        </div>
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
      </div>
    </article>
  )
}
