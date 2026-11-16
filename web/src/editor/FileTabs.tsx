import { useT } from '../i18n/react'
import { useApp } from '../store'
import { dirtyIndexes, fileLabels } from '../store/files'

// One tab per file; only shown with more than one file.
export function FileTabs() {
  const t = useT()
  const files = useApp(s => s.files)
  const disk = useApp(s => s.disk)
  const active = useApp(s => s.active)
  const setActive = useApp(s => s.setActive)
  const analysis = useApp(s => s.analysis)
  if (files.length < 2) return null
  const labels = fileLabels(files)
  const dirty = new Set(dirtyIndexes(files, disk))
  return (
    <div className="ftabs" role="tablist" aria-label={t('tabs.files')}>
      {files.map((f, i) => {
        const errs = analysis?.issues.filter(x => x.file === i && x.severity === 'error').length ?? 0
        return (
          <button key={f.path} role="tab" aria-selected={i === active} className={i === active ? 'on' : ''} title={f.path} onClick={() => setActive(i)}>
            {labels[i]}{dirty.has(i) && <span className="dirty" title={t('tabs.unsaved')}> ●</span>}
            {errs > 0 && <span className="ftab-err" title={t.n('problems.error', errs)}>{errs}</span>}
          </button>
        )
      })}
    </div>
  )
}
