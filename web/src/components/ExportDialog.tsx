import { useT } from '../i18n/react'
import { useApp } from '../store'
import { baseName } from '../store/files'
import type { SourceFile } from '../core/types'
import { Rich } from '../markdown/mini'
import { Modal } from './Modal'

function download(f: SourceFile) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([f.text], { type: 'text/plain' }))
  a.download = baseName(f.path)
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 2000)
}

export function ExportDialog({ onClose }: { onClose(): void }) {
  const t = useT()
  const files = useApp(s => s.files)
  const active = useApp(s => s.active)
  const errors = useApp(s => s.analysis?.issues.filter(i => i.severity === 'error').length ?? 0)
  const toast = useApp(s => s.toast)
  const copy = async (text: string) => {
    try { await navigator.clipboard.writeText(text); toast(t('export.copied')) } catch { toast(t('export.copyFailed')) }
  }
  const f = files[active]
  return (
    <Modal title={files.length > 1 ? t('export.title') : t('export.titleOne', { name: baseName(f.path) })} wide onClose={onClose} footer={<>
      <button className="btn" onClick={() => void copy(f.text)}>{files.length > 1 ? t('export.copyThis') : t('export.copy')}</button>
      <button className="btn" onClick={() => files.forEach((x, i) => setTimeout(() => download(x), i * 250))}>{files.length > 1 ? t('export.downloadAll', { n: files.length }) : t('export.download')}</button>
      <button className="btn primary" onClick={onClose}>{t('common.close')}</button>
    </>}>
      {errors > 0 && <div className="iss sev-error"><span>{t('common.warning')}</span> {t('export.errorsDetected', { n: errors })}</div>}
      {files.length > 1 && <p className="muted sm">{t('export.loadOrder', { files: files.map(x => baseName(x.path)).join(' → ') })}</p>}
      <p className="muted sm">{f.path}</p>
      <textarea className="mono big" readOnly spellCheck={false} aria-label={t('export.contentLabel')} value={f.text} />
      <p className="muted sm"><Rich text={t('export.validate')} /></p>
    </Modal>
  )
}
