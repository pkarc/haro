import type { Analysis, Knowledge } from '../core/types'
import { Rich } from '../markdown/mini'
import { useT } from '../i18n/react'
import { lineKey, SEVERITY_KEY, type AnalysisIndex } from '../store/selectors'

interface Props { analysis: Analysis | null; index: AnalysisIndex; knowledge: Knowledge | null; file: number; line: number }

// Bottom bar: explains the line under the cursor (the core computes the text).
export function CodeStatus({ analysis, index, knowledge, file, line }: Props) {
  const t = useT()
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
      <span className="st-ln">{t('editor.line')}{line}</span>
      {section && <span className="badge sm" style={{ '--c': info?.color } as React.CSSProperties}>{section.type}{section.name ? ` ${section.name}` : ''}</span>}
      {isHeader && info && <span className="st-x"><b>{info.title}.</b> {info.description}</span>}
      {!isHeader && ex && <><code className="kw">{ex.keyword}</code><span className="st-x"><Rich text={ex.explanation.text} /></span></>}
      {!isHeader && !ex && l?.kind === 'comment' && <span className="st-x muted">{t('editor.comment')}</span>}
      {!isHeader && !ex && l?.kind !== 'comment' && <span className="st-x muted">{t('editor.hint')}</span>}
      {issues.map((i, k) => <span key={k} className={`st-iss sev-${i.severity}`}>{t(SEVERITY_KEY[i.severity])}: {i.message}</span>)}
    </div>
  )
}
