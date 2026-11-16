import { useEffect, useRef, type ReactNode } from 'react'
import { useT } from '../i18n/react'

interface Props {
  title: string
  wide?: boolean
  onClose(): void
  footer?: ReactNode
  children: ReactNode
}

// Minimal modal dialog: Escape and a click outside close it, focus moves to the
// first field and returns to the previous element on close.
export function Modal({ title, wide, onClose, footer, children }: Props) {
  const t = useT()
  const ref = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    const el = ref.current
    const first = el?.querySelector<HTMLElement>('input,textarea,select,[data-autofocus]')
    first?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.stopPropagation(); close.current() }
      if (e.key === 'Tab' && el) { // focus does not leave the dialog
        const f = [...el.querySelectorAll<HTMLElement>('button,input,textarea,select,[tabindex]:not([tabindex="-1"])')].filter(x => !x.hasAttribute('disabled'))
        if (!f.length) return
        const i = f.indexOf(document.activeElement as HTMLElement)
        if (e.shiftKey && i <= 0) { e.preventDefault(); f[f.length - 1].focus() }
        else if (!e.shiftKey && i === f.length - 1) { e.preventDefault(); f[0].focus() }
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => { document.removeEventListener('keydown', onKey, true); prev?.focus?.() }
  }, [])
  return (
    <div className="modal-bg" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className={`modal${wide ? ' wide' : ''}`} role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <header><h3>{title}</h3><button className="icon" aria-label={t('common.close')} onClick={onClose}>✕</button></header>
        <div className="modal-b">{children}</div>
        {footer && <footer>{footer}</footer>}
      </div>
    </div>
  )
}
