import { useState } from 'react'
import type { Severity } from '../core/types'
import { useApp } from '../store'
import { baseName } from '../store/files'
import { SEVERITY_LABEL } from '../store/selectors'

const ORDER: Severity[] = ['error', 'warn', 'info']
const PLURAL = { error: ['error', 'errores'], warn: ['aviso', 'avisos'], info: ['sugerencia', 'sugerencias'] } as const

export function useIssueCounts() {
  const issues = useApp(s => s.analysis?.issues)
  const c: Record<Severity, number> = { error: 0, warn: 0, info: 0 }
  issues?.forEach(i => c[i.severity]++)
  return { counts: c, total: issues?.length ?? 0 }
}

// Botón-resumen del encabezado: abre y cierra el panel de problemas.
export function ProblemsButton({ open, onToggle }: { open: boolean; onToggle(): void }) {
  const { counts, total } = useIssueCounts()
  return (
    <button className="probs-btn" onClick={onToggle} aria-expanded={open} data-testid="problems-button">
      {!total
        ? <span className="ok-t">✓ Sin problemas</span>
        : ORDER.filter(k => counts[k]).map(k => <span key={k} className={`cnt sev-${k}`}>{counts[k]} {PLURAL[k][counts[k] > 1 ? 1 : 0]}</span>)}
    </button>
  )
}

export function ProblemsPanel() {
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
        <b>Problemas detectados</b>
        <div className="seg sm" role="group" aria-label="Filtrar por gravedad">
          <button className={filter === 'all' ? 'on' : ''} onClick={() => setFilter('all')}>Todos {total}</button>
          {ORDER.map(k => <button key={k} className={filter === k ? 'on' : ''} disabled={!counts[k]} onClick={() => setFilter(k)}>{SEVERITY_LABEL[k]} {counts[k]}</button>)}
        </div>
      </div>
      {list.map((i, k) => (
        <div key={k} className={`prob sev-${i.severity}`} data-testid="problem">
          <span className="sv">{SEVERITY_LABEL[i.severity]}</span>
          <span className="pl">{multi ? `${baseName(files[i.file]?.path ?? '')}:` : 'L'}{i.line}</span>
          <span className="pm">{i.message}</span>
          <span className="pr" title="Identificador de la regla">{i.rule}</span>
          <button className="link" onClick={() => goto(i.file, i.line)}>Ir</button>
        </div>
      ))}
      {!list.length && <p className="muted pad">{total ? 'Ningún problema con este filtro.' : <>No se detectaron problemas. (El análisis es orientativo; valida siempre con <code>haproxy -c</code>.)</>}</p>}
    </div>
  )
}
