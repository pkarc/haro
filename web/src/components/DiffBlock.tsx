import { useEffect, useState } from 'react'
import { useT } from '../i18n/react'
import { useCoreClient } from '../store'
import type { DiffOp } from '../core/types'

const CONTEXT = 3

// Line diff computed by the core (CoreService.Diff).
export function DiffBlock({ before, after }: { before: string; after: string }) {
  const t = useT()
  const coreClient = useCoreClient()
  const [ops, setOps] = useState<DiffOp[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    coreClient.diff(before, after).then(o => { if (live) setOps(o) }, e => { if (live) setError((e as Error).message) })
    return () => { live = false }
  }, [coreClient, before, after])
  if (error) return <p className="iss sev-error">{t('diff.failed', { msg: error })}</p>
  if (!ops) return <p className="muted sm">{t('diff.calculating')}</p>
  return <DiffView ops={ops} />
}

export function diffStats(ops: DiffOp[]) {
  return { add: ops.filter(o => o.op === '+').length, del: ops.filter(o => o.op === '-').length }
}

export function DiffView({ ops }: { ops: DiffOp[] }) {
  const t = useT()
  let oldN = 0, newN = 0
  const rows = ops.map(o => {
    if (o.op !== '+') oldN++
    if (o.op !== '-') newN++
    return { ...o, on: o.op !== '+' ? oldN : '', nn: o.op !== '-' ? newN : '' }
  })
  const changed = rows.map(r => r.op !== '=')
  const near = (k: number) => { for (let d = -CONTEXT; d <= CONTEXT; d++) if (changed[k + d]) return true; return false }
  const out: React.ReactNode[] = []
  let skipping = 0
  const flush = (k: number) => { if (skipping) { out.push(<div key={`s${k}`} className="dl skip">{t.n('diff.skip', skipping)}</div>); skipping = 0 } }
  rows.forEach((r, k) => {
    if (r.op === '=' && !near(k)) { skipping++; return }
    flush(k)
    out.push(
      <div key={k} className={`dl ${r.op === '+' ? 'da' : r.op === '-' ? 'dd' : ''}`} data-op={r.op}>
        <span className="n">{r.on}</span><span className="n">{r.nn}</span>
        <span className="s">{r.op === '=' ? ' ' : r.op === '-' ? '−' : '+'}</span><span className="c">{r.text || ' '}</span>
      </div>,
    )
  })
  flush(rows.length)
  const { add, del } = diffStats(ops)
  return (
    <>
      <div className="diff-sum"><span className="add">+{add}</span> <span className="del">−{del}</span> {t('diff.lines')}</div>
      <div className="diff" data-testid="diff">{out.length ? out : <div className="dl skip">{t('diff.none')}</div>}</div>
    </>
  )
}
