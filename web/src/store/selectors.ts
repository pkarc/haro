import { useMemo } from 'react'
import type { Analysis, Issue, LineExplanation } from '../core/types'

export interface AnalysisIndex {
  explanations: Map<string, LineExplanation>
  issues: Map<string, Issue[]>
}

export const lineKey = (file: number, line: number) => `${file}:${line}`

export function indexAnalysis(a: Analysis | null): AnalysisIndex {
  const explanations = new Map<string, LineExplanation>()
  const issues = new Map<string, Issue[]>()
  if (a) {
    for (const e of a.explanations) explanations.set(lineKey(e.file, e.line), e)
    for (const i of a.issues) {
      const k = lineKey(i.file, i.line)
      issues.set(k, [...(issues.get(k) ?? []), i])
    }
  }
  return { explanations, issues }
}

export function useAnalysisIndex(a: Analysis | null): AnalysisIndex {
  return useMemo(() => indexAnalysis(a), [a])
}

const RANK = { error: 3, warn: 2, info: 1 } as const
export const worstSeverity = (list: Issue[]): Issue['severity'] =>
  list.reduce<Issue['severity']>((w, i) => RANK[i.severity] > RANK[w] ? i.severity : w, 'info')

export const SEVERITY_LABEL = { error: 'Error', warn: 'Aviso', info: 'Sugerencia' } as const
