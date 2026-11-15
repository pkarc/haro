import { useApp } from '../store'
import { baseName } from '../store/files'
import type { SourceFile } from '../core/types'
import { Modal } from './Modal'

function download(f: SourceFile) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([f.text], { type: 'text/plain' }))
  a.download = baseName(f.path)
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 2000)
}

export function ExportDialog({ onClose }: { onClose(): void }) {
  const files = useApp(s => s.files)
  const active = useApp(s => s.active)
  const errors = useApp(s => s.analysis?.issues.filter(i => i.severity === 'error').length ?? 0)
  const toast = useApp(s => s.toast)
  const copy = async (t: string) => {
    try { await navigator.clipboard.writeText(t); toast('Copiado al portapapeles') } catch { toast('No se pudo copiar automáticamente; selecciona el texto y cópialo.') }
  }
  const f = files[active]
  return (
    <Modal title={files.length > 1 ? 'Exportar configuración' : `Exportar ${baseName(f.path)}`} wide onClose={onClose} footer={<>
      <button className="btn" onClick={() => void copy(f.text)}>Copiar {files.length > 1 ? 'este archivo' : ''}</button>
      <button className="btn" onClick={() => files.forEach((x, i) => setTimeout(() => download(x), i * 250))}>Descargar {files.length > 1 ? `los ${files.length} archivos` : ''}</button>
      <button className="btn primary" onClick={onClose}>Cerrar</button>
    </>}>
      {errors > 0 && <div className="iss sev-error"><span>Atención</span> Hay {errors} error(es) detectados. Revísalos antes de desplegar.</div>}
      {files.length > 1 && <p className="muted sm">El orden de carga es el de la lista: {files.map(x => baseName(x.path)).join(' → ')}.</p>}
      <p className="muted sm">{f.path}</p>
      <textarea className="mono big" readOnly spellCheck={false} aria-label="Contenido a exportar" value={f.text} />
      <p className="muted sm">Valida antes de recargar: <code>haproxy -c -f /etc/haproxy/haproxy.cfg</code></p>
    </Modal>
  )
}
