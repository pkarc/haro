import { useEffect } from 'react'
import { useApp } from '../store'
import type { Toast } from '../store/store'

function ToastItem({ t }: { t: Toast }) {
  const dismiss = useApp(s => s.dismissToast)
  useEffect(() => {
    const id = setTimeout(() => dismiss(t.id), t.kind === 'info' ? 4600 : 9000)
    return () => clearTimeout(id)
  }, [t.id, t.kind, dismiss])
  return (
    <div className={`toast ${t.kind}`} role={t.kind === 'info' ? 'status' : 'alert'}>
      <span>{t.msg}</span>
      {t.action && <button className="link" onClick={() => { t.action!.run(); dismiss(t.id) }}>{t.action.label}</button>}
      <button className="icon" aria-label="Cerrar aviso" onClick={() => dismiss(t.id)}>✕</button>
    </div>
  )
}

export function Toasts() {
  const toasts = useApp(s => s.toasts)
  return <div id="toastRoot">{toasts.map(t => <ToastItem key={t.id} t={t} />)}</div>
}
