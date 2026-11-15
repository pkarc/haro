import { useEffect, useMemo, useState } from 'react'
import { HttpCoreClient } from './core/client'
import type { Analysis, SourceFile } from './core/types'

// Punto de partida mínimo: un editor por archivo + explicaciones + problemas.
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
  const [files, setFiles] = useState<SourceFile[]>([{ path: 'haproxy.cfg', text: SAMPLE }])
  const [active, setActive] = useState(0)
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [error, setError] = useState('')

  useEffect(() => { core.getConfig().then(f => f && f.length && setFiles(f)).catch(() => {}) }, [])
  useEffect(() => {
    const t = setTimeout(() => core.analyze(files).then(a => { setAnalysis(a); setError('') }).catch(e => setError(e.message)), 200)
    return () => clearTimeout(t)
  }, [files])
  const setText = (text: string) => setFiles(fs => fs.map((f, i) => i === active ? { ...f, text } : f))
  const text = files[active]?.text ?? ''

  const bySection = useMemo(() => analysis?.doc.sections.map((s, i) => ({ s, ex: analysis.explanations.filter(e => e.section === i) })) ?? [], [analysis])

  return (
    <main style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, padding: 16, fontFamily: 'system-ui' }}>
      <div>
        {files.length > 1 && <nav>{files.map((f, i) => <button key={f.path} onClick={() => setActive(i)} disabled={i === active}>{f.path.split('/').pop()}</button>)}</nav>}
        <textarea value={text} onChange={e => setText(e.target.value)} spellCheck={false} style={{ fontFamily: 'monospace', width: '100%', minHeight: '80vh' }} />
      </div>
      <section>
        {error && <p style={{ color: 'crimson' }}>{error}</p>}
        {analysis?.issues.map((i, k) => <p key={k}>[{i.severity}] {files[i.file]?.path.split('/').pop()}:{i.line} {i.message}</p>)}
        {bySection.map(({ s, ex }) => (
          <article key={`${s.file}:${s.line}`}>
            <h3>{s.type} {s.name}</h3>
            {ex.map(e => <p key={`${e.file}:${e.line}`}><code>{e.keyword} {e.args}</code><br /><Rich text={e.explanation.text} /></p>)}
          </article>
        ))}
      </section>
    </main>
  )
}
