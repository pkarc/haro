import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CoreClient } from '../core/client'
import { ServedStore, type ConfigStore, type SaveResult } from '../core/configstore'
import type { Analysis, SourceFile } from '../core/types'
import { proposalStale } from './chat'
import { applyChanges, dirtyIndexes } from './files'
import { createAppStore, COALESCE_MS } from './store'

const emptyAnalysis = (tag: string): Analysis => ({
  doc: { pre: [], sections: [], files: [tag] }, issues: [], explanations: [], effective: [],
  topology: { entries: [], backends: [], edges: [], missing: [] },
})

function fakeClient(over: Partial<CoreClient> = {}): CoreClient {
  return {
    capabilities: async () => ({ version: 't', canRead: false, canWrite: false, canCheck: false, paths: [], haproxyBin: '', canAssist: false, assistantModel: '' }),
    analyze: async f => emptyAnalysis(f[0].text),
    apply: async files => ({ files, changes: [] }),
    diff: async () => [],
    check: async () => ({ available: false, ok: false, output: '' }),
    getConfig: async () => null,
    putFile: async () => ({ saved: true }),
    putFiles: async () => ({ saved: true }),
    knowledge: async () => ({ categories: {}, sections: {}, directives: {}, rules: {}, templates: {} }),
    templates: async () => [],
    renderTemplate: async files => ({ ops: [], files, changes: [] }),
    assist: async function* () { /* sin asistente */ },
    ...over,
  }
}
const mk = (client = fakeClient(), configStore: ConfigStore = new ServedStore(client)) => {
  const mem = new Map<string, string>()
  return createAppStore(client, configStore, { storage: { getItem: k => mem.get(k) ?? null, setItem: (k, v) => { mem.set(k, v) }, removeItem: k => { mem.delete(k) } } })
}
const F = (text: string, path = 'a.cfg'): SourceFile => ({ path, text })

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(0) })
afterEach(() => { vi.useRealTimers() })

describe('deshacer y rehacer', () => {
  it('deshace y rehace ediciones', () => {
    const s = mk()
    s.setState({ files: [F('uno')] })
    s.getState().setText(0, 'dos')
    s.getState().setText(0, 'tres')
    s.getState().undo()
    expect(s.getState().files[0].text).toBe('dos')
    s.getState().undo()
    expect(s.getState().files[0].text).toBe('uno')
    s.getState().redo()
    expect(s.getState().files[0].text).toBe('dos')
    s.getState().setText(0, 'nuevo')
    expect(s.getState().redoStack).toHaveLength(0)
  })

  it('fusiona pulsaciones seguidas y separa tras una pausa', () => {
    const s = mk()
    s.setState({ files: [F('')] })
    for (const t of ['a', 'ab', 'abc']) { s.getState().setText(0, t, { coalesce: true }); vi.advanceTimersByTime(100) }
    expect(s.getState().undoStack).toHaveLength(1)
    vi.advanceTimersByTime(COALESCE_MS + 1)
    s.getState().setText(0, 'abcd', { coalesce: true })
    expect(s.getState().undoStack).toHaveLength(2)
    s.getState().undo()
    expect(s.getState().files[0].text).toBe('abc')
  })
})

describe('apply', () => {
  it('aplica los changes a los archivos', async () => {
    const client = fakeClient({ apply: async files => ({ files, changes: [{ file: 1, path: 'b.cfg', text: 'B2' }] }) })
    const s = mk(client)
    s.setState({ files: [F('A'), F('B', 'b.cfg')] })
    expect(await s.getState().applyOps([{ op: 'remove_section', section: 0 }])).toBe(true)
    expect(s.getState().files.map(f => f.text)).toEqual(['A', 'B2'])
    expect(s.getState().undoStack).toHaveLength(1)
  })

  it('muestra un aviso si apply falla y no cambia nada', async () => {
    const s = mk(fakeClient({ apply: async () => { throw new Error('operación 1: no existe') } }))
    s.setState({ files: [F('A')] })
    expect(await s.getState().applyOps([{ op: 'remove_section', section: 9 }])).toBe(false)
    expect(s.getState().files[0].text).toBe('A')
    expect(s.getState().toasts[0].msg).toContain('no existe')
  })

  it('applyChanges rechaza rutas que no encajan', () => {
    expect(() => applyChanges([F('x')], [{ file: 0, path: 'otro.cfg', text: 'y' }])).toThrow()
  })
})

describe('plantillas por tecnología', () => {
  const ops = [{ op: 'add_section' as const, type: 'backend', name: 'nextcloud', at: 1 }]

  it('guarda una versión antes de aplicar, cambia los archivos y avisa con deshacer', async () => {
    const apply = vi.fn(async (files: SourceFile[]) => ({ files, changes: [{ file: 0, path: 'a.cfg', text: 'A\nbackend nextcloud' }] }))
    const s = mk(fakeClient({ apply }))
    s.setState({ files: [F('A')] })
    expect(await s.getState().applyTemplate(ops, 'Nextcloud')).toBe(true)
    expect(apply).toHaveBeenCalledWith([F('A')], ops)
    expect(s.getState().files[0].text).toBe('A\nbackend nextcloud')
    expect(s.getState().versions[0]).toMatchObject({ label: 'Antes de añadir Nextcloud', files: [F('A')] })
    const toast = s.getState().toasts.at(-1)!
    expect(toast.msg).toContain('Nextcloud')
    toast.action!.run()
    expect(s.getState().files[0].text).toBe('A')
  })

  it('si apply falla no cambia nada ni avisa de éxito', async () => {
    const s = mk(fakeClient({ apply: async () => { throw new Error('operación 1: no existe') } }))
    s.setState({ files: [F('A')] })
    expect(await s.getState().applyTemplate(ops, 'Nextcloud')).toBe(false)
    expect(s.getState().files[0].text).toBe('A')
    expect(s.getState().toasts.some(t => t.msg.includes('añadida'))).toBe(false)
  })
})

describe('análisis', () => {
  it('descarta respuestas obsoletas', async () => {
    const resolvers: Record<string, (a: Analysis) => void> = {}
    const s = mk(fakeClient({ analyze: f => new Promise(r => { resolvers[f[0].text] = r }) }))
    s.setState({ files: [F('v1')] })
    s.getState().refreshAnalysis(true)
    s.getState().setText(0, 'v2')
    await vi.advanceTimersByTimeAsync(250) // el debounce lanza la segunda petición
    resolvers.v2(emptyAnalysis('v2'))
    await vi.advanceTimersByTimeAsync(0)
    resolvers.v1(emptyAnalysis('v1')) // llega tarde
    await vi.advanceTimersByTimeAsync(0)
    expect(s.getState().analysis?.doc.files?.[0]).toBe('v2')
    expect(s.getState().analysisRev).toBe(s.getState().rev)
  })

  it('agrupa varias ediciones en un solo análisis (debounce)', async () => {
    const analyze = vi.fn(async (f: SourceFile[]) => emptyAnalysis(f[0].text))
    const s = mk(fakeClient({ analyze }))
    s.setState({ files: [F('')] })
    for (const t of ['a', 'ab', 'abc']) s.getState().setText(0, t)
    await vi.advanceTimersByTimeAsync(250)
    expect(analyze).toHaveBeenCalledTimes(1)
  })
})

describe('archivos de disco', () => {
  it('los archivos servidos sustituyen a la sesión local y se marcan sucios al editar', () => {
    const s = mk()
    s.getState().loadServed([F('X', 'x.cfg')])
    expect(s.getState().files[0].text).toBe('X')
    s.getState().setText(0, 'Y')
    expect(s.getState().dirty()).toEqual([0])
    s.getState().markSaved('x.cfg', 'Y')
    expect(s.getState().dirty()).toEqual([])
  })

  it('conserva ediciones sin guardar si el disco no cambió', () => {
    const s = mk()
    s.getState().loadServed([F('X', 'x.cfg')])
    s.getState().setText(0, 'editado')
    s.getState().loadServed([F('X', 'x.cfg')])
    expect(s.getState().files[0].text).toBe('editado')
    s.getState().loadServed([F('X2', 'x.cfg')])
    expect(s.getState().files[0].text).toBe('X2')
  })

  it('dirtyIndexes ignora archivos que no están en disco', () => {
    expect(dirtyIndexes([F('a'), F('b', 'n.cfg')], [F('z')])).toEqual([0])
  })
})

describe('idioma', () => {
  const kn = (tag: string) => ({ categories: { net: tag }, sections: {}, directives: {}, rules: {}, templates: {} })

  it('usa el idioma del navegador si es inglés y español si no', async () => {
    const orig = Object.getOwnPropertyDescriptor(globalThis, 'navigator')
    const setNav = (language: string) => Object.defineProperty(globalThis, 'navigator', { value: { language }, configurable: true })
    try {
      setNav('en-US')
      expect(mk().getState().lang).toBe('en')
      setNav('es-MX')
      expect(mk().getState().lang).toBe('es')
      setNav('fr-FR')
      expect(mk().getState().lang).toBe('es')
    } finally {
      if (orig) Object.defineProperty(globalThis, 'navigator', orig)
    }
  })

  it('pasa el idioma a analyze y knowledge, y al cambiarlo reanaliza y recarga el conocimiento', async () => {
    const analyze = vi.fn(async (f: SourceFile[], _lang?: string) => emptyAnalysis(f[0].text))
    const knowledge = vi.fn(async (lang?: string) => kn(lang ?? '?'))
    const s = mk(fakeClient({ analyze, knowledge }))
    s.setState({ files: [F('x')], lang: 'es' })
    await s.getState().init()
    expect(analyze).toHaveBeenLastCalledWith([F('x')], 'es')
    expect(knowledge).toHaveBeenLastCalledWith('es')
    await s.getState().setLang('en')
    expect(s.getState().lang).toBe('en')
    expect(analyze).toHaveBeenLastCalledWith([F('x')], 'en')
    expect(knowledge).toHaveBeenLastCalledWith('en')
    expect(s.getState().knowledge?.categories.net).toBe('en')
    const n = analyze.mock.calls.length
    await s.getState().setLang('en')
    expect(analyze.mock.calls.length).toBe(n) // sin cambio no hay llamadas
  })

  it('persiste el idioma y descarta valores inválidos', () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } }
    const a = createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage })
    a.setState({ lang: 'en' })
    a.getState().setText(0, 'cambio') // fuerza la escritura en el almacenamiento
    const saved = JSON.parse(mem.get('haro.v1')!)
    expect(saved.state.lang).toBe('en')
    expect(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().lang).toBe('en')
    saved.state.lang = 'klingon'
    mem.set('haro.v1', JSON.stringify(saved))
    expect(['es', 'en']).toContain(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().lang)
  })
})

describe('asistente', () => {
  type Ev = import('../core/types').AssistEvent
  const done: Ev = { type: 'done', done: { stopReason: 'end_turn', refusalCategory: '', inputTokens: 1, outputTokens: 1 } }
  const proposal = (path: string, text: string): Ev => ({ type: 'proposal', proposal: { changes: [{ file: 0, path, text }], issuesAfter: [], unmatched: 0, truncated: false } })
  // cliente con un asistente guionizado; recuerda las peticiones
  const assistant = (script: (req: import('../core/types').AssistRequest, signal?: AbortSignal) => AsyncGenerator<Ev>) => {
    const calls: import('../core/types').AssistRequest[] = []
    return { calls, client: fakeClient({ assist: (req, signal) => { calls.push(req); return script(req, signal) } }) }
  }

  it('recibe la respuesta en streaming y la guarda con el idioma y los archivos', async () => {
    const { client, calls } = assistant(async function* () { yield { type: 'text', text: 'Ho' }; yield { type: 'text', text: 'la' }; yield done })
    const s = mk(client)
    s.setState({ files: [F('uno')], lang: 'en' })
    await s.getState().sendChat('  pregunta  ')
    expect(calls[0]).toEqual({ files: [F('uno')], messages: [{ role: 'user', content: 'pregunta' }], lang: 'en' })
    const chat = s.getState().chat
    expect(chat.map(m => [m.role, m.content])).toEqual([['user', 'pregunta'], ['assistant', 'Hola']])
    expect(chat[1].pending).toBe(false)
    expect(s.getState().chatBusy).toBe(false)
  })

  it('muestra el texto parcial mientras llega y bloquea un segundo envío', async () => {
    let release!: () => void
    const gate = new Promise<void>(r => { release = r })
    const { client, calls } = assistant(async function* () { yield { type: 'text', text: 'par' }; await gate; yield { type: 'text', text: 'cial' }; yield done })
    const s = mk(client)
    const p = s.getState().sendChat('hola')
    await vi.waitFor(() => expect(s.getState().chat[1]?.content).toBe('par'))
    expect(s.getState().chatBusy).toBe(true)
    expect(s.getState().chat[1].pending).toBe(true)
    await s.getState().sendChat('otra') // ignorada
    expect(calls).toHaveLength(1)
    release()
    await p
    expect(s.getState().chat[1].content).toBe('parcial')
  })

  it('detener corta el stream y conserva lo recibido', async () => {
    const { client } = assistant(async function* (_r, signal) {
      yield { type: 'text', text: 'medio' }
      await new Promise((_, rej) => signal?.addEventListener('abort', () => rej(new Error('abortado'))))
    })
    const s = mk(client)
    const p = s.getState().sendChat('hola')
    await vi.waitFor(() => expect(s.getState().chat[1]?.content).toBe('medio'))
    s.getState().stopChat()
    await p
    const m = s.getState().chat[1]
    expect(m).toMatchObject({ content: 'medio', stopped: true, pending: false })
    expect(m.error).toBeUndefined()
    expect(s.getState().chatBusy).toBe(false)
  })

  it('un error queda en el mensaje y se puede reintentar sin repetir la petición del usuario', async () => {
    let n = 0
    const { client, calls } = assistant(async function* () {
      if (++n === 1) throw new Error('la API de Anthropic rechazó las credenciales')
      yield { type: 'text', text: 'ahora sí' }
      yield done
    })
    const s = mk(client)
    await s.getState().sendChat('hola')
    expect(s.getState().chat[1].error).toContain('credenciales')
    await s.getState().retryChat()
    expect(s.getState().chat.map(m => m.content)).toEqual(['hola', 'ahora sí'])
    expect(calls[1].messages).toEqual([{ role: 'user', content: 'hola' }])
  })

  it('el historial enviado alterna roles y omite los mensajes fallidos', async () => {
    let n = 0
    const { client, calls } = assistant(async function* () {
      if (++n === 2) throw new Error('límite')
      yield { type: 'text', text: `r${n}` }
      yield done
    })
    const s = mk(client)
    await s.getState().sendChat('a')
    await s.getState().sendChat('b') // falla
    await s.getState().sendChat('c')
    expect(calls[2].messages).toEqual([{ role: 'user', content: 'a' }, { role: 'assistant', content: 'r1' }, { role: 'user', content: 'b\n\nc' }])
  })

  it('el historial se guarda, sobrevive a la recarga y una respuesta inconclusa queda como interrumpida', async () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } }
    const { client } = assistant(async function* () { yield { type: 'text', text: 'resp' }; yield done })
    const a = createAppStore(client, new ServedStore(client), { storage })
    await a.getState().sendChat('hola')
    a.setState({ chat: [...a.getState().chat, { role: 'user', content: 'otra', ts: 1 }, { role: 'assistant', content: 'a medias', ts: 2, pending: true }] })
    a.getState().setText(0, 'x') // fuerza guardar
    const b = createAppStore(client, new ServedStore(client), { storage })
    expect(b.getState().chat.map(m => m.content)).toEqual(['hola', 'resp', 'otra', 'a medias'])
    expect(b.getState().chat[3]).toMatchObject({ error: expect.stringContaining('interrumpió') })
    expect(b.getState().chat[3].pending).toBeUndefined()
    expect(b.getState().chatBusy).toBe(false)
  })

  it('nueva conversación borra el historial y corta la respuesta en curso', async () => {
    const { client } = assistant(async function* (_r, signal) {
      yield { type: 'text', text: 'x' }
      await new Promise((_, rej) => signal?.addEventListener('abort', () => rej(new Error('abortado'))))
    })
    const s = mk(client)
    const p = s.getState().sendChat('hola')
    await vi.waitFor(() => expect(s.getState().chat[1]?.content).toBe('x'))
    s.getState().newChat()
    await p
    expect(s.getState().chat).toEqual([])
    expect(s.getState().chatBusy).toBe(false)
  })

  describe('propuesta', () => {
    const run = async (files: SourceFile[], text: string) => {
      const { client } = assistant(async function* () { yield { type: 'text', text: 'ok' }; yield proposal(files[0].path, text); yield done })
      const s = mk(client)
      s.setState({ files })
      await s.getState().sendChat('cambia')
      return s
    }

    it('llega con su estado y el texto base', async () => {
      const s = await run([F('uno\n')], 'dos')
      expect(s.getState().chat[1].proposal).toMatchObject({ status: 'pending', base: [F('uno\n')] })
    })

    it('aplicar guarda una versión, sustituye los archivos y se deshace de una vez', async () => {
      const s = await run([F('uno\n'), F('otro\n', 'b.cfg')], 'dos')
      expect(s.getState().applyProposal(1)).toBe(true)
      expect(s.getState().files).toEqual([F('dos\n'), F('otro\n', 'b.cfg')]) // conserva el salto de línea final
      expect(s.getState().versions[0]).toMatchObject({ label: 'Antes de aplicar propuesta del asistente', files: [F('uno\n'), F('otro\n', 'b.cfg')] })
      expect(s.getState().chat[1].proposal?.status).toBe('applied')
      expect(s.getState().toasts.at(-1)?.action?.label).toBe('Deshacer')
      expect(s.getState().applyProposal(1)).toBe(false) // ya aplicada
      s.getState().undo()
      expect(s.getState().files).toEqual([F('uno\n'), F('otro\n', 'b.cfg')])
    })

    it('descartar no toca los archivos', async () => {
      const s = await run([F('uno')], 'dos')
      s.getState().discardProposal(1)
      expect(s.getState().chat[1].proposal?.status).toBe('discarded')
      expect(s.getState().applyProposal(1)).toBe(false)
      expect(s.getState().files[0].text).toBe('uno')
    })

    it('detecta que la configuración cambió desde la petición', async () => {
      const s = await run([F('uno')], 'dos')
      const p = () => s.getState().chat[1].proposal!
      expect(proposalStale(p(), s.getState().files)).toBe(false)
      s.getState().setText(0, 'editado')
      expect(proposalStale(p(), s.getState().files)).toBe(true)
    })

    it('no aplica si el archivo propuesto ya no existe', async () => {
      const s = await run([F('uno')], 'dos')
      s.setState({ files: [F('uno', 'otro.cfg')] })
      expect(s.getState().applyProposal(1)).toBe(false)
      expect(s.getState().versions).toHaveLength(0)
      expect(s.getState().toasts.at(-1)?.kind).toBe('warn')
    })
  })
})


// ---------- almacén de configuración inyectado ----------

/** Almacén en memoria con control de versión, como lo tendría un repositorio. */
class MemoryStore implements ConfigStore {
  label = 'memoria'
  canSave = true
  storageKey = 'test.memoria'
  version = 1
  loads = 0
  saved: SourceFile[][] = []
  constructor(public files: SourceFile[], private loadedVersion = 1) {}
  async load() { this.loads++; this.loadedVersion = this.version; return this.files }
  async save(files: SourceFile[]): Promise<SaveResult> {
    if (this.loadedVersion !== this.version) return { saved: false, conflict: true, error: 'La versión remota cambió desde que se cargó' }
    this.saved.push(files)
    this.files = this.files.map(f => files.find(g => g.path === f.path) ?? f)
    this.version++
    this.loadedVersion = this.version
    return { saved: true }
  }
}

describe('ConfigStore inyectado', () => {
  it('init carga los archivos del almacén y los marca como lo de origen', async () => {
    const store = new MemoryStore([F('uno', 'a.cfg'), F('dos', 'b.cfg')])
    const s = mk(fakeClient(), store)
    await s.getState().init()
    expect(store.loads).toBe(1)
    expect(s.getState().files.map(f => f.text)).toEqual(['uno', 'dos'])
    expect(s.getState().dirty()).toEqual([])
    s.getState().setText(1, 'DOS')
    expect(s.getState().dirty()).toEqual([1])
  })

  it('guardar pasa por el almacén y marca los archivos como guardados', async () => {
    const store = new MemoryStore([F('uno', 'a.cfg'), F('dos', 'b.cfg')])
    const s = mk(fakeClient(), store)
    await s.getState().init()
    s.getState().setText(0, 'UNO')
    s.getState().setText(1, 'DOS')
    const res = await s.getState().save(s.getState().dirty())
    expect(res.saved).toBe(true)
    expect(store.saved).toHaveLength(1) // una sola llamada con todos los archivos
    expect(store.saved[0].map(f => f.text)).toEqual(['UNO', 'DOS'])
    expect(s.getState().dirty()).toEqual([])
  })

  it('un conflicto de versión no marca nada como guardado y reload() recupera lo remoto', async () => {
    const store = new MemoryStore([F('uno', 'a.cfg')])
    const s = mk(fakeClient(), store)
    await s.getState().init()
    s.getState().setText(0, 'LOCAL')
    store.version++ // alguien cambió el origen
    store.files = [F('REMOTO', 'a.cfg')]
    const res = await s.getState().save([0])
    expect(res.saved).toBe(false)
    expect(res.conflict).toBe(true)
    expect(s.getState().dirty()).toEqual([0])
    expect(await s.getState().reload()).toBe(true)
    expect(s.getState().files[0].text).toBe('REMOTO')
    expect(s.getState().dirty()).toEqual([])
    // Antes de recargar se guardó una versión con lo local, para no perderlo.
    expect(s.getState().versions.some(v => v.files[0].text === 'LOCAL')).toBe(true)
    // Tras recargar, guardar funciona de nuevo.
    s.getState().setText(0, 'NUEVO')
    expect((await s.getState().save([0])).saved).toBe(true)
  })

  it('si load() falla, avisa y la sesión sigue local', async () => {
    const store = new MemoryStore([])
    store.load = async () => { throw new Error('sin red') }
    const s = mk(fakeClient(), store)
    await s.getState().init()
    expect(s.getState().toasts.some(t => t.msg.includes('sin red') && t.msg.includes('memoria'))).toBe(true)
    expect(s.getState().files.length).toBeGreaterThan(0) // muestra
    expect(s.getState().disk).toBeNull()
  })

  it('dos almacenes con distinta storageKey no mezclan sesiones', async () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } }
    const a = createAppStore(fakeClient(), new MemoryStore([F('A')]), { storage })
    const other = new MemoryStore([F('B')]); other.storageKey = 'test.otra'
    const b = createAppStore(fakeClient(), other, { storage })
    await a.getState().init(); await b.getState().init()
    a.getState().setText(0, 'A2')
    expect([...mem.keys()].sort()).toEqual(['test.memoria', 'test.otra'])
    expect(b.getState().files[0].text).toBe('B')
  })
})

describe('ServedStore', () => {
  it('carga con getConfig, solo guarda si el servidor lo permite y guarda con putFiles', async () => {
    const put = vi.fn(async () => ({ saved: true }))
    const base = fakeClient({ getConfig: async () => [F('x')], putFiles: put, capabilities: async () => ({ ...(await fakeClient().capabilities()), canWrite: false }) })
    const ro = new ServedStore(base)
    expect(await ro.load()).toEqual([F('x')])
    expect(ro.canSave).toBe(false)
    expect(ro.readOnlyHint).toContain('--allow-write')
    const rw = new ServedStore({ ...base, capabilities: async () => ({ ...(await fakeClient().capabilities()), canWrite: true }) })
    await rw.load()
    expect(rw.canSave).toBe(true)
    await rw.save([F('y')])
    expect(put).toHaveBeenCalledWith([F('y')])
  })

  it('sin --file la sesión es local', async () => {
    const st = new ServedStore(fakeClient({ getConfig: async () => null }))
    expect(await st.load()).toBeNull()
  })
})
