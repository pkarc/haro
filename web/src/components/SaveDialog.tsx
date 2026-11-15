import { useEffect, useState } from 'react'
import type { CheckResult, PutResult } from '../core/types'
import { coreClient, useApp } from '../store'
import { fileLabels } from '../store/files'
import { DiffBlock } from './DiffBlock'
import { Modal } from './Modal'

type Result = PutResult & { pending?: boolean }

// Guardar en disco: diff previo de cada archivo modificado, validación y
// salida de haproxy -c si el servidor rechaza el guardado.
export function SaveDialog({ onClose }: { onClose(): void }) {
  const files = useApp(s => s.files)
  const disk = useApp(s => s.disk)
  const caps = useApp(s => s.capabilities)
  const save = useApp(s => s.save)
  const toast = useApp(s => s.toast)
  const dirtyFn = useApp(s => s.dirty)
  const [dirty] = useState(dirtyFn)
  const [busy, setBusy] = useState(false)
  const [results, setResults] = useState<Record<number, Result>>({})
  const [check, setCheck] = useState<CheckResult | null>(null)
  const labels = fileLabels(files)
  const remaining = dirtyFn()

  useEffect(() => { if (!dirty.length) onClose() }, [dirty, onClose])

  const validate = async () => {
    setBusy(true)
    try { setCheck(await coreClient.check(files)) } catch (e) { toast(`No se pudo validar: ${(e as Error).message}`, 'error') } finally { setBusy(false) }
  }

  // Cada guardado valida la configuración completa con el resto de archivos tal
  // como están en disco. Si un archivo depende de otro modificado, se reintenta
  // cuando los demás ya se guardaron.
  const doSave = async () => {
    setBusy(true)
    const res: Record<number, Result> = {}
    let pending = [...dirty]
    for (;;) {
      const failed: number[] = []
      let progress = false
      for (const i of pending) {
        const r = await save(i)
        res[i] = r
        if (r.saved) progress = true
        else failed.push(i)
      }
      pending = failed
      if (!pending.length || !progress) break
    }
    setResults(res)
    setBusy(false)
    if (!pending.length) {
      toast(dirty.length > 1 ? `Guardados ${dirty.length} archivos` : 'Guardado', 'info')
      onClose()
    }
  }

  return (
    <Modal title="Guardar cambios en disco" wide onClose={onClose} footer={<>
      {caps?.canCheck && <button className="btn" disabled={busy} onClick={validate}>Validar con haproxy -c</button>}
      <button className="btn" onClick={onClose}>Cancelar</button>
      <button className="btn primary" disabled={busy || !remaining.length} onClick={doSave} data-testid="confirm-save">{busy ? 'Guardando…' : 'Guardar'}</button>
    </>}>
      <p className="muted sm">Se valida la configuración completa antes de escribir y se deja una copia <code>.bak</code> de cada archivo.</p>
      {check && <CheckOutput check={check} />}
      {dirty.map(i => (
        <section key={files[i].path} className="save-file">
          <h4>{labels[i]} <span className="muted sm">{files[i].path}</span></h4>
          {results[i] && !results[i].saved && <SaveError r={results[i]} />}
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

function SaveError({ r }: { r: PutResult }) {
  return (
    <div className="iss sev-error" role="alert" data-testid="save-error">
      <span>No guardado</span> {r.error}
      {r.issue && <div>Detector: {r.path ? `${r.path}:` : ''}{r.issue.line} · {r.issue.message}</div>}
      {r.check?.output && <pre className="check-out" data-testid="check-output">{r.check.output}</pre>}
    </div>
  )
}
