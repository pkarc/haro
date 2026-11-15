import type { Analysis, Knowledge } from '../core/types'
import { Rich } from '../markdown/mini'
import { lineKey, SEVERITY_LABEL, type AnalysisIndex } from '../store/selectors'

interface Props { analysis: Analysis | null; index: AnalysisIndex; knowledge: Knowledge | null; file: number; line: number }

// Barra inferior: explica la línea bajo el cursor (el texto lo calcula el núcleo).
export function CodeStatus({ analysis, index, knowledge, file, line }: Props) {
  let section: Analysis['doc']['sections'][number] | null = null
  for (const s of analysis?.doc.sections ?? []) {
    if (s.file === file && s.line <= line && (!section || s.line >= section.line)) section = s
  }
  const info = section ? knowledge?.sections[section.type] : undefined
  const isHeader = section?.line === line
  const l = section && !isHeader ? section.lines.find(x => x.line === line) : undefined
  const ex = index.explanations.get(lineKey(file, line))
  const issues = index.issues.get(lineKey(file, line)) ?? []

  return (
    <div className="status" data-testid="code-status">
      <span className="st-ln">L{line}</span>
      {section && <span className="badge sm" style={{ '--c': info?.color } as React.CSSProperties}>{section.type}{section.name ? ` ${section.name}` : ''}</span>}
      {isHeader && info && <span className="st-x"><b>{info.title}.</b> {info.description}</span>}
      {!isHeader && ex && <><code className="kw">{ex.keyword}</code><span className="st-x"><Rich text={ex.explanation.text} /></span></>}
      {!isHeader && !ex && l?.kind === 'comment' && <span className="st-x muted">Comentario</span>}
      {!isHeader && !ex && l?.kind !== 'comment' && <span className="st-x muted">Sitúa el cursor sobre una línea para ver qué hace.</span>}
      {issues.map((i, k) => <span key={k} className={`st-iss sev-${i.severity}`}>{SEVERITY_LABEL[i.severity]}: {i.message}</span>)}
    </div>
  )
}
