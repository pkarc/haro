import { create } from 'zustand'
import { createJSONStorage, persist, type StateStorage } from 'zustand/middleware'
import type { CoreClient } from '../core/client'
import type { ConfigStore, SaveResult } from '../core/configstore'
import { makeT, setActiveLang, type T } from '../i18n'
import { browserLang, type Analysis, type Capabilities, type Knowledge, type Lang, type Op, type SourceFile } from '../core/types'
import { applyProposalToFiles, buildMessages, persistableChat, sanitizeChat, snapshotFor, type ChatMessage } from './chat'
import { applyChanges, dirtyIndexes, sameFiles } from './files'
import { sampleFiles } from './sample'

export const ANALYZE_DELAY_MS = 200
export const COALESCE_MS = 1200
const MAX_HISTORY = 200
const MAX_VERSIONS = 40

export interface Version { ts: number; label: string; files: SourceFile[] }
export interface Toast { id: number; msg: string; kind: 'info' | 'warn' | 'error'; action?: { label: string; run: () => void } }
export interface FocusRequest { file: number; line: number; nonce: number }
export type Mode = 'code' | 'visual'

export interface UiPrefs {
  /** Collapsed sections, by `file:type:name` key. */
  collapsed: Record<string, boolean>
  mapOpen: boolean
  /** Desktop: assistant panel open. */
  chatOpen: boolean
}

export interface AppState {
  files: SourceFile[]
  active: number
  /** Last thing read from disk (haro serve --file mode); null in a local session. */
  disk: SourceFile[] | null
  analysis: Analysis | null
  analysisError: string
  /** `rev` of the files `analysis` was computed with; if it differs from `rev`, it is stale. */
  analysisRev: number
  knowledge: Knowledge | null
  /** Language of the interface, of the explanations and of the detector messages. */
  lang: Lang
  capabilities: Capabilities | null
  ready: boolean
  mode: Mode
  /** Mobile only: «Assistant» tab. */
  showAssistant: boolean
  /** Conversation with the assistant (saved with the rest of the state). */
  chat: ChatMessage[]
  chatBusy: boolean
  /** The user already confirmed in this session that the configuration is sent to Anthropic (not saved). */
  chatAck: boolean
  undoStack: SourceFile[][]
  redoStack: SourceFile[][]
  versions: Version[]
  ui: UiPrefs
  focus: FocusRequest | null
  toasts: Toast[]
  /** Incremented on every change of files; used to discard stale responses. */
  rev: number

  init(): Promise<void>
  loadServed(served: SourceFile[] | null): void
  /** Reads again from the configuration store (after a version conflict). Saves a version of the local state first. */
  reload(): Promise<boolean>
  setActive(i: number): void
  setText(index: number, text: string, opts?: { coalesce?: boolean }): void
  applyOps(ops: Op[]): Promise<boolean>
  /** Applies the operations of a technology template, saving a version first. */
  applyTemplate(ops: Op[], label: string): Promise<boolean>
  undo(): void
  redo(): void
  setLang(l: Lang): Promise<void>
  setMode(m: Mode): void
  setShowAssistant(v: boolean): void
  setChatOpen(v: boolean): void
  ackChat(): void
  /** Sends a request to the assistant and renders the response as it arrives. */
  sendChat(text: string): Promise<void>
  /** Resends the last request after an error. */
  retryChat(): Promise<void>
  /** Cuts the response in progress (keeps what was received). */
  stopChat(): void
  newChat(): void
  /** Applies the proposal of message `mi` (saves a version first; a single undo step). */
  applyProposal(mi: number): boolean
  discardProposal(mi: number): void
  saveVersion(label: string): void
  restoreVersion(i: number): void
  deleteVersion(i: number): void
  importFiles(files: SourceFile[]): void
  loadSample(): void
  toggleCollapsed(key: string): void
  setMapOpen(v: boolean): void
  goto(file: number, line: number): void
  clearFocus(): void
  dirty(): number[]
  // Saves several files at once: the set is validated and everything or nothing is saved.
  save(indexes: number[]): Promise<SaveResult>
  markSaved(path: string, text: string): void
  toast(msg: string, kind?: Toast['kind'], action?: Toast['action']): void
  dismissToast(id: number): void
  refreshAnalysis(immediate?: boolean): void
}

export const DEFAULT_STORAGE_KEY = 'haro.v1'

export interface StoreOptions {
  storage?: StateStorage
  /** Defaults to the configuration store's (`ConfigStore.storageKey`) or `haro.v1`. */
  storageKey?: string
  /** For tests: without waiting for the real debounce. */
  analyzeDelay?: number
}

const memoryStorage = (): StateStorage => {
  const m = new Map<string, string>()
  return { getItem: k => m.get(k) ?? null, setItem: (k, v) => { m.set(k, v) }, removeItem: k => { m.delete(k) } }
}

function browserStorage(): StateStorage {
  try {
    if (typeof localStorage !== 'undefined') return localStorage
  } catch { /* storage blocked */ }
  return memoryStorage()
}

const normalize = (t: string) => t.replace(/\r\n?/g, '\n')

/**
 * Creates the application store. It receives the core client (Connect or
 * WASM, or a fake one in tests) and the configuration store (where the files
 * are loaded from and saved to).
 */
export function createAppStore(client: CoreClient, configStore: ConfigStore, opts: StoreOptions = {}) {
  let timer: ReturnType<typeof setTimeout> | undefined
  let requestId = 0
  let toastId = 0
  let focusNonce = 0
  // Last text edit, to merge consecutive keystrokes into a single undo step.
  let lastTyping: { at: number; file: number } | null = null
  let applyQueue: Promise<unknown> = Promise.resolve()
  // Conversation in progress: the epoch invalidates the writes of a response when another conversation starts.
  let chatEpoch = 0
  let chatCtl: AbortController | null = null
  const delay = opts.analyzeDelay ?? ANALYZE_DELAY_MS
  const initialLang = browserLang()

  const store = create<AppState>()(persist((set, get) => {
    // Translator for the current language (it changes when the user switches).
    const t = (): T => makeT(get().lang)
    // Changes the files recording a history step (unless `record: false`).
    const commit = (files: SourceFile[], o: { record?: boolean; coalesce?: boolean; file?: number } = {}) => {
      const s = get()
      if (files === s.files) return
      let undoStack = s.undoStack
      if (o.record !== false) {
        const now = Date.now()
        const merge = o.coalesce && lastTyping && lastTyping.file === o.file && now - lastTyping.at < COALESCE_MS && undoStack.length > 0
        if (!merge) undoStack = [...undoStack, s.files].slice(-MAX_HISTORY)
        lastTyping = o.coalesce ? { at: now, file: o.file ?? s.active } : null
        set({ files, undoStack, redoStack: [], rev: s.rev + 1 })
      } else {
        lastTyping = null
        set({ files, rev: s.rev + 1 })
      }
      get().refreshAnalysis()
    }

    const setProposalStatus = (chat: ChatMessage[], mi: number, status: 'applied' | 'discarded') =>
      chat.map((m, i) => i === mi && m.proposal ? { ...m, proposal: { ...m.proposal, status } } : m)

    // One assistant turn: sends the conversation with the current configuration
    // and completes the last message with what arrives through the stream.
    const runChat = async () => {
      const s = get()
      const messages = buildMessages(s.chat)
      if (!messages.length) return
      const files = s.files
      const epoch = chatEpoch
      const ctl = new AbortController()
      chatCtl = ctl
      const idx = s.chat.length
      set({ chat: [...s.chat, { role: 'assistant', content: '', ts: Date.now(), pending: true }], chatBusy: true })
      const patch = (fn: (m: ChatMessage) => ChatMessage) => {
        if (epoch !== chatEpoch) return
        set(st => ({ chat: st.chat.map((m, i) => i === idx ? fn(m) : m) }))
      }
      try {
        for await (const ev of client.assist({ files, messages, lang: get().lang }, ctl.signal)) {
          if (ctl.signal.aborted) break
          if (ev.type === 'text') patch(m => ({ ...m, content: m.content + ev.text }))
          else if (ev.type === 'proposal') {
            const p = ev.proposal
            patch(m => ({ ...m, proposal: { ...p, status: 'pending', base: snapshotFor(files, p.changes) } }))
          } else patch(m => ({ ...m, done: { stopReason: ev.done.stopReason, refusalCategory: ev.done.refusalCategory } }))
        }
        patch(m => ctl.signal.aborted ? { ...m, pending: false, stopped: true } : { ...m, pending: false })
      } catch (e) {
        if (ctl.signal.aborted) patch(m => ({ ...m, pending: false, stopped: true }))
        else patch(m => ({ ...m, pending: false, error: (e as Error).message || t()('state.unknownError') }))
      } finally {
        if (epoch === chatEpoch) { set({ chatBusy: false }); chatCtl = null }
      }
    }

    return {
      files: sampleFiles(browserLang()),
      active: 0,
      disk: null,
      analysis: null,
      analysisError: '',
      analysisRev: -1,
      knowledge: null,
      lang: initialLang,
      capabilities: null,
      ready: false,
      mode: 'visual',
      showAssistant: false,
      chat: [],
      chatBusy: false,
      chatAck: false,
      undoStack: [],
      redoStack: [],
      versions: [],
      ui: { collapsed: {}, mapOpen: true, chatOpen: false },
      focus: null,
      toasts: [],
      rev: 0,

      async init() {
        const [caps, kn, served] = await Promise.all([
          client.capabilities().catch(() => null),
          client.knowledge(get().lang).catch(() => null),
          configStore.load().catch((e: unknown) => {
            get().toast(t()('state.loadFailed', { label: configStore.label, msg: (e as Error).message }), 'error')
            return null
          }),
        ])
        set({ capabilities: caps, knowledge: kn })
        get().loadServed(served && served.length ? served : null)
        set({ ready: true })
        get().refreshAnalysis(true)
      },

      // If haro serve serves files, they replace the local session, unless the
      // saved session already started from exactly that disk content (page
      // reload with unsaved edits).
      loadServed(served) {
        const s = get()
        if (!served) {
          set({ disk: null })
          return
        }
        if (s.disk && sameFiles(s.disk, served) && s.files.length === served.length && s.files.every((f, i) => f.path === served[i].path)) {
          return
        }
        set({ files: served, disk: served, active: 0, undoStack: [], redoStack: [], rev: s.rev + 1 })
      },

      async reload() {
        let files: SourceFile[] | null
        try { files = await configStore.load() } catch (e) {
          get().toast(t()('state.reloadFailed', { label: configStore.label, msg: (e as Error).message }), 'error')
          return false
        }
        if (!files?.length) return false
        get().saveVersion(t()('state.beforeReload'))
        set(s => ({ files: files!, disk: files, active: 0, undoStack: [], redoStack: [], rev: s.rev + 1 }))
        get().refreshAnalysis()
        return true
      },

      setActive(i) { if (i >= 0 && i < get().files.length) set({ active: i }) },

      setText(index, text, o = {}) {
        const s = get()
        const cur = s.files[index]
        if (!cur || cur.text === text) return
        const files = s.files.slice()
        files[index] = { ...cur, text }
        commit(files, { coalesce: o.coalesce, file: index })
      },

      // All visual-mode edits go through the core (apply). They are queued so
      // each one sees the result of the previous one.
      applyOps(ops) {
        const run = async (): Promise<boolean> => {
          const { files, rev } = get()
          try {
            const res = await client.apply(files, ops)
            if (get().rev !== rev) {
              get().toast(t()('state.changedWhileApplying'), 'warn')
              return false
            }
            if (res.changes.length) commit(applyChanges(files, res.changes, get().lang))
            return true
          } catch (e) {
            get().toast(t()('state.applyFailed', { msg: (e as Error).message }), 'warn')
            return false
          }
        }
        const p = applyQueue.then(run, run)
        applyQueue = p
        return p
      },

      // A technology template is a big change: a version is saved first and the
      // user is notified with an undo option.
      async applyTemplate(ops, label) {
        get().saveVersion(t()('state.beforeTemplate', { label }))
        const ok = await get().applyOps(ops)
        if (ok) get().toast(t()('state.templateAdded', { label }), 'info', { label: t()('common.undo'), run: () => get().undo() })
        return ok
      },

      undo() {
        const s = get()
        if (!s.undoStack.length) return
        const prev = s.undoStack[s.undoStack.length - 1]
        set({ undoStack: s.undoStack.slice(0, -1), redoStack: [...s.redoStack, s.files] })
        commit(prev, { record: false })
        set(st => ({ active: Math.min(st.active, st.files.length - 1) }))
      },

      redo() {
        const s = get()
        if (!s.redoStack.length) return
        const next = s.redoStack[s.redoStack.length - 1]
        set({ redoStack: s.redoStack.slice(0, -1), undoStack: [...s.undoStack, s.files] })
        commit(next, { record: false })
        set(st => ({ active: Math.min(st.active, st.files.length - 1) }))
      },

      // Changes the language of the interface and of the core texts: reloads the
      // knowledge and analyzes again. If the knowledge does not arrive, the
      // previous one is kept.
      async setLang(l) {
        if (get().lang === l) return
        set({ lang: l })
        get().refreshAnalysis(true)
        const kn = await client.knowledge(l).catch(() => null)
        if (kn && get().lang === l) set({ knowledge: kn })
      },

      setMode(m) { set({ mode: m, showAssistant: false }) },
      setShowAssistant(v) { set({ showAssistant: v }) },
      setChatOpen(v) { set(s => ({ ui: { ...s.ui, chatOpen: v } })) },
      ackChat() { set({ chatAck: true }) },

      async sendChat(text) {
        const t = text.trim()
        if (!t || get().chatBusy) return
        set(s => ({ chat: [...s.chat, { role: 'user', content: t, ts: Date.now() }] }))
        await runChat()
      },

      async retryChat() {
        const s = get()
        if (s.chatBusy) return
        const last = s.chat[s.chat.length - 1]
        if (last?.role === 'assistant' && last.error) set({ chat: s.chat.slice(0, -1) })
        await runChat()
      },

      stopChat() { chatCtl?.abort() },

      newChat() {
        chatCtl?.abort()
        chatEpoch++
        chatCtl = null
        set({ chat: [], chatBusy: false })
      },

      applyProposal(mi) {
        const m = get().chat[mi]
        if (!m?.proposal || m.proposal.status !== 'pending') return false
        let files: SourceFile[]
        try { files = applyProposalToFiles(get().files, m.proposal.changes, get().lang) } catch (e) {
          get().toast((e as Error).message, 'warn')
          return false
        }
        get().saveVersion(t()('state.beforeProposal'))
        commit(files)
        set(st => ({ chat: setProposalStatus(st.chat, mi, 'applied') }))
        get().toast(t()('state.proposalApplied'), 'info', { label: t()('common.undo'), run: () => get().undo() })
        return true
      },

      discardProposal(mi) { set(st => ({ chat: setProposalStatus(st.chat, mi, 'discarded') })) },

      saveVersion(label) {
        const s = get()
        if (s.versions[0] && sameFiles(s.versions[0].files, s.files)) return
        set({ versions: [{ ts: Date.now(), label, files: s.files }, ...s.versions].slice(0, MAX_VERSIONS) })
      },

      restoreVersion(i) {
        const v = get().versions[i]
        if (!v) return
        get().saveVersion(t()('state.beforeRestore'))
        commit(v.files)
        set(st => ({ active: Math.min(st.active, st.files.length - 1) }))
        get().toast(t()('state.restored', { label: v.label }), 'info', { label: t()('common.undo'), run: () => get().undo() })
      },

      deleteVersion(i) { set(s => ({ versions: s.versions.filter((_, k) => k !== i) })) },

      // With files served from disk only the content of the active file can be
      // replaced (the paths are set by the server).
      importFiles(incoming) {
        if (!incoming.length) return
        const s = get()
        get().saveVersion(t()('state.beforeImport'))
        let files: SourceFile[]
        if (s.disk) {
          files = s.files.slice()
          files[s.active] = { ...files[s.active], text: normalize(incoming[0].text) }
        } else {
          files = incoming.map(f => ({ path: f.path, text: normalize(f.text) }))
        }
        commit(files)
        set(st => ({ active: Math.min(st.active, st.files.length - 1) }))
        get().toast(t()('state.imported'), 'info', { label: t()('common.undo'), run: () => get().undo() })
      },

      loadSample() {
        get().saveVersion(t()('state.beforeSample'))
        set({ disk: null })
        commit(sampleFiles(get().lang))
        set({ active: 0 })
      },

      toggleCollapsed(key) { set(s => ({ ui: { ...s.ui, collapsed: { ...s.ui.collapsed, [key]: !s.ui.collapsed[key] } } })) },
      setMapOpen(v) { set(s => ({ ui: { ...s.ui, mapOpen: v } })) },

      goto(file, line) { set({ active: file, focus: { file, line, nonce: ++focusNonce } }) },

      clearFocus() { set({ focus: null }) },

      dirty() { const s = get(); return dirtyIndexes(s.files, s.disk) },

      async save(indexes) {
        const files = indexes.map(i => get().files[i])
        const res = await configStore.save(files)
        if (res.saved) files.forEach(f => get().markSaved(f.path, f.text))
        return res
      },

      markSaved(path, text) {
        set(s => s.disk ? { disk: s.disk.map(f => f.path === path ? { path, text } : f) } : {})
      },

      toast(msg, kind = 'info', action) {
        set(s => ({ toasts: [...s.toasts, { id: ++toastId, msg, kind, action }] }))
      },
      dismissToast(id) { set(s => ({ toasts: s.toasts.filter(t => t.id !== id) })) },

      // Debounced analysis; responses of superseded requests are discarded.
      refreshAnalysis(immediate = false) {
        clearTimeout(timer)
        const run = () => {
          const id = ++requestId
          const { files, rev } = get()
          client.analyze(files, get().lang).then(
            a => { if (id === requestId) set({ analysis: a, analysisError: '', analysisRev: rev }) },
            e => { if (id === requestId) set({ analysisError: (e as Error).message }) },
          )
        }
        if (immediate) run()
        else timer = setTimeout(run, delay)
      },
    }
  }, {
    name: opts.storageKey ?? configStore.storageKey ?? DEFAULT_STORAGE_KEY,
    version: 1,
    storage: createJSONStorage(() => opts.storage ?? browserStorage()),
    partialize: s => ({ lang: s.lang, chat: persistableChat(s.chat, s.lang).slice(-100), files: s.files, active: s.active, disk: s.disk, mode: s.mode, versions: s.versions, ui: s.ui }),
    // Validate what was saved: a corrupt localStorage must not break the app.
    merge: (persisted, base) => {
      let current = base
      const p = persisted as Partial<AppState> | undefined
      if (p && Array.isArray(p.chat)) current = { ...current, chat: sanitizeChat(p.chat) }
      if (!p || !Array.isArray(p.files) || !p.files.length || !p.files.every(f => typeof f?.path === 'string' && typeof f?.text === 'string')) return current
      return {
        ...current,
        files: p.files,
        active: Math.min(Math.max(p.active ?? 0, 0), p.files.length - 1),
        disk: Array.isArray(p.disk) ? p.disk : null,
        lang: p.lang === 'en' || p.lang === 'es' ? p.lang : current.lang,
        mode: p.mode === 'code' ? 'code' : 'visual',
        versions: Array.isArray(p.versions) ? p.versions : [],
        ui: { ...current.ui, ...(p.ui ?? {}) },
      }
    },
  }))
  // Code outside React (core client, configuration stores) translates with the active language.
  setActiveLang(store.getState().lang)
  store.subscribe(st => setActiveLang(st.lang))
  return store
}
