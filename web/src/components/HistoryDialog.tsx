import { useState } from 'react'
import { useApp } from '../store'
import { Modal } from './Modal'
import { DiffBlock } from './DiffBlock'
import { baseName } from '../store/files'
import type { SourceFile } from '../core/types'

const lines = (files: SourceFile[]) => files.reduce((n, f) => n + f.text.split('\n').length, 0)

export function HistoryDialog({ onClose }: { onClose(): void }) {
  const versions = useApp(s => s.versions)
  const files = useApp(s => s.files)
  const saveVersion = useApp(s => s.saveVersion)
  const restore = useApp(s => s.restoreVersion)
  const del = useApp(s => s.deleteVersion)
  const toast = useApp(s => s.toast)
  const [label, setLabel] = useState('')
  const [cmp, setCmp] = useState<number | null>(null)
  const v = cmp !== null ? versions[cmp] : null

  return (
    <Modal title="Historial de versiones" wide onClose={onClose}>
      <form className="hist-save" onSubmit={e => { e.preventDefault(); saveVersion(label.trim() || 'Guardada manualmente'); setLabel(''); toast('Versión guardada') }}>
        <input type="text" placeholder="Nombre de la versión (opcional)" aria-label="Nombre de la versión" value={label} onChange={e => setLabel(e.target.value)} />
        <button className="btn sm primary" type="submit">Guardar versión actual</button>
      </form>
      {v && (
        <div className="hist-cmp">
          <div className="hist-cmp-h"><b>Actual → «{v.label}»</b><button className="btn sm" onClick={() => setCmp(null)}>Cerrar comparación</button></div>
          {v.files.map(f => {
            const cur = files.find(x => x.path === f.path)?.text ?? ''
            return cur === f.text ? null : <div key={f.path}><h4>{baseName(f.path)}</h4><DiffBlock before={cur} after={f.text} /></div>
          })}
        </div>
      )}
      {versions.length
        ? versions.map((x, k) => (
          <div className="hist" key={x.ts + ':' + k} data-testid="version">
            <div><b>{x.label}</b><div className="muted sm">{new Date(x.ts).toLocaleString()} · {x.files.length > 1 ? `${x.files.length} archivos · ` : ''}{lines(x.files)} líneas</div></div>
            <div className="hist-b">
              <button className="btn sm" onClick={() => setCmp(k)}>Comparar</button>
              <button className="btn sm" onClick={() => { restore(k); onClose() }}>Restaurar</button>
              <button className="icon danger" aria-label="Borrar versión" title="Borrar" onClick={() => del(k)}>✕</button>
            </div>
          </div>
        ))
        : <p className="muted">Aún no hay versiones. Se guardan automáticamente antes de importar o restaurar.</p>}
    </Modal>
  )
}
