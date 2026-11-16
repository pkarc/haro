import { create } from 'zustand'
import { createJSONStorage, persist, type StateStorage } from 'zustand/middleware'
import type { CoreClient } from '../core/client'
import type { ConfigStore, SaveResult } from '../core/configstore'
import { browserLang, type Analysis, type Capabilities, type Knowledge, type Lang, type Op, type SourceFile } from '../core/types'
import { applyProposalToFiles, buildMessages, persistableChat, sanitizeChat, snapshotFor, type ChatMessage } from './chat'
import { applyChanges, dirtyIndexes, sameFiles } from './files'
import { SAMPLE_FILES } from './sample'

export const ANALYZE_DELAY_MS = 200
export const COALESCE_MS = 1200
const MAX_HISTORY = 200
const MAX_VERSIONS = 40

export interface Version { ts: number; label: string; files: SourceFile[] }
export interface Toast { id: number; msg: string; kind: 'info' | 'warn' | 'error'; action?: { label: string; run: () => void } }
export interface FocusRequest { file: number; line: number; nonce: number }
export type Mode = 'code' | 'visual'

export interface UiPrefs {
  /** Secciones plegadas, por clave `archivo:tipo:nombre`. */
  collapsed: Record<string, boolean>
  mapOpen: boolean
  /** Escritorio: panel del asistente abierto. */
  chatOpen: boolean
}

export interface AppState {
  files: SourceFile[]
  active: number
  /** Lo último leído de disco (modo haro serve --file); null en sesión local. */
  disk: SourceFile[] | null
  analysis: Analysis | null
  analysisError: string
  /** `rev` de los archivos con los que se calculó `analysis`; si no coincide con `rev`, está atrasado. */
  analysisRev: number
  knowledge: Knowledge | null
  /** Idioma de las explicaciones y de los mensajes del detector (la interfaz sigue en español). */
  lang: Lang
  capabilities: Capabilities | null
  ready: boolean
  mode: Mode
  /** Solo móvil: pestaña «Asistente». */
  showAssistant: boolean
  /** Conversación con el asistente (se guarda con el resto del estado). */
  chat: ChatMessage[]
  chatBusy: boolean
  /** El usuario ya confirmó en esta sesión que la configuración se envía a Anthropic (no se guarda). */
  chatAck: boolean
  undoStack: SourceFile[][]
  redoStack: SourceFile[][]
  versions: Version[]
  ui: UiPrefs
  focus: FocusRequest | null
  toasts: Toast[]
  /** Se incrementa con cada cambio de archivos; sirve para descartar respuestas viejas. */
  rev: number

  init(): Promise<void>
  loadServed(served: SourceFile[] | null): void
  /** Vuelve a leer del almacén de configuración (tras un conflicto de versión). Guarda antes una versión con lo local. */
  reload(): Promise<boolean>
  setActive(i: number): void
  setText(index: number, text: string, opts?: { coalesce?: boolean }): void
  applyOps(ops: Op[]): Promise<boolean>
  /** Aplica las operaciones de una plantilla por tecnología guardando antes una versión. */
  applyTemplate(ops: Op[], label: string): Promise<boolean>
  undo(): void
  redo(): void
  setLang(l: Lang): Promise<void>
  setMode(m: Mode): void
  setShowAssistant(v: boolean): void
  setChatOpen(v: boolean): void
  ackChat(): void
  /** Envía una petición al asistente y va pintando la respuesta. */
  sendChat(text: string): Promise<void>
  /** Reenvía la última petición tras un error. */
  retryChat(): Promise<void>
  /** Corta la respuesta en curso (conserva lo recibido). */
  stopChat(): void
  newChat(): void
  /** Aplica la propuesta del mensaje `mi` (guarda antes una versión; un solo paso de deshacer). */
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
  // Guarda varios archivos a la vez: se valida el conjunto y se guarda todo o nada.
  save(indexes: number[]): Promise<SaveResult>
  markSaved(path: string, text: string): void
  toast(msg: string, kind?: Toast['kind'], action?: Toast['action']): void
  dismissToast(id: number): void
  refreshAnalysis(immediate?: boolean): void
}

export const DEFAULT_STORAGE_KEY = 'haro.v1'

export interface StoreOptions {
  storage?: StateStorage
  /** Por defecto, la del almacén de configuración (`ConfigStore.storageKey`) o `haro.v1`. */
  storageKey?: string
  /** Para pruebas: sin esperar el debounce real. */
  analyzeDelay?: number
}

const memoryStorage = (): StateStorage => {
  const m = new Map<string, string>()
  return { getItem: k => m.get(k) ?? null, setItem: (k, v) => { m.set(k, v) }, removeItem: k => { m.delete(k) } }
}

function browserStorage(): StateStorage {
  try {
    if (typeof localStorage !== 'undefined') return localStorage
  } catch { /* almacenamiento bloqueado */ }
  return memoryStorage()
}

const normalize = (t: string) => t.replace(/\r\n?/g, '\n')

/**
 * Crea el almacén de la aplicación. Recibe el cliente del núcleo (Connect o
 * WASM, o uno falso en pruebas) y el almacén de configuración (de dónde
 * se cargan y adónde se guardan los archivos).
 */
export function createAppStore(client: CoreClient, configStore: ConfigStore, opts: StoreOptions = {}) {
  let timer: ReturnType<typeof setTimeout> | undefined
  let requestId = 0
  let toastId = 0
  let focusNonce = 0
  // Última edición de texto, para fusionar pulsaciones seguidas en un solo paso de deshacer.
  let lastTyping: { at: number; file: number } | null = null
  let applyQueue: Promise<unknown> = Promise.resolve()
  // Conversación en curso: la época invalida las escrituras de una respuesta cuando se empieza otra conversación.
  let chatEpoch = 0
  let chatCtl: AbortController | null = null
  const delay = opts.analyzeDelay ?? ANALYZE_DELAY_MS

  return create<AppState>()(persist((set, get) => {
    // Cambia los archivos registrando un paso de historial (salvo `record: false`).
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

    // Un turno del asistente: envía la conversación con la configuración actual
    // y va completando el último mensaje con lo que llega por el stream.
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
        else patch(m => ({ ...m, pending: false, error: (e as Error).message || 'Error desconocido' }))
      } finally {
        if (epoch === chatEpoch) { set({ chatBusy: false }); chatCtl = null }
      }
    }

    return {
      files: SAMPLE_FILES,
      active: 0,
      disk: null,
      analysis: null,
      analysisError: '',
      analysisRev: -1,
      knowledge: null,
      lang: browserLang(),
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
            get().toast(`No se pudo cargar desde ${configStore.label}: ${(e as Error).message}`, 'error')
            return null
          }),
        ])
        set({ capabilities: caps, knowledge: kn })
        get().loadServed(served && served.length ? served : null)
        set({ ready: true })
        get().refreshAnalysis(true)
      },

      // Si haro serve sirve archivos, sustituyen a la sesión local; salvo que
      // la sesión guardada ya partiera exactamente de ese contenido de disco
      // (recarga de la página con ediciones sin guardar).
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
          get().toast(`No se pudo recargar desde ${configStore.label}: ${(e as Error).message}`, 'error')
          return false
        }
        if (!files?.length) return false
        get().saveVersion('Antes de recargar')
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

      // Todas las ediciones del modo visual pasan por el núcleo (apply). Se
      // encolan para que cada una vea el resultado de la anterior.
      applyOps(ops) {
        const run = async (): Promise<boolean> => {
          const { files, rev } = get()
          try {
            const res = await client.apply(files, ops)
            if (get().rev !== rev) {
              get().toast('Los archivos cambiaron mientras se aplicaba la edición; inténtalo de nuevo.', 'warn')
              return false
            }
            if (res.changes.length) commit(applyChanges(files, res.changes))
            return true
          } catch (e) {
            get().toast(`No se pudo aplicar el cambio: ${(e as Error).message}`, 'warn')
            return false
          }
        }
        const p = applyQueue.then(run, run)
        applyQueue = p
        return p
      },

      // Una plantilla por tecnología es un cambio grande: se guarda una versión
      // antes y se avisa con la opción de deshacer.
      async applyTemplate(ops, label) {
        get().saveVersion(`Antes de añadir ${label}`)
        const ok = await get().applyOps(ops)
        if (ok) get().toast(`Plantilla «${label}» añadida`, 'info', { label: 'Deshacer', run: () => get().undo() })
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

      // Cambia el idioma de los textos del núcleo: recarga el conocimiento y
      // vuelve a analizar. Si el conocimiento no llega, se conserva el anterior.
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
        try { files = applyProposalToFiles(get().files, m.proposal.changes) } catch (e) {
          get().toast((e as Error).message, 'warn')
          return false
        }
        get().saveVersion('Antes de aplicar propuesta del asistente')
        commit(files)
        set(st => ({ chat: setProposalStatus(st.chat, mi, 'applied') }))
        get().toast('Propuesta del asistente aplicada', 'info', { label: 'Deshacer', run: () => get().undo() })
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
        get().saveVersion('Antes de restaurar')
        commit(v.files)
        set(st => ({ active: Math.min(st.active, st.files.length - 1) }))
        get().toast(`Restaurada «${v.label}»`, 'info', { label: 'Deshacer', run: () => get().undo() })
      },

      deleteVersion(i) { set(s => ({ versions: s.versions.filter((_, k) => k !== i) })) },

      // Con archivos servidos desde disco solo se puede reemplazar el contenido
      // del archivo activo (las rutas las fija el servidor).
      importFiles(incoming) {
        if (!incoming.length) return
        const s = get()
        get().saveVersion('Antes de importar')
        let files: SourceFile[]
        if (s.disk) {
          files = s.files.slice()
          files[s.active] = { ...files[s.active], text: normalize(incoming[0].text) }
        } else {
          files = incoming.map(f => ({ path: f.path, text: normalize(f.text) }))
        }
        commit(files)
        set(st => ({ active: Math.min(st.active, st.files.length - 1) }))
        get().toast('Configuración importada', 'info', { label: 'Deshacer', run: () => get().undo() })
      },

      loadSample() {
        get().saveVersion('Antes de cargar el ejemplo')
        set({ disk: null })
        commit(SAMPLE_FILES)
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

      // Análisis con debounce; las respuestas de peticiones superadas se descartan.
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
    partialize: s => ({ lang: s.lang, chat: persistableChat(s.chat).slice(-100), files: s.files, active: s.active, disk: s.disk, mode: s.mode, versions: s.versions, ui: s.ui }),
    // Validar lo guardado: un localStorage corrupto no debe romper la app.
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
}
