import { useState } from 'react'
import type { SourceFile } from '../core/types'
import { useT } from '../i18n/react'
import { Rich } from '../markdown/mini'
import { useApp } from '../store'
import { Modal } from './Modal'

export function ImportDialog({ onClose }: { onClose(): void }) {
  const t = useT()
  const served = useApp(s => !!s.disk)
  const importFiles = useApp(s => s.importFiles)
  const toast = useApp(s => s.toast)
  const [text, setText] = useState('')
  const [loaded, setLoaded] = useState<SourceFile[]>([])

  const pick = async (list: FileList | null) => {
    if (!list?.length) return
    const read = await Promise.all([...list].map(async f => ({ path: f.name, text: await f.text() })))
    // Like haproxy -f with a directory: lexicographic order by name.
    read.sort((a, b) => a.path.localeCompare(b.path))
    setLoaded(served ? read.slice(0, 1) : read)
    setText('')
  }
  const apply = () => {
    const incoming = loaded.length ? loaded : text.trim() ? [{ path: 'haproxy.cfg', text }] : []
    if (!incoming.length) { toast(t('import.nothing')); return }
    importFiles(incoming)
    onClose()
  }
  return (
    <Modal title={t('import.title')} wide onClose={onClose} footer={<>
      <button className="btn" onClick={onClose}>{t('common.cancel')}</button>
      <button className="btn primary" onClick={apply}>{t('import.replace')}</button>
    </>}>
      <p className="muted">
        <Rich text={t(served ? 'import.servedHint' : 'import.localHint')} />
      </p>
      <textarea className="mono big" spellCheck={false} placeholder={'global\n    ...'} aria-label={t('import.textLabel')} value={text} onChange={e => { setText(e.target.value); setLoaded([]) }} />
      <div className="row-btns">
        <label className="btn sm">
          {t(served ? 'import.loadFile' : 'import.loadFiles')}
          <input type="file" accept=".cfg,.conf,.txt,text/plain" multiple={!served} hidden onChange={e => { void pick(e.target.files); e.target.value = '' }} data-testid="import-file" />
        </label>
        {loaded.length > 0 && <span className="muted sm">{loaded.map(f => f.path).join(' → ')}</span>}
      </div>
    </Modal>
  )
}
