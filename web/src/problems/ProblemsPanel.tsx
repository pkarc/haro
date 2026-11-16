import { useState } from 'react'
import type { Severity } from '../core/types'
import { useT } from '../i18n/react'
import { Rich } from '../markdown/mini'
import { useApp } from '../store'
import { baseName } from '../store/files'
import { SEVERITY_KEY } from '../store/selectors'

const ORDER: Severity[] = ['error', 'warn', 'info']
const COUNT_KEY = { error: 'problems.error', warn: 'problems.warn', info: 'problems.info' } as const

export function useIssueCounts() {
  const issues = useApp(s => s.analysis?.issues)
  const c: Record<Severity, number> = { error: 0, warn: 0, info: 0 }
  issues?.forEach(i => c[i.severity]++)
  return { counts: c, total: issues?.length ?? 0 }
}

// Summary button in the header: opens and closes the problems panel.
export function ProblemsButton({ open, onToggle }: { open: boolean; onToggle(): void }) {
  const t = useT()
  const { counts, total } = useIssueCounts()
  return (
    <button className="probs-btn" onClick={onToggle} aria-expanded={open} data-testid="problems-button">
      {!total
        ? <span className="ok-t">{t('problems.none')}</span>
        : ORDER.filter(k => counts[k]).map(k => <span key={k} className={`cnt sev-${k}`}>{t.n(COUNT_KEY[k], counts[k])}</span>)}
    </button>
  )
}

export function ProblemsPanel() {
  const t = useT()
  const analysis = useApp(s => s.analysis)
  const files = useApp(s => s.files)
  const goto = useApp(s => s.goto)
  const multi = files.length > 1
  const { counts, total } = useIssueCounts()
  const [filter, setFilter] = useState<Severity | 'all'>('all')
  const list = (analysis?.issues ?? []).filter(i => filter === 'all' || i.severity === filter)

  return (
    <div className="probs" data-testid="problems-panel">
      <div className="probs-h">
        <b>{t('problems.title')}</b>
        <div className="seg sm" role="group" aria-label={t('problems.filter')}>
          <button className={filter === 'all' ? 'on' : ''} onClick={() => setFilter('all')}>{t('problems.all')} {total}</button>
          {ORDER.map(k => <button key={k} className={filter === k ? 'on' : ''} disabled={!counts[k]} onClick={() => setFilter(k)}>{t(SEVERITY_KEY[k])} {counts[k]}</button>)}
        </div>
      </div>
      {list.map((i, k) => (
        <div key={k} className={`prob sev-${i.severity}`} data-testid="problem">
          <span className="sv">{t(SEVERITY_KEY[i.severity])}</span>
          <span className="pl">{multi ? `${baseName(files[i.file]?.path ?? '')}:` : t('editor.line')}{i.line}</span>
          <span className="pm">{i.message}</span>
          <span className="pr" title={t('problems.rule')}>{i.rule}</span>
          <button className="link" onClick={() => goto(i.file, i.line)}>{t('problems.go')}</button>
        </div>
      ))}
      {!list.length && <p className="muted pad">{total ? t('problems.noneFilter') : <Rich text={t('problems.noneAtAll')} />}</p>}
    </div>
  )
}
