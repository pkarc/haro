import { useState } from 'react'
import { useApp, useAppContext } from '../store'
import { dirtyIndexes } from '../store/files'
import { ExportDialog } from './ExportDialog'
import { HistoryDialog } from './HistoryDialog'
import { ImportDialog } from './ImportDialog'
import { SaveDialog } from './SaveDialog'
import { SettingsDialog } from './SettingsDialog'

type Dlg = 'history' | 'import' | 'export' | 'save' | 'settings' | null

export function TopBar() {
  const undo = useApp(s => s.undo)
  const redo = useApp(s => s.redo)
  const canUndo = useApp(s => s.undoStack.length > 0)
  const canRedo = useApp(s => s.redoStack.length > 0)
  const { configStore, actions } = useAppContext()
  const canSave = configStore.canSave
  const nDirty = useApp(s => dirtyIndexes(s.files, s.disk).length)
  const served = useApp(s => !!s.disk)
  const toast = useApp(s => s.toast)
  const [dlg, setDlg] = useState<Dlg>(null)
  const close = () => setDlg(null)

  const onSave = () => {
    if (!canSave) { toast(configStore.readOnlyHint ?? 'Esta configuración es de solo lectura.', 'warn'); return }
    if (!nDirty) { toast('No hay cambios sin guardar.'); return }
    setDlg('save')
  }

  return (
    <header className="top">
      <div className="brand">
        <svg viewBox="0 0 32 32" width="22" height="22" aria-hidden="true"><circle cx="6" cy="16" r="3.2" fill="#38bdf8" /><circle cx="26" cy="7" r="3" fill="#fbbf24" /><circle cx="26" cy="16" r="3" fill="#fbbf24" /><circle cx="26" cy="25" r="3" fill="#fbbf24" /><path d="M9 16 C16 16 16 7 23 7 M9 16 H23 M9 16 C16 16 16 25 23 25" stroke="#5eead4" strokeWidth="1.8" fill="none" /></svg>
        <span><b>haro</b></span>
      </div>
      <div className="top-actions">
        <button className="icon" aria-label="Deshacer" title="Deshacer (Ctrl+Z)" disabled={!canUndo} onClick={undo}>↶</button>
        <button className="icon" aria-label="Rehacer" title="Rehacer (Ctrl+Shift+Z)" disabled={!canRedo} onClick={redo}>↷</button>
        <span className="sep" />
        <button className="tbtn" title="Historial de versiones" onClick={() => setDlg('history')}><span className="ic">⟲</span><span className="lbl">Historial</span></button>
        <button className="tbtn" title="Importar" onClick={() => setDlg('import')}><span className="ic">⇣</span><span className="lbl">Importar</span></button>
        <button className="tbtn" title="Exportar" onClick={() => setDlg('export')}><span className="ic">⇡</span><span className="lbl">Exportar</span></button>
        {served && (
          <button className={`tbtn save${nDirty ? ' on' : ''}${canSave ? '' : ' ro'}`} title={canSave ? `Guardar en ${configStore.label}` : `Solo lectura: ${configStore.readOnlyHint ?? ''}`} onClick={onSave} data-testid="save-button">
            <span className="ic">💾</span><span className="lbl">Guardar{nDirty ? ` (${nDirty})` : ''}</span>
          </button>
        )}
        {actions}
        <button className="icon" aria-label="Ajustes" title="Ajustes" onClick={() => setDlg('settings')}>⚙</button>
      </div>
      {dlg === 'history' && <HistoryDialog onClose={close} />}
      {dlg === 'import' && <ImportDialog onClose={close} />}
      {dlg === 'export' && <ExportDialog onClose={close} />}
      {dlg === 'save' && <SaveDialog onClose={close} />}
      {dlg === 'settings' && <SettingsDialog onClose={close} />}
    </header>
  )
}
