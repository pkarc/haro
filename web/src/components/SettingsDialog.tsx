import { useApp } from '../store'
import { confirmBox } from './dialogs'
import { Modal } from './Modal'

export function SettingsDialog({ onClose }: { onClose(): void }) {
  const served = useApp(s => !!s.disk)
  const loadSample = useApp(s => s.loadSample)
  const toast = useApp(s => s.toast)
  const undo = useApp(s => s.undo)
  return (
    <Modal title="Ajustes" onClose={onClose}>
      <p className="muted sm">La sesión (archivos, versiones y preferencias) se guarda solo en el localStorage de este navegador.</p>
      <div className="row-btns">
        {!served && <button className="btn sm" onClick={() => { loadSample(); onClose(); toast('Ejemplo cargado', 'info', { label: 'Deshacer', run: undo }) }}>Cargar configuración de ejemplo</button>}
        <button className="btn sm danger" onClick={async () => {
          onClose()
          if (await confirmBox('Se borrarán archivos, versiones y ajustes guardados en este navegador.', 'Borrar todo', true)) {
            try { localStorage.removeItem('haro.v1') } catch { /* sin acceso al almacenamiento */ }
            location.reload()
          }
        }}>Borrar toda la sesión</button>
      </div>
    </Modal>
  )
}
