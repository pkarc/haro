import { useEffect, useRef, useState } from 'react'
import { DiffBlock } from '../components/DiffBlock'
import { openDialog } from '../components/dialogs'
import { Modal } from '../components/Modal'
import type { RenderResult, TechTemplate, TemplateParam } from '../core/types'
import { coreClient, useApp } from '../store'
import { initialValues, isVisible, missingRequired, toParams, type FormValues } from './form'

const RENDER_DELAY_MS = 300

type Preview =
  | { state: 'idle'; hint: string }
  | { state: 'loading' }
  | { state: 'error'; message: string }
  | { state: 'ok'; result: RenderResult }

function Field({ p, value, onChange }: { p: TemplateParam; value: string | boolean; onChange(v: string | boolean): void }) {
  const hint = p.help ? <small className="muted">{p.help}</small> : null
  if (p.type === 'bool') {
    return (
      <div className="fld tpl-bool">
        <label className="chk"><input type="checkbox" checked={value === true} onChange={e => onChange(e.target.checked)} /> {p.label}</label>
        {hint}
      </div>
    )
  }
  const label = <span>{p.label}{p.required ? ' *' : ''}</span>
  if (p.type === 'list') {
    return (
      <label className="fld">{label}
        <textarea className="mono" rows={Math.max(2, String(value).split('\n').length)} spellCheck={false} placeholder={p.example}
          value={String(value)} onChange={e => onChange(e.target.value)} />
        {hint}
      </label>
    )
  }
  return (
    <label className="fld">{label}
      <input type={p.type === 'int' ? 'number' : 'text'} spellCheck={false} autoCapitalize="off" placeholder={p.example}
        value={String(value)} onChange={e => onChange(e.target.value)} />
      {hint}
    </label>
  )
}

export function TemplateDialog({ onClose }: { onClose(): void }) {
  const files = useApp(s => s.files)
  const lang = useApp(s => s.lang)
  const applyTemplate = useApp(s => s.applyTemplate)
  const [list, setList] = useState<TechTemplate[] | null>(null)
  const [loadError, setLoadError] = useState('')
  const [id, setId] = useState('')
  const [values, setValues] = useState<FormValues>({})
  const [preview, setPreview] = useState<Preview>({ state: 'idle', hint: 'Elige una tecnología.' })
  const [busy, setBusy] = useState(false)
  const req = useRef(0)

  useEffect(() => {
    let live = true
    coreClient.templates(lang).then(l => { if (live) setList(l) }, e => { if (live) setLoadError((e as Error).message) })
    return () => { live = false }
  }, [lang])

  const tpl = list?.find(t => t.id === id)

  const choose = (nid: string) => {
    setId(nid)
    const t = list?.find(x => x.id === nid)
    setValues(t ? initialValues(t) : {})
  }

  // Vista previa: cada cambio del formulario vuelve a pedir el resultado al núcleo.
  useEffect(() => {
    if (!tpl) { setPreview({ state: 'idle', hint: 'Elige una tecnología.' }); return }
    const missing = missingRequired(tpl, values)
    if (missing.length) {
      const names = missing.map(k => tpl.params.find(p => p.key === k)?.label ?? k)
      setPreview({ state: 'idle', hint: `Rellena: ${names.join(', ')}.` })
      return
    }
    const mine = ++req.current
    setPreview({ state: 'loading' })
    const timer = setTimeout(() => {
      coreClient.renderTemplate(files, tpl.id, toParams(tpl, values), lang).then(
        result => { if (mine === req.current) setPreview({ state: 'ok', result }) },
        e => { if (mine === req.current) setPreview({ state: 'error', message: (e as Error).message }) },
      )
    }, RENDER_DELAY_MS)
    return () => clearTimeout(timer)
  }, [tpl, values, files, lang])

  const ready = preview.state === 'ok' && preview.result.changes.length > 0
  const confirm = async () => {
    if (preview.state !== 'ok' || !tpl) return
    setBusy(true)
    const ok = await applyTemplate(preview.result.ops, tpl.name)
    setBusy(false)
    if (ok) onClose()
  }

  return (
    <Modal title="Añadir desde plantilla de tecnología" wide onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>Cancelar</button>
      <button className="btn primary" disabled={!ready || busy} onClick={() => void confirm()}>Añadir a la configuración</button>
    </>}>
      {loadError && <p className="iss sev-error">No se pudieron cargar las plantillas: {loadError}</p>}
      {!list && !loadError && <p className="muted">Cargando plantillas…</p>}
      {list && (
        <div className="tpl">
          <div className="tpl-form">
            <label className="fld"><span>Tecnología</span>
              <select aria-label="Tecnología" value={id} onChange={e => choose(e.target.value)}>
                <option value="">Elige una…</option>
                {list.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
              </select>
            </label>
            {tpl && <p className="muted sm tpl-desc">{tpl.description}</p>}
            {tpl?.params.filter(p => isVisible(p, values)).map(p => (
              <Field key={p.key} p={p} value={values[p.key] ?? ''} onChange={v => setValues(prev => ({ ...prev, [p.key]: v }))} />
            ))}
          </div>
          <div className="tpl-preview" data-testid="template-preview">
            <h4>Cambios</h4>
            {preview.state === 'idle' && <p className="muted sm">{preview.hint}</p>}
            {preview.state === 'loading' && <p className="muted sm">Calculando…</p>}
            {preview.state === 'error' && <p className="iss sev-error" role="alert">{preview.message}</p>}
            {preview.state === 'ok' && !preview.result.changes.length && <p className="muted sm">La plantilla no cambia ningún archivo.</p>}
            {preview.state === 'ok' && preview.result.changes.map(c => (
              <div key={c.file}>
                {files.length > 1 && <h5 className="mono">{c.path}</h5>}
                <DiffBlock before={files[c.file]?.text ?? ''} after={c.text} />
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  )
}

export const templateBox = () => openDialog<boolean>(done => <TemplateDialog onClose={() => done(undefined)} />)
