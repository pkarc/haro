import { useState, type ReactNode } from 'react'
import { create } from 'zustand'
import { useT } from '../i18n/react'
import { Modal } from './Modal'

// Promise-based dialogs: `await confirmBox(...)`. DialogHost renders them.

interface Entry { id: number; render(done: (v: unknown) => void): ReactNode; resolve(v: unknown): void }
let nextId = 0
const useDialogs = create<{ list: Entry[] }>(() => ({ list: [] }))

export function openDialog<T>(render: (done: (v: T | undefined) => void) => ReactNode): Promise<T | undefined> {
  return new Promise(resolve => {
    const entry: Entry = { id: ++nextId, render: render as Entry['render'], resolve: resolve as Entry['resolve'] }
    useDialogs.setState(s => ({ list: [...s.list, entry] }))
  })
}

export function DialogHost() {
  const list = useDialogs(s => s.list)
  return <>{list.map(e => <div key={e.id}>{e.render(v => {
    useDialogs.setState(s => ({ list: s.list.filter(x => x.id !== e.id) }))
    e.resolve(v)
  })}</div>)}</>
}

function ConfirmForm({ msg, ok, danger, done }: { msg: string; ok?: string; danger: boolean; done(v: boolean): void }) {
  const t = useT()
  return (
    <Modal title={t('dialog.confirmTitle')} onClose={() => done(false)} footer={<>
      <button className="btn" onClick={() => done(false)}>{t('common.cancel')}</button>
      <button className={`btn ${danger ? 'primary danger' : 'primary'}`} data-autofocus onClick={() => done(true)}>{ok ?? t('common.confirm')}</button>
    </>}><p>{msg}</p></Modal>
  )
}

/** `ok` is the label of the confirm button (default: «Confirm»). */
export const confirmBox = async (msg: string, ok?: string, danger = false): Promise<boolean> =>
  (await openDialog<boolean>(done => <ConfirmForm msg={msg} ok={ok} danger={danger} done={done} />)) === true

function PromptForm(props: { title: string; label: string; value: string; check?: { label: string; value: boolean }; done(v: { text: string; check: boolean } | undefined): void }) {
  const t = useT()
  const [text, setText] = useState(props.value)
  const [check, setCheck] = useState(props.check?.value ?? false)
  const submit = () => props.done({ text: text.trim(), check })
  return (
    <Modal title={props.title} onClose={() => props.done(undefined)} footer={<>
      <button className="btn" onClick={() => props.done(undefined)}>{t('common.cancel')}</button>
      <button className="btn primary" onClick={submit}>{t('common.accept')}</button>
    </>}>
      <form onSubmit={e => { e.preventDefault(); submit() }}>
        <label className="fld"><span>{props.label}</span>
          <input type="text" value={text} onChange={e => setText(e.target.value)} spellCheck={false} autoCapitalize="off" />
        </label>
        {props.check && <label className="chk"><input type="checkbox" checked={check} onChange={e => setCheck(e.target.checked)} /> {props.check.label}</label>}
      </form>
    </Modal>
  )
}

export const promptBox = async (title: string, label: string, value = '', check?: { label: string; value: boolean }) =>
  openDialog<{ text: string; check: boolean }>(done => <PromptForm title={title} label={label} value={value} check={check} done={done} />)

function AddSectionForm(props: { type: string; named: boolean; files: string[]; defaultFile: number; done(v: { name: string; file: number } | undefined): void }) {
  const t = useT()
  const [name, setName] = useState('')
  const [file, setFile] = useState(props.defaultFile)
  const submit = () => props.done({ name: name.trim(), file })
  return (
    <Modal title={t('dialog.addSection', { type: props.type })} onClose={() => props.done(undefined)} footer={<>
      <button className="btn" onClick={() => props.done(undefined)}>{t('common.cancel')}</button>
      <button className="btn primary" onClick={submit}>{t('common.add')}</button>
    </>}>
      <form onSubmit={e => { e.preventDefault(); submit() }}>
        {props.named && <label className="fld"><span>{t('dialog.sectionName')}</span>
          <input type="text" value={name} onChange={e => setName(e.target.value)} spellCheck={false} autoCapitalize="off" /></label>}
        {props.files.length > 1 && <label className="fld"><span>{t('dialog.file')}</span>
          <select value={file} onChange={e => setFile(+e.target.value)}>{props.files.map((f, i) => <option key={f} value={i}>{f}</option>)}</select></label>}
      </form>
    </Modal>
  )
}

export const addSectionBox = (type: string, named: boolean, files: string[], defaultFile: number) =>
  openDialog<{ name: string; file: number }>(done => <AddSectionForm type={type} named={named} files={files} defaultFile={defaultFile} done={done} />)
