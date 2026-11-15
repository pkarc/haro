import { useEffect, useMemo, useState } from 'react'
import { HttpCoreClient } from './core/client'
import type { Analysis } from './core/types'

// Punto de partida mínimo: editor de texto + explicaciones + problemas.
// Aquí se integrarán CodeMirror 6 (modo código), las tarjetas por sección
// (modo visual), React Flow (mapa) y el asistente, portando el prototipo.

const core = new HttpCoreClient()
const SAMPLE = `frontend web\n    bind *:80\n    default_backend app\n\nbackend app\n    balance roundrobin\n    server a1 10.0.0.1:8080 check\n`

// El núcleo devuelve markdown mínimo: **negrita** y `código`.
function Rich({ text }: { text: string }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`)/g)
  return <>{parts.map((p, i) => p.startsWith('**') ? <b key={i}>{p.slice(2, -2)}</b> : p.startsWith('`') ? <code key={i}>{p.slice(1, -1)}</code> : p)}</>
}

export function App() {
  const [text, setText] = useState(SAMPLE)
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [error, setError] = useState('')

  useEffect(() => { core.getConfig().then(c => c && setText(c.text)).catch(() => {}) }, [])
  useEffect(() => {
    const t = setTimeout(() => core.analyze(text).then(a => { setAnalysis(a); setError('') }).catch(e => setError(e.message)), 200)
    return () => clearTimeout(t)
  }, [text])

  const bySection = useMemo(() => analysis?.doc.sections.map((s, i) => ({ s, ex: analysis.explanations.filter(e => e.section === i) })) ?? [], [analysis])

  return (
    <main style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, padding: 16, fontFamily: 'system-ui' }}>
      <textarea value={text} onChange={e => setText(e.target.value)} spellCheck={false} style={{ fontFamily: 'monospace', minHeight: '80vh' }} />
      <section>
        {error && <p style={{ color: 'crimson' }}>{error}</p>}
        {analysis?.issues.map((i, k) => <p key={k}>[{i.severity}] L{i.line}: {i.message}</p>)}
        {bySection.map(({ s, ex }) => (
          <article key={s.line}>
            <h3>{s.type} {s.name}</h3>
            {ex.map(e => <p key={e.line}><code>{e.keyword} {e.args}</code><br /><Rich text={e.explanation.text} /></p>)}
          </article>
        ))}
      </section>
    </main>
  )
}
