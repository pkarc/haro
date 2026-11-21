import type { Issue, Line, LineExplanation } from '../core/types'
import { Rich } from '../markdown/mini'
import { useT } from '../i18n/react'
import { SEVERITY_KEY, worstSeverity } from '../store/selectors'

interface Props {
  line: Line
  explanation?: LineExplanation
  issues: Issue[]
  categories: Record<string, string>
  /** The editor cursor is on this line. */
  current: boolean
  canUp: boolean
  canDown: boolean
  /** Sends the user to this line in the code editor. */
  onReveal(): void
  onMove(delta: number): void
  onRemove(): void
}

export function DirectiveRow(p: Props) {
  const t = useT()
  const { line: l, explanation: ex, issues } = p
  // Enter and Space do the same as a click.
  const keys = (e: React.KeyboardEvent) => {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); p.onReveal() }
  }
  const cur = p.current ? ' current' : ''
  const curAttr = p.current ? { 'aria-current': 'location' as const } : {}

  if (l.kind === 'comment') {
    return (
      <div className={`cmt row${cur}`} data-file={l.file} data-line={l.line} {...curAttr}>
        <span className="cmt-t" role="button" tabIndex={0} title={t('directive.editInCode')} onClick={p.onReveal} onKeyDown={keys}>{l.text}</span>
        <span className="dir-act">
          <button className="icon" aria-label={t('directive.editInCode')} title={t('directive.editInCode')} onClick={p.onReveal}>✎</button>
          <button className="icon danger" aria-label={t('directive.removeComment')} title={t('common.delete')} onClick={p.onRemove}>✕</button>
        </span>
      </div>
    )
  }
  const cat = ex?.explanation.category ?? 'misc'
  const known = ex?.explanation.known ?? false
  return (
    <div className={`dir row ${issues.length ? 'sev-' + worstSeverity(issues) : ''} ${known ? '' : 'unknown'}${cur}`} data-file={l.file} data-line={l.line} data-testid="directive" data-raw={l.text} {...curAttr}>
      <div className="dir-main" role="button" tabIndex={0} title={t('directive.editInCode')} onClick={p.onReveal} onKeyDown={keys}>
        <div className="dir-code">
          <span className={`cat cat-${cat}`}>{p.categories[cat] ?? cat}</span>
          <code className="kw">{l.keyword}</code>{' '}
          <code className="args">{ex?.args ?? (l.args ?? []).join(' ')}</code>
        </div>
        {ex && <div className="dir-exp"><Rich text={ex.explanation.text} /></div>}
        {l.comment && <div className="dir-cmt">{l.comment}</div>}
        {issues.map((i, k) => <div key={k} className={`iss sev-${i.severity}`}><span>{t(SEVERITY_KEY[i.severity])}</span> {i.message}</div>)}
      </div>
      <div className="dir-act">
        <button className="icon" aria-label={t('directive.editInCode')} title={t('directive.editInCode')} onClick={p.onReveal}>✎</button>
        <button className="icon" aria-label={t('directive.up')} title={t('directive.upTitle')} disabled={!p.canUp} onClick={() => p.onMove(-1)}>↑</button>
        <button className="icon" aria-label={t('directive.down')} title={t('directive.downTitle')} disabled={!p.canDown} onClick={() => p.onMove(1)}>↓</button>
        <button className="icon danger" aria-label={t('directive.remove')} title={t('common.delete')} onClick={p.onRemove}>✕</button>
      </div>
    </div>
  )
}
