import { useEffect, useState } from 'react'
import { AssistantDisabled, ChatPanel } from './assistant/ChatPanel'
import { DialogHost } from './components/dialogs'
import { Toasts } from './components/Toasts'
import { TopBar } from './components/TopBar'
import { useMediaQuery } from './components/useMediaQuery'
import { CodeView } from './editor/CodeView'
import { ProblemsButton, ProblemsPanel } from './problems/ProblemsPanel'
import { useApp } from './store'
import { VisualView } from './visual/VisualView'

export function App() {
  const ready = useApp(s => s.ready)
  const mode = useApp(s => s.mode)
  const setMode = useApp(s => s.setMode)
  const showAssistant = useApp(s => s.showAssistant)
  const canAssist = useApp(s => !!s.capabilities?.canAssist)
  const chatOpen = useApp(s => s.ui.chatOpen)
  const setChatOpen = useApp(s => s.setChatOpen)
  const setShowAssistant = useApp(s => s.setShowAssistant)
  const error = useApp(s => s.analysisError)
  const mobile = useMediaQuery('(max-width: 899px)')
  const [problems, setProblems] = useState(false)

  useEffect(() => { void useApp.getState().init() }, [])

  // Ctrl/Cmd+Z fuera de campos de texto (dentro, cada uno gestiona el suyo).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return
      const t = e.target as HTMLElement | null
      if (t?.closest('input,textarea,select,[contenteditable="true"],.cm-editor')) return
      const k = e.key.toLowerCase()
      if (k === 'z') { e.preventDefault(); if (e.shiftKey) useApp.getState().redo(); else useApp.getState().undo() }
      else if (k === 'y') { e.preventDefault(); useApp.getState().redo() }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  if (!ready) return <div id="app"><p className="muted pad">Cargando…</p></div>

  const assistantVisible = mobile && showAssistant
  return (
    <div id="app" className={mobile ? 'mobile' : ''}>
      <TopBar />
      {error && <div className="iss sev-error net-error" role="alert"><span>Núcleo</span> {error}</div>}
      <main className={`layout${canAssist && !mobile && chatOpen ? ' with-chat' : ''}`}>
        {!assistantVisible && (
          <section className="pane main-pane">
            <div className="pane-head">
              <div className="seg" role="tablist" aria-label="Modo de edición">
                <button role="tab" aria-selected={mode === 'code'} className={mode === 'code' ? 'on' : ''} onClick={() => setMode('code')}>Código</button>
                <button role="tab" aria-selected={mode === 'visual'} className={mode === 'visual' ? 'on' : ''} onClick={() => setMode('visual')}>Visual</button>
              </div>
              <ProblemsButton open={problems} onToggle={() => setProblems(p => !p)} />
              {canAssist && !mobile && (
                <button className={`tbtn assist-toggle${chatOpen ? ' on' : ''}`} aria-pressed={chatOpen} data-testid="assistant-toggle" onClick={() => setChatOpen(!chatOpen)}>✦ Asistente</button>
              )}
            </div>
            {problems && <ProblemsPanel />}
            <div className="pane-body">{mode === 'code' ? <CodeView /> : <VisualView />}</div>
          </section>
        )}
        {assistantVisible && (
          <section className="pane assistant-pane" data-testid="assistant-pane">
            {canAssist ? <ChatPanel /> : <AssistantDisabled />}
          </section>
        )}
        {canAssist && !mobile && chatOpen && (
          <section className="pane assistant-pane side" data-testid="assistant-pane" aria-label="Asistente"><ChatPanel /></section>
        )}
      </main>
      {mobile && (
        <nav className="mobile-tabs" role="tablist" aria-label="Secciones">
          <button role="tab" aria-selected={!showAssistant && mode === 'code'} className={!showAssistant && mode === 'code' ? 'on' : ''} onClick={() => setMode('code')}>‹/› Código</button>
          <button role="tab" aria-selected={!showAssistant && mode === 'visual'} className={!showAssistant && mode === 'visual' ? 'on' : ''} onClick={() => setMode('visual')}>◫ Visual</button>
          <button role="tab" aria-selected={showAssistant} className={showAssistant ? 'on' : ''} onClick={() => setShowAssistant(true)}>✦ Asistente</button>
        </nav>
      )}
      <DialogHost />
      <Toasts />
    </div>
  )
}
