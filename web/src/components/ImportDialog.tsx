import { useState } from 'react'
import type { SourceFile } from '../core/types'
import { useApp } from '../store'
import { Modal } from './Modal'

export function ImportDialog({ onClose }: { onClose(): void }) {
  const served = useApp(s => !!s.disk)
  const importFiles = useApp(s => s.importFiles)
  const toast = useApp(s => s.toast)
  const [text, setText] = useState('')
  const [loaded, setLoaded] = useState<SourceFile[]>([])

  const pick = async (list: FileList | null) => {
    if (!list?.length) return
    const read = await Promise.all([...list].map(async f => ({ path: f.name, text: await f.text() })))
    // Como haproxy -f con un directorio: orden lexicográfico por nombre.
    read.sort((a, b) => a.path.localeCompare(b.path))
    setLoaded(served ? read.slice(0, 1) : read)
    setText('')
  }
  const apply = () => {
    const incoming = loaded.length ? loaded : text.trim() ? [{ path: 'haproxy.cfg', text }] : []
    if (!incoming.length) { toast('No hay nada que importar.'); return }
    importFiles(incoming)
    onClose()
  }
  return (
    <Modal title="Importar configuración" wide onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancelar</button>
      <button className="btn primary" onClick={apply}>Reemplazar</button>
    </>}>
      <p className="muted">
        {served
          ? <>Reemplaza el contenido del archivo activo (se guarda una versión antes). Las rutas las fija <code>haro serve</code>.</>
          : <>Pega tu <code>haproxy.cfg</code> o carga uno o varios ficheros (se cargan ordenados por nombre, como <code>haproxy -f</code>). Reemplaza la configuración actual; se guarda una versión antes.</>}
      </p>
      <textarea className="mono big" spellCheck={false} placeholder={'global\n    ...'} aria-label="Texto a importar" value={text} onChange={e => { setText(e.target.value); setLoaded([]) }} />
      <div className="row-btns">
        <label className="btn sm">
          Cargar {served ? 'fichero' : 'ficheros'}…
          <input type="file" accept=".cfg,.conf,.txt,text/plain" multiple={!served} hidden onChange={e => { void pick(e.target.files); e.target.value = '' }} data-testid="import-file" />
        </label>
        {loaded.length > 0 && <span className="muted sm">{loaded.map(f => f.path).join(' → ')}</span>}
      </div>
    </Modal>
  )
}
