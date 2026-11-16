import { LANGS, type Lang } from '../core/types'
import { useApp, useConfigStore } from '../store'
import { DEFAULT_STORAGE_KEY } from '../store/store'
import { confirmBox } from './dialogs'
import { Modal } from './Modal'

export function SettingsDialog({ onClose }: { onClose(): void }) {
  const configStore = useConfigStore()
  const served = useApp(s => !!s.disk)
  const loadSample = useApp(s => s.loadSample)
  const toast = useApp(s => s.toast)
  const undo = useApp(s => s.undo)
  const lang = useApp(s => s.lang)
  const setLang = useApp(s => s.setLang)
  return (
    <Modal title="Ajustes" onClose={onClose}>
      <p className="muted sm">La sesión (archivos, versiones y preferencias) se guarda solo en el localStorage de este navegador.</p>
      <label className="fld"><span>Idioma</span>
        <select aria-label="Idioma de las explicaciones" value={lang} onChange={e => void setLang(e.target.value as Lang)}>
          {LANGS.map(l => <option key={l.value} value={l.value}>{l.label}</option>)}
        </select>
      </label>
      <p className="muted sm">Cambia solo el texto que produce el núcleo (explicaciones y avisos del detector); la interfaz sigue en español.</p>
      <div className="row-btns">
        {!served && <button className="btn sm" onClick={() => { loadSample(); onClose(); toast('Ejemplo cargado', 'info', { label: 'Deshacer', run: undo }) }}>Cargar configuración de ejemplo</button>}
        <button className="btn sm danger" onClick={async () => {
          onClose()
          if (await confirmBox('Se borrarán archivos, versiones y ajustes guardados en este navegador.', 'Borrar todo', true)) {
            try { localStorage.removeItem(configStore.storageKey ?? DEFAULT_STORAGE_KEY) } catch { /* sin acceso al almacenamiento */ }
            location.reload()
          }
        }}>Borrar toda la sesión</button>
      </div>
    </Modal>
  )
}
