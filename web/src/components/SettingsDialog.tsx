import { LANGS, type Lang } from '../core/types'
import { useT } from '../i18n/react'
import { useApp, useConfigStore } from '../store'
import { DEFAULT_STORAGE_KEY } from '../store/store'
import { confirmBox } from './dialogs'
import { Modal } from './Modal'

export function SettingsDialog({ onClose }: { onClose(): void }) {
  const t = useT()
  const configStore = useConfigStore()
  const served = useApp(s => !!s.disk)
  const loadSample = useApp(s => s.loadSample)
  const toast = useApp(s => s.toast)
  const undo = useApp(s => s.undo)
  const lang = useApp(s => s.lang)
  const setLang = useApp(s => s.setLang)
  return (
    <Modal title={t('settings.title')} onClose={onClose}>
      <p className="muted sm">{t('settings.sessionNote')}</p>
      <label className="fld"><span>{t('settings.language')}</span>
        <select aria-label={t('settings.language')} value={lang} onChange={e => void setLang(e.target.value as Lang)}>
          {LANGS.map(l => <option key={l.value} value={l.value}>{l.label}</option>)}
        </select>
      </label>
      <p className="muted sm">{t('settings.languageNote')}</p>
      <div className="row-btns">
        {!served && <button className="btn sm" onClick={() => { loadSample(); onClose(); toast(t('settings.sampleLoaded'), 'info', { label: t('common.undo'), run: undo }) }}>{t('settings.loadSample')}</button>}
        <button className="btn sm danger" onClick={async () => {
          onClose()
          if (await confirmBox(t('settings.clearConfirm'), t('settings.clearOk'), true)) {
            try { localStorage.removeItem(configStore.storageKey ?? DEFAULT_STORAGE_KEY) } catch { /* no access to storage */ }
            location.reload()
          }
        }}>{t('settings.clear')}</button>
      </div>
    </Modal>
  )
}
