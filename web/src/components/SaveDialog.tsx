import { useEffect, useState } from 'react'
import type { SaveResult } from '../core/configstore'
import type { CheckResult } from '../core/types'
import { useApp, useConfigStore, useCoreClient } from '../store'
import { fileLabels } from '../store/files'
import { DiffBlock } from './DiffBlock'
import { Modal } from './Modal'

// Guardar en disco: diff previo de cada archivo modificado, validación y
// salida de haproxy -c si el servidor rechaza el guardado.
export function SaveDialog({ onClose }: { onClose(): void }) {
  const coreClient = useCoreClient()
  const configStore = useConfigStore()
  const reload = useApp(s => s.reload)
  const files = useApp(s => s.files)
  const disk = useApp(s => s.disk)
  const caps = useApp(s => s.capabilities)
  const save = useApp(s => s.save)
  const toast = useApp(s => s.toast)
  const dirtyFn = useApp(s => s.dirty)
  const [dirty] = useState(dirtyFn)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<SaveResult | null>(null)
  const [check, setCheck] = useState<CheckResult | null>(null)
  const labels = fileLabels(files)
  const remaining = dirtyFn()

  useEffect(() => { if (!dirty.length) onClose() }, [dirty, onClose])

  const validate = async () => {
    setBusy(true)
    try { setCheck(await coreClient.check(files)) } catch (e) { toast(`No se pudo validar: ${(e as Error).message}`, 'error') } finally { setBusy(false) }
  }

  // Se guardan todos los archivos modificados de una vez: el servidor valida el
  // conjunto, así que un cambio que toca varios archivos (renombrar un backend y
  // sus referencias) no queda a medias.
  const doSave = async () => {
    setBusy(true)
    const r = await save(dirty)
    setBusy(false)
    if (r.saved) {
      toast(dirty.length > 1 ? `Guardados ${dirty.length} archivos` : 'Guardado', 'info')
      onClose()
    } else {
      setFailure(r)
    }
  }

  // Conflicto de versión: lo del origen cambió; se recarga (queda una versión con lo local).
  const doReload = async () => {
    setBusy(true)
    const ok = await reload()
    setBusy(false)
    if (ok) { toast(`Recargado desde ${configStore.label}; tus cambios anteriores están en el historial`, 'info'); onClose() }
  }

  return (
    <Modal title={`Guardar cambios en ${configStore.label}`} wide onClose={onClose} footer={<>
      {caps?.canCheck && <button className="btn" disabled={busy} onClick={validate}>Validar con haproxy -c</button>}
      <button className="btn" onClick={onClose}>Cancelar</button>
      {failure?.conflict && <button className="btn" disabled={busy} onClick={doReload} data-testid="reload-store">Recargar desde {configStore.label}</button>}
      <button className="btn primary" disabled={busy || !remaining.length} onClick={doSave} data-testid="confirm-save">{busy ? 'Guardando…' : 'Guardar'}</button>
    </>}>
      {configStore.saveHint && <p className="muted sm">{configStore.saveHint}</p>}
      {check && <CheckOutput check={check} />}
      {failure && <SaveError r={failure} />}
      {dirty.map(i => (
        <section key={files[i].path} className="save-file">
          <h4>{labels[i]} <span className="muted sm">{files[i].path}</span></h4>
          <DiffBlock before={disk?.find(f => f.path === files[i].path)?.text ?? ''} after={files[i].text} />
        </section>
      ))}
    </Modal>
  )
}

export function CheckOutput({ check }: { check: CheckResult }) {
  if (!check.available) return <p className="iss sev-warn"><span>Aviso</span> haproxy no está instalado en el servidor: solo se usa el detector de haro.</p>
  return (
    <div className={`iss ${check.ok ? 'sev-info' : 'sev-error'}`}>
      <span>haproxy -c</span> {check.ok ? 'la configuración es válida' : 'rechazó la configuración'}
      {check.output && <pre className="check-out" data-testid="check-output">{check.output}</pre>}
    </div>
  )
}

function SaveError({ r }: { r: SaveResult }) {
  return (
    <div className="iss sev-error" role="alert" data-testid="save-error">
      <span>{r.conflict ? 'Conflicto' : 'No guardado'}</span> {r.error}{r.conflict && !r.error && 'La versión guardada cambió desde que la cargaste. Recarga para partir de la nueva (tus cambios quedan en el historial).'}
      {r.issue && <div>Detector: {r.path ? `${r.path}:` : ''}{r.issue.line} · {r.issue.message}</div>}
      {r.check?.output && <pre className="check-out" data-testid="check-output">{r.check.output}</pre>}
    </div>
  )
}
