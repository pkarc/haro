import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { AssistantDisabled, ChatPanel } from './assistant/ChatPanel'
import { useT } from './i18n/react'
import { DialogHost } from './components/dialogs'
import { Toasts } from './components/Toasts'
import { TopBar } from './components/TopBar'
import { useMediaQuery } from './components/useMediaQuery'
import { CodeView } from './editor/CodeView'
import { ProblemsButton, ProblemsPanel } from './problems/ProblemsPanel'
import type { CoreClient } from './core/client'
import type { ConfigStore } from './core/configstore'
import { AppContext, useApp, useAppApi, useAppContext } from './store'
import { createAppStore } from './store/store'
import { VisualView } from './visual/VisualView'

export interface AppProps {
  client: CoreClient
  store: ConfigStore
  /** Content above the top bar (e.g. active repository, session). */
  header?: ReactNode
  /** Extra buttons in the top bar, before Settings. */
  actions?: ReactNode
}

/**
 * The complete application with the core and the configuration store
 * injected. It creates its state once per (client, store) pair; to change the
 * store, unmount it or give it another `key`.
 */
export function App({ client, store, header, actions }: AppProps) {
  const appStore = useMemo(() => createAppStore(client, store), [client, store])
  const value = useMemo(() => ({ client, configStore: store, store: appStore, header, actions }), [client, store, appStore, header, actions])
  return <AppContext.Provider value={value}><AppShell /></AppContext.Provider>
}

function AppShell() {
  const t = useT()
  const api = useAppApi()
  const { header } = useAppContext()
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

  useEffect(() => { void api.getState().init() }, [api])
  useEffect(() => { document.documentElement.lang = t.lang }, [t.lang])

  // Ctrl/Cmd+Z outside text fields (inside them, each one handles its own).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return
      const t = e.target as HTMLElement | null
      if (t?.closest('input,textarea,select,[contenteditable="true"],.cm-editor')) return
      const k = e.key.toLowerCase()
      if (k === 'z') { e.preventDefault(); if (e.shiftKey) api.getState().redo(); else api.getState().undo() }
      else if (k === 'y') { e.preventDefault(); api.getState().redo() }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [api])

  if (!ready) return <div id="app"><p className="muted pad">{t('app.loading')}</p></div>

  const assistantVisible = mobile && showAssistant
  return (
    <div id="app" className={mobile ? 'mobile' : ''}>
      {header}
      <TopBar />
      {error && <div className="iss sev-error net-error" role="alert"><span>{t('app.core')}</span> {error}</div>}
      <main className={`layout${canAssist && !mobile && chatOpen ? ' with-chat' : ''}`}>
        {!assistantVisible && (
          <section className="pane main-pane">
            <div className="pane-head">
              <div className="seg" role="tablist" aria-label={t('app.editMode')}>
                <button role="tab" aria-selected={mode === 'code'} className={mode === 'code' ? 'on' : ''} onClick={() => setMode('code')}>{t('app.code')}</button>
                <button role="tab" aria-selected={mode === 'visual'} className={mode === 'visual' ? 'on' : ''} onClick={() => setMode('visual')}>{t('app.visual')}</button>
              </div>
              <ProblemsButton open={problems} onToggle={() => setProblems(p => !p)} />
              {canAssist && !mobile && (
                <button className={`tbtn assist-toggle${chatOpen ? ' on' : ''}`} aria-pressed={chatOpen} data-testid="assistant-toggle" onClick={() => setChatOpen(!chatOpen)}>✦ {t('app.assistant')}</button>
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
          <section className="pane assistant-pane side" data-testid="assistant-pane" aria-label={t('app.assistant')}><ChatPanel /></section>
        )}
      </main>
      {mobile && (
        <nav className="mobile-tabs" role="tablist" aria-label={t('app.sections')}>
          <button role="tab" aria-selected={!showAssistant && mode === 'code'} className={!showAssistant && mode === 'code' ? 'on' : ''} onClick={() => setMode('code')}>‹/› {t('app.code')}</button>
          <button role="tab" aria-selected={!showAssistant && mode === 'visual'} className={!showAssistant && mode === 'visual' ? 'on' : ''} onClick={() => setMode('visual')}>◫ {t('app.visual')}</button>
          <button role="tab" aria-selected={showAssistant} className={showAssistant ? 'on' : ''} onClick={() => setShowAssistant(true)}>✦ {t('app.assistant')}</button>
        </nav>
      )}
      <DialogHost />
      <Toasts />
    </div>
  )
}
