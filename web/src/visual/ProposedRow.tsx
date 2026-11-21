import { useId } from 'react'
import type { Issue } from '../core/types'
import { useT } from '../i18n/react'
import { Rich } from '../markdown/mini'
import { SEVERITY_KEY } from '../store/selectors'
import type { ProposedItem } from '../store/proposals'

interface Props {
  item: ProposedItem
  /** The section changed after this was proposed. */
  stale: boolean
  busy: boolean
  onApprove(): void
  onReject(): void
}

/**
 * A directive proposed by the assistant, shown inside its section but clearly not
 * part of the configuration yet: it cannot be edited, moved or revealed in the code.
 */
export function ProposedRow({ item, stale, busy, onApprove, onReject }: Props) {
  const t = useT()
  const id = useId()
  const text = item.text.trim()
  const sp = text.search(/\s/)
  const kw = sp < 0 ? text : text.slice(0, sp)
  const args = sp < 0 ? '' : text.slice(sp + 1).trim()
  const issues: Issue[] = item.issuesAfter
  return (
    <div className="row proposed" role="group" aria-labelledby={`${id}-code`} data-testid="proposed">
      <div className="prop-main">
        <div className="dir-code">
          <span className="prop-badge">{t('propose.badge')}</span>
          <span id={`${id}-code`} className="prop-text"><code className="kw">{kw}</code>{args && <>{' '}<code className="args">{args}</code></>}</span>
        </div>
        {item.explanation && <div className="dir-exp"><Rich text={item.explanation} /></div>}
        {!item.known && <div className="iss sev-warn" data-testid="proposed-unknown"><span>{t('severity.warn')}</span> {t('propose.unknown')}</div>}
        {issues.map((i, k) => <div key={k} className={`iss sev-${i.severity}`} data-testid="proposed-issue"><span>{t(SEVERITY_KEY[i.severity])}</span> {i.message}</div>)}
        {stale && <div className="iss sev-info" data-testid="proposed-stale">{t('propose.stale')}</div>}
        <div className="prop-btns">
          <button type="button" className="btn sm primary" disabled={busy} onClick={onApprove}>{t('propose.approve')}</button>
          <button type="button" className="btn sm" disabled={busy} onClick={onReject}>{t('propose.reject')}</button>
        </div>
      </div>
    </div>
  )
}
