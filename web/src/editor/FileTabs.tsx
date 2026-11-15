import { useApp } from '../store'
import { dirtyIndexes, fileLabels } from '../store/files'

// Pestañas por archivo; solo se muestran con más de un archivo.
export function FileTabs() {
  const files = useApp(s => s.files)
  const disk = useApp(s => s.disk)
  const active = useApp(s => s.active)
  const setActive = useApp(s => s.setActive)
  const analysis = useApp(s => s.analysis)
  if (files.length < 2) return null
  const labels = fileLabels(files)
  const dirty = new Set(dirtyIndexes(files, disk))
  return (
    <div className="ftabs" role="tablist" aria-label="Archivos">
      {files.map((f, i) => {
        const errs = analysis?.issues.filter(x => x.file === i && x.severity === 'error').length ?? 0
        return (
          <button key={f.path} role="tab" aria-selected={i === active} className={i === active ? 'on' : ''} title={f.path} onClick={() => setActive(i)}>
            {labels[i]}{dirty.has(i) && <span className="dirty" title="Cambios sin guardar"> ●</span>}
            {errs > 0 && <span className="ftab-err" title={`${errs} error(es)`}>{errs}</span>}
          </button>
        )
      })}
    </div>
  )
}
