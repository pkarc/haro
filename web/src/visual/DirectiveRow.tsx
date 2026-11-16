import { useState } from 'react'
import type { Issue, Line, LineExplanation } from '../core/types'
import { Rich } from '../markdown/mini'
import { useT } from '../i18n/react'
import { SEVERITY_KEY, worstSeverity } from '../store/selectors'

interface Props {
  line: Line
  explanation?: LineExplanation
  issues: Issue[]
  categories: Record<string, string>
  editing: boolean
  canUp: boolean
  canDown: boolean
  onEdit(): void
  onCancel(): void
  onSave(text: string): void
  onMove(delta: number): void
  onRemove(): void
}

export function DirectiveRow(p: Props) {
  const t = useT()
  const { line: l, explanation: ex, issues } = p
  if (p.editing) return <EditForm initial={l.text ?? ''} onSave={p.onSave} onCancel={p.onCancel} />

  if (l.kind === 'comment') {
    return (
      <div className="cmt row" data-file={l.file} data-line={l.line}>
        <span className="cmt-t" role="button" tabIndex={0} onClick={p.onEdit} onKeyDown={e => { if (e.key === 'Enter') p.onEdit() }}>{l.text}</span>
        <span className="dir-act">
          <button className="icon danger" aria-label={t('directive.removeComment')} title={t('common.delete')} onClick={p.onRemove}>✕</button>
        </span>
      </div>
    )
  }
  const cat = ex?.explanation.category ?? 'misc'
  const known = ex?.explanation.known ?? false
  return (
    <div className={`dir row ${issues.length ? 'sev-' + worstSeverity(issues) : ''} ${known ? '' : 'unknown'}`} data-file={l.file} data-line={l.line} data-testid="directive">
      <div className="dir-main" role="button" tabIndex={0} title={t('directive.edit')} onClick={p.onEdit} onKeyDown={e => { if (e.key === 'Enter') p.onEdit() }}>
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
        <button className="icon" aria-label={t('directive.up')} title={t('directive.upTitle')} disabled={!p.canUp} onClick={() => p.onMove(-1)}>↑</button>
        <button className="icon" aria-label={t('directive.down')} title={t('directive.downTitle')} disabled={!p.canDown} onClick={() => p.onMove(1)}>↓</button>
        <button className="icon danger" aria-label={t('directive.remove')} title={t('common.delete')} onClick={p.onRemove}>✕</button>
      </div>
    </div>
  )
}

function EditForm({ initial, onSave, onCancel }: { initial: string; onSave(text: string): void; onCancel(): void }) {
  const t = useT()
  const [text, setText] = useState(initial)
  return (
    <form className="dir editing" onSubmit={e => { e.preventDefault(); onSave(text) }}>
      <input
        value={text} onChange={e => setText(e.target.value)} aria-label={t('directive.editLabel')}
        spellCheck={false} autoCapitalize="off" autoComplete="off" autoFocus
        onFocus={e => e.currentTarget.setSelectionRange(e.currentTarget.value.length, e.currentTarget.value.length)}
        onKeyDown={e => { if (e.key === 'Escape') { e.preventDefault(); onCancel() } }}
      />
      <div className="ed-act">
        <button className="btn sm primary" type="submit">{t('common.save')}</button>
        <button className="btn sm" type="button" onClick={onCancel}>{t('common.cancel')}</button>
      </div>
      <div className="muted sm">{t('directive.editHint')}</div>
    </form>
  )
}
