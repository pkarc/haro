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
    proposeDirectives: async () => ({ directives: [], note: '', inputTokens: 0, outputTokens: 0 }),
    ...over,
  }
}
const mk = (client = fakeClient(), configStore: ConfigStore = new ServedStore(client)) => {
  const mem = new Map<string, string>()
  return createAppStore(client, configStore, { storage: { getItem: k => mem.get(k) ?? null, setItem: (k, v) => { mem.set(k, v) }, removeItem: k => { mem.delete(k) } } })
}
const F = (text: string, path = 'a.cfg'): SourceFile => ({ path, text })

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(0); vi.stubGlobal('navigator', { language: 'en-US' }) })
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

describe('undo and redo', () => {
  it('undoes and redoes edits', () => {
    const s = mk()
    s.setState({ files: [F('one')] })
    s.getState().setText(0, 'two')
    s.getState().setText(0, 'three')
    s.getState().undo()
    expect(s.getState().files[0].text).toBe('two')
    s.getState().undo()
    expect(s.getState().files[0].text).toBe('one')
    s.getState().redo()
    expect(s.getState().files[0].text).toBe('two')
    s.getState().setText(0, 'new')
    expect(s.getState().redoStack).toHaveLength(0)
  })

  it('merges consecutive keystrokes and splits after a pause', () => {
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
  it('applies the changes to the files', async () => {
    const client = fakeClient({ apply: async files => ({ files, changes: [{ file: 1, path: 'b.cfg', text: 'B2' }] }) })
    const s = mk(client)
    s.setState({ files: [F('A'), F('B', 'b.cfg')] })
    expect(await s.getState().applyOps([{ op: 'remove_section', section: 0 }])).toBe(true)
    expect(s.getState().files.map(f => f.text)).toEqual(['A', 'B2'])
    expect(s.getState().undoStack).toHaveLength(1)
  })

  it('shows a warning if apply fails and changes nothing', async () => {
    const s = mk(fakeClient({ apply: async () => { throw new Error('operation 1: does not exist') } }))
    s.setState({ files: [F('A')] })
    expect(await s.getState().applyOps([{ op: 'remove_section', section: 9 }])).toBe(false)
    expect(s.getState().files[0].text).toBe('A')
    expect(s.getState().toasts[0].msg).toContain('does not exist')
  })

  it('applyChanges rejects paths that do not match', () => {
    expect(() => applyChanges([F('x')], [{ file: 0, path: 'other.cfg', text: 'y' }])).toThrow()
  })
})

describe('technology templates', () => {
  const ops = [{ op: 'add_section' as const, type: 'backend', name: 'nextcloud', at: 1 }]

  it('saves a version before applying, changes the files and notifies with undo', async () => {
    const apply = vi.fn(async (files: SourceFile[]) => ({ files, changes: [{ file: 0, path: 'a.cfg', text: 'A\nbackend nextcloud' }] }))
    const s = mk(fakeClient({ apply }))
    s.setState({ files: [F('A')] })
    expect(await s.getState().applyTemplate(ops, 'Nextcloud')).toBe(true)
    expect(apply).toHaveBeenCalledWith([F('A')], ops)
    expect(s.getState().files[0].text).toBe('A\nbackend nextcloud')
    expect(s.getState().versions[0]).toMatchObject({ label: 'Before adding Nextcloud', files: [F('A')] })
    const toast = s.getState().toasts.at(-1)!
    expect(toast.msg).toContain('Nextcloud')
    toast.action!.run()
    expect(s.getState().files[0].text).toBe('A')
  })

  it('if apply fails nothing changes and no success is reported', async () => {
    const s = mk(fakeClient({ apply: async () => { throw new Error('operation 1: does not exist') } }))
    s.setState({ files: [F('A')] })
    expect(await s.getState().applyTemplate(ops, 'Nextcloud')).toBe(false)
    expect(s.getState().files[0].text).toBe('A')
    expect(s.getState().toasts.some(t => t.msg.includes('added'))).toBe(false)
  })
})

describe('analysis', () => {
  it('discards stale responses', async () => {
    const resolvers: Record<string, (a: Analysis) => void> = {}
    const s = mk(fakeClient({ analyze: f => new Promise(r => { resolvers[f[0].text] = r }) }))
    s.setState({ files: [F('v1')] })
    s.getState().refreshAnalysis(true)
    s.getState().setText(0, 'v2')
    await vi.advanceTimersByTimeAsync(250) // the debounce fires the second request
    resolvers.v2(emptyAnalysis('v2'))
    await vi.advanceTimersByTimeAsync(0)
    resolvers.v1(emptyAnalysis('v1')) // arrives late
    await vi.advanceTimersByTimeAsync(0)
    expect(s.getState().analysis?.doc.files?.[0]).toBe('v2')
    expect(s.getState().analysisRev).toBe(s.getState().rev)
  })

  it('groups several edits into a single analysis (debounce)', async () => {
    const analyze = vi.fn(async (f: SourceFile[]) => emptyAnalysis(f[0].text))
    const s = mk(fakeClient({ analyze }))
    s.setState({ files: [F('')] })
    for (const t of ['a', 'ab', 'abc']) s.getState().setText(0, t)
    await vi.advanceTimersByTimeAsync(250)
    expect(analyze).toHaveBeenCalledTimes(1)
  })
})

describe('files from disk', () => {
  it('served files replace the local session and are marked dirty on edit', () => {
    const s = mk()
    s.getState().loadServed([F('X', 'x.cfg')])
    expect(s.getState().files[0].text).toBe('X')
    s.getState().setText(0, 'Y')
    expect(s.getState().dirty()).toEqual([0])
    s.getState().markSaved('x.cfg', 'Y')
    expect(s.getState().dirty()).toEqual([])
  })

  it('keeps unsaved edits if the disk did not change', () => {
    const s = mk()
    s.getState().loadServed([F('X', 'x.cfg')])
    s.getState().setText(0, 'edited')
    s.getState().loadServed([F('X', 'x.cfg')])
    expect(s.getState().files[0].text).toBe('edited')
    s.getState().loadServed([F('X2', 'x.cfg')])
    expect(s.getState().files[0].text).toBe('X2')
  })

  it('dirtyIndexes ignores files that are not on disk', () => {
    expect(dirtyIndexes([F('a'), F('b', 'n.cfg')], [F('z')])).toEqual([0])
  })
})

describe('language', () => {
  const kn = (tag: string) => ({ categories: { net: tag }, sections: {}, directives: {}, rules: {}, templates: {} })

  it('defaults to English; Spanish only if the browser language starts with «es»', () => {
    const setNav = (language: string) => vi.stubGlobal('navigator', { language })
    setNav('en-US')
    expect(mk().getState().lang).toBe('en')
    setNav('es-MX')
    expect(mk().getState().lang).toBe('es')
    setNav('es')
    expect(mk().getState().lang).toBe('es')
    setNav('fr-FR')
    expect(mk().getState().lang).toBe('en')
    setNav('')
    expect(mk().getState().lang).toBe('en')
  })

  it('the interface messages follow the language', async () => {
    const s = mk(fakeClient({ apply: async () => { throw new Error('x') } }))
    s.setState({ files: [F('A')] })
    await s.getState().applyOps([])
    expect(s.getState().toasts.at(-1)?.msg).toBe('Could not apply the change: x')
    await s.getState().setLang('es')
    await s.getState().applyOps([])
    expect(s.getState().toasts.at(-1)?.msg).toBe('No se pudo aplicar el cambio: x')
    expect(s.getState().toasts.at(-1)?.msg).not.toMatch(/vosotros|vuestr/)
  })

  it('passes the language to analyze and knowledge, and on change re-analyzes and reloads the knowledge', async () => {
    const analyze = vi.fn(async (f: SourceFile[], _lang?: string) => emptyAnalysis(f[0].text))
    const knowledge = vi.fn(async (lang?: string) => kn(lang ?? '?'))
    const s = mk(fakeClient({ analyze, knowledge }))
    s.setState({ files: [F('x')], lang: 'en' })
    await s.getState().init()
    expect(analyze).toHaveBeenLastCalledWith([F('x')], 'en')
    expect(knowledge).toHaveBeenLastCalledWith('en')
    await s.getState().setLang('es')
    expect(s.getState().lang).toBe('es')
    expect(analyze).toHaveBeenLastCalledWith([F('x')], 'es')
    expect(knowledge).toHaveBeenLastCalledWith('es')
    expect(s.getState().knowledge?.categories.net).toBe('es')
    const n = analyze.mock.calls.length
    await s.getState().setLang('es')
    expect(analyze.mock.calls.length).toBe(n) // no change, no calls
  })

  it('persists the language and discards invalid values', () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } }
    const a = createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage })
    a.setState({ lang: 'es' })
    a.getState().setText(0, 'change') // forces the write to storage
    const saved = JSON.parse(mem.get('haro.v1')!)
    expect(saved.state.lang).toBe('es')
    expect(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().lang).toBe('es')
    saved.state.lang = 'klingon'
    mem.set('haro.v1', JSON.stringify(saved))
    expect(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().lang).toBe('en') // falls back to the browser language
  })
})

describe('assistant', () => {
  type Ev = import('../core/types').AssistEvent
  const done: Ev = { type: 'done', done: { stopReason: 'end_turn', refusalCategory: '', inputTokens: 1, outputTokens: 1 } }
  const proposal = (path: string, text: string): Ev => ({ type: 'proposal', proposal: { changes: [{ file: 0, path, text }], issuesAfter: [], unmatched: 0, truncated: false } })
  // client with a scripted assistant; remembers the requests
  const assistant = (script: (req: import('../core/types').AssistRequest, signal?: AbortSignal) => AsyncGenerator<Ev>) => {
    const calls: import('../core/types').AssistRequest[] = []
    return { calls, client: fakeClient({ assist: (req, signal) => { calls.push(req); return script(req, signal) } }) }
  }

  it('receives the streamed response and stores it with the language and files', async () => {
    const { client, calls } = assistant(async function* () { yield { type: 'text', text: 'He' }; yield { type: 'text', text: 'llo' }; yield done })
    const s = mk(client)
    s.setState({ files: [F('one')], lang: 'en' })
    await s.getState().sendChat('  question  ')
    expect(calls[0]).toEqual({ files: [F('one')], messages: [{ role: 'user', content: 'question' }], lang: 'en' })
    const chat = s.getState().chat
    expect(chat.map(m => [m.role, m.content])).toEqual([['user', 'question'], ['assistant', 'Hello']])
    expect(chat[1].pending).toBe(false)
    expect(s.getState().chatBusy).toBe(false)
  })

  it('shows the partial text while it arrives and blocks a second send', async () => {
    let release!: () => void
    const gate = new Promise<void>(r => { release = r })
    const { client, calls } = assistant(async function* () { yield { type: 'text', text: 'par' }; await gate; yield { type: 'text', text: 'tial' }; yield done })
    const s = mk(client)
    const p = s.getState().sendChat('hi')
    await vi.waitFor(() => expect(s.getState().chat[1]?.content).toBe('par'))
    expect(s.getState().chatBusy).toBe(true)
    expect(s.getState().chat[1].pending).toBe(true)
    await s.getState().sendChat('other') // ignored
    expect(calls).toHaveLength(1)
    release()
    await p
    expect(s.getState().chat[1].content).toBe('partial')
  })

  it('stopping cuts the stream and keeps what was received', async () => {
    const { client } = assistant(async function* (_r, signal) {
      yield { type: 'text', text: 'half' }
      await new Promise((_, rej) => signal?.addEventListener('abort', () => rej(new Error('aborted'))))
    })
    const s = mk(client)
    const p = s.getState().sendChat('hi')
    await vi.waitFor(() => expect(s.getState().chat[1]?.content).toBe('half'))
    s.getState().stopChat()
    await p
    const m = s.getState().chat[1]
    expect(m).toMatchObject({ content: 'half', stopped: true, pending: false })
    expect(m.error).toBeUndefined()
    expect(s.getState().chatBusy).toBe(false)
  })

  it('an error stays in the message and can be retried without repeating the user request', async () => {
    let n = 0
    const { client, calls } = assistant(async function* () {
      if (++n === 1) throw new Error('the Anthropic API rejected the credentials')
      yield { type: 'text', text: 'now it works' }
      yield done
    })
    const s = mk(client)
    await s.getState().sendChat('hi')
    expect(s.getState().chat[1].error).toContain('credentials')
    await s.getState().retryChat()
    expect(s.getState().chat.map(m => m.content)).toEqual(['hi', 'now it works'])
    expect(calls[1].messages).toEqual([{ role: 'user', content: 'hi' }])
  })

  it('the history sent alternates roles and omits failed messages', async () => {
    let n = 0
    const { client, calls } = assistant(async function* () {
      if (++n === 2) throw new Error('limit')
      yield { type: 'text', text: `r${n}` }
      yield done
    })
    const s = mk(client)
    await s.getState().sendChat('a')
    await s.getState().sendChat('b') // fails
    await s.getState().sendChat('c')
    expect(calls[2].messages).toEqual([{ role: 'user', content: 'a' }, { role: 'assistant', content: 'r1' }, { role: 'user', content: 'b\n\nc' }])
  })

  it('the history is saved, survives a reload and an unfinished response is marked as interrupted', async () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } }
    const { client } = assistant(async function* () { yield { type: 'text', text: 'resp' }; yield done })
    const a = createAppStore(client, new ServedStore(client), { storage })
    await a.getState().sendChat('hi')
    a.setState({ chat: [...a.getState().chat, { role: 'user', content: 'other', ts: 1 }, { role: 'assistant', content: 'half way', ts: 2, pending: true }] })
    a.getState().setText(0, 'x') // forces a save
    const b = createAppStore(client, new ServedStore(client), { storage })
    expect(b.getState().chat.map(m => m.content)).toEqual(['hi', 'resp', 'other', 'half way'])
    expect(b.getState().chat[3]).toMatchObject({ error: expect.stringContaining('interrupted') })
    expect(b.getState().chat[3].pending).toBeUndefined()
    expect(b.getState().chatBusy).toBe(false)
  })

  it('a new conversation clears the history and cuts the response in progress', async () => {
    const { client } = assistant(async function* (_r, signal) {
      yield { type: 'text', text: 'x' }
      await new Promise((_, rej) => signal?.addEventListener('abort', () => rej(new Error('aborted'))))
    })
    const s = mk(client)
    const p = s.getState().sendChat('hi')
    await vi.waitFor(() => expect(s.getState().chat[1]?.content).toBe('x'))
    s.getState().newChat()
    await p
    expect(s.getState().chat).toEqual([])
    expect(s.getState().chatBusy).toBe(false)
  })

  describe('proposal', () => {
    const run = async (files: SourceFile[], text: string) => {
      const { client } = assistant(async function* () { yield { type: 'text', text: 'ok' }; yield proposal(files[0].path, text); yield done })
      const s = mk(client)
      s.setState({ files })
      await s.getState().sendChat('change')
      return s
    }

    it('arrives with its status and the base text', async () => {
      const s = await run([F('one\n')], 'two')
      expect(s.getState().chat[1].proposal).toMatchObject({ status: 'pending', base: [F('one\n')] })
    })

    it('applying saves a version, replaces the files and is undone in one step', async () => {
      const s = await run([F('one\n'), F('another\n', 'b.cfg')], 'two')
      expect(s.getState().applyProposal(1)).toBe(true)
      expect(s.getState().files).toEqual([F('two\n'), F('another\n', 'b.cfg')]) // keeps the final newline
      expect(s.getState().versions[0]).toMatchObject({ label: 'Before applying assistant proposal', files: [F('one\n'), F('another\n', 'b.cfg')] })
      expect(s.getState().chat[1].proposal?.status).toBe('applied')
      expect(s.getState().toasts.at(-1)?.action?.label).toBe('Undo')
      expect(s.getState().applyProposal(1)).toBe(false) // already applied
      s.getState().undo()
      expect(s.getState().files).toEqual([F('one\n'), F('another\n', 'b.cfg')])
    })

    it('discarding does not touch the files', async () => {
      const s = await run([F('one')], 'two')
      s.getState().discardProposal(1)
      expect(s.getState().chat[1].proposal?.status).toBe('discarded')
      expect(s.getState().applyProposal(1)).toBe(false)
      expect(s.getState().files[0].text).toBe('one')
    })

    it('detects that the configuration changed since the request', async () => {
      const s = await run([F('one')], 'two')
      const p = () => s.getState().chat[1].proposal!
      expect(proposalStale(p(), s.getState().files)).toBe(false)
      s.getState().setText(0, 'edited')
      expect(proposalStale(p(), s.getState().files)).toBe(true)
    })

    it('does not apply if the proposed file no longer exists', async () => {
      const s = await run([F('one')], 'two')
      s.setState({ files: [F('one', 'another.cfg')] })
      expect(s.getState().applyProposal(1)).toBe(false)
      expect(s.getState().versions).toHaveLength(0)
      expect(s.getState().toasts.at(-1)?.kind).toBe('warn')
    })
  })
})


// ---------- injected configuration store ----------

/** In-memory store with version control, like a repository would have. */
class MemoryStore implements ConfigStore {
  label = 'memory'
  canSave = true
  storageKey = 'test.memory'
  version = 1
  loads = 0
  saved: SourceFile[][] = []
  constructor(public files: SourceFile[], private loadedVersion = 1) {}
  async load() { this.loads++; this.loadedVersion = this.version; return this.files }
  async save(files: SourceFile[]): Promise<SaveResult> {
    if (this.loadedVersion !== this.version) return { saved: false, conflict: true, error: 'The remote version changed since it was loaded' }
    this.saved.push(files)
    this.files = this.files.map(f => files.find(g => g.path === f.path) ?? f)
    this.version++
    this.loadedVersion = this.version
    return { saved: true }
  }
}

describe('injected ConfigStore', () => {
  it('init loads the files from the store and marks them as the origin', async () => {
    const store = new MemoryStore([F('one', 'a.cfg'), F('two', 'b.cfg')])
    const s = mk(fakeClient(), store)
    await s.getState().init()
    expect(store.loads).toBe(1)
    expect(s.getState().files.map(f => f.text)).toEqual(['one', 'two'])
    expect(s.getState().dirty()).toEqual([])
    s.getState().setText(1, 'DOS')
    expect(s.getState().dirty()).toEqual([1])
  })

  it('saving goes through the store and marks the files as saved', async () => {
    const store = new MemoryStore([F('one', 'a.cfg'), F('two', 'b.cfg')])
    const s = mk(fakeClient(), store)
    await s.getState().init()
    s.getState().setText(0, 'UNO')
    s.getState().setText(1, 'DOS')
    const res = await s.getState().save(s.getState().dirty())
    expect(res.saved).toBe(true)
    expect(store.saved).toHaveLength(1) // a single call with all the files
    expect(store.saved[0].map(f => f.text)).toEqual(['UNO', 'DOS'])
    expect(s.getState().dirty()).toEqual([])
  })

  it('a version conflict marks nothing as saved and reload() recovers the remote', async () => {
    const store = new MemoryStore([F('one', 'a.cfg')])
    const s = mk(fakeClient(), store)
    await s.getState().init()
    s.getState().setText(0, 'LOCAL')
    store.version++ // somebody changed the origin
    store.files = [F('REMOTE', 'a.cfg')]
    const res = await s.getState().save([0])
    expect(res.saved).toBe(false)
    expect(res.conflict).toBe(true)
    expect(s.getState().dirty()).toEqual([0])
    expect(await s.getState().reload()).toBe(true)
    expect(s.getState().files[0].text).toBe('REMOTE')
    expect(s.getState().dirty()).toEqual([])
    // Before reloading, a version with the local state was saved so it is not lost.
    expect(s.getState().versions.some(v => v.files[0].text === 'LOCAL')).toBe(true)
    // After reloading, saving works again.
    s.getState().setText(0, 'NEW')
    expect((await s.getState().save([0])).saved).toBe(true)
  })

  it('if load() fails, it warns and the session stays local', async () => {
    const store = new MemoryStore([])
    store.load = async () => { throw new Error('no network') }
    const s = mk(fakeClient(), store)
    await s.getState().init()
    expect(s.getState().toasts.some(t => t.msg.includes('no network') && t.msg.includes('memory'))).toBe(true)
    expect(s.getState().files.length).toBeGreaterThan(0) // sample
    expect(s.getState().disk).toBeNull()
  })

  it('two stores with different storageKey do not mix sessions', async () => {
    const mem = new Map<string, string>()
    const storage = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } }
    const a = createAppStore(fakeClient(), new MemoryStore([F('A')]), { storage })
    const other = new MemoryStore([F('B')]); other.storageKey = 'test.other'
    const b = createAppStore(fakeClient(), other, { storage })
    await a.getState().init(); await b.getState().init()
    a.getState().setText(0, 'A2')
    expect([...mem.keys()].sort()).toEqual(['test.memory', 'test.other'])
    expect(b.getState().files[0].text).toBe('B')
  })
})

describe('ServedStore', () => {
  it('loads with getConfig, only saves if the server allows it and saves with putFiles', async () => {
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

  it('without --file the session is local', async () => {
    const st = new ServedStore(fakeClient({ getConfig: async () => null }))
    expect(await st.load()).toBeNull()
  })
})

describe('page, focus channels and cursor', () => {
  const memStorage = () => {
    const mem = new Map<string, string>()
    return { mem, storage: { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => { mem.set(k, v) }, removeItem: (k: string) => { mem.delete(k) } } }
  }

  it('defaults to the edit page, persists the map page and migrates states without page', () => {
    const { mem, storage } = memStorage()
    const a = createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage })
    expect(a.getState().page).toBe('edit')
    a.getState().setPage('map')
    a.getState().setText(0, 'x') // forces the write
    expect(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().page).toBe('map')
    // an older saved state (no `page`, or garbage) falls back to «edit»
    const saved = JSON.parse(mem.get('haro.v1')!)
    delete saved.state.page
    mem.set('haro.v1', JSON.stringify(saved))
    expect(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().page).toBe('edit')
    saved.state.page = 'nonsense'
    mem.set('haro.v1', JSON.stringify(saved))
    expect(createAppStore(fakeClient(), new ServedStore(fakeClient()), { storage }).getState().page).toBe('edit')
  })

  it('goto leaves the map page and sets both focus channels; they clear independently', () => {
    const s = mk()
    s.getState().setPage('map')
    s.getState().goto(0, 3)
    const st = s.getState()
    expect(st.page).toBe('edit')
    expect(st.codeFocus).toMatchObject({ file: 0, line: 3 })
    expect(st.visualFocus).toMatchObject({ file: 0, line: 3 })
    s.getState().clearCodeFocus()
    expect(s.getState().codeFocus).toBeNull()
    expect(s.getState().visualFocus).not.toBeNull()
    s.getState().clearVisualFocus()
    expect(s.getState().visualFocus).toBeNull()
  })

  it('revealInCode switches the active file and only touches the code channel', () => {
    const s = mk()
    s.getState().importFiles([F('a', 'a.cfg'), F('b', 'b.cfg')])
    s.getState().revealInCode(1, 2)
    const st = s.getState()
    expect(st.active).toBe(1)
    expect(st.mode).toBe('code')
    expect(st.codeFocus).toMatchObject({ file: 1, line: 2 })
    expect(st.visualFocus).toBeNull()
  })

  it('setMode and setPage leave the assistant tab', () => {
    const s = mk()
    s.getState().setShowAssistant(true)
    s.getState().setPage('map')
    expect(s.getState().showAssistant).toBe(false)
    s.getState().setShowAssistant(true)
    s.getState().setMode('code')
    expect(s.getState()).toMatchObject({ showAssistant: false, page: 'edit', mode: 'code' })
  })

  it('setCursor records the line and ignores repeats', () => {
    const s = mk()
    s.getState().setCursor(0, 4, true)
    const first = s.getState().cursor
    expect(first).toMatchObject({ file: 0, line: 4, user: true })
    s.getState().setCursor(0, 4, true)
    expect(s.getState().cursor).toBe(first)
    s.getState().setCursor(0, 5, false)
    expect(s.getState().cursor).toMatchObject({ line: 5, user: false })
  })
})

// A tiny stand-in for the core: sections are lines that start in column 0, their
// directives the indented lines; apply understands add_directive (and nothing else).
describe('directive proposals', () => {
  type PD = import('../core/types').ProposedDirective
  const sectionsOf = (files: SourceFile[]) => {
    const out: import('../core/types').Section[] = []
    files.forEach((f, fi) => f.text.split('\n').forEach((raw, ln) => {
      if (raw && !/^\s/.test(raw)) {
        const [type, name = ''] = raw.split(' ')
        out.push({ file: fi, type, name, line: ln + 1, indent: '', text: raw, tokens: [], comment: '', lines: [] })
      } else if (raw.trim() && out.length) {
        const sec = out[out.length - 1]
        sec.lines.push({ file: sec.file, kind: 'directive', line: ln + 1, indent: '    ', text: raw.trim(), keyword: raw.trim().split(' ')[0], args: [], comment: '' })
      }
    }))
    return out
  }
  const fake = (propose: (req: import('../core/types').ProposeDirectivesRequest, signal?: AbortSignal) => Promise<{ directives: PD[]; note?: string }>) => {
    const reqs: import('../core/types').ProposeDirectivesRequest[] = []
    const client = fakeClient({
      analyze: async files => ({ ...emptyAnalysis(files[0].text), doc: { pre: [], files: files.map(f => f.path), sections: sectionsOf(files) } }),
      apply: async (files, ops) => {
        const lines = files[0].text.split('\n')
        for (const op of ops) {
          if (op.op !== 'add_directive') throw new Error('unsupported')
          const sec = sectionsOf(files)[op.section ?? 0]
          if (!sec) throw new Error('no such section')
          const start = sec.line // header is line `sec.line` (1-based) = index sec.line-1; body starts at sec.line
          const body = lines.slice(start).findIndex(l => l && !/^\s/.test(l))
          const len = body < 0 ? lines.length - start : body
          const at = op.at ?? len
          lines.splice(start + at, 0, '    ' + op.text)
        }
        return { files, changes: [{ file: 0, path: files[0].path, text: lines.join('\n') }] }
      },
      proposeDirectives: async (req, signal) => { reqs.push(req); const r = await propose(req, signal); return { note: '', inputTokens: 1, outputTokens: 1, ...r } },
    })
    return { client, reqs }
  }
  const pd = (text: string, at = -1, extra: Partial<PD> = {}): PD => ({ text, explanation: `Explains ${text}`, issuesAfter: [], known: true, at, ...extra })
  const KEY = '0:backend:app#0'
  const CFG = 'backend app\n    balance roundrobin\n    server a 1.1.1.1:80\nfrontend web\n    bind :80'
  const setup = (propose: Parameters<typeof fake>[0]) => {
    const { client, reqs } = fake(propose)
    const s = mk(client)
    s.setState({ files: [F(CFG)] })
    return { s, reqs }
  }
  const texts = (s: ReturnType<typeof mk>) => s.getState().files[0].text.split('\n')

  it('proposes with the current files, the section index and the language, and keeps it transient', async () => {
    const { s, reqs } = setup(async () => ({ directives: [pd('timeout server 30s', 2), pd('retries 3')], note: 'hello' }))
    await s.getState().proposeForSection(KEY, '  slow clients  ')
    expect(reqs).toEqual([{ files: [F(CFG)], section: 0, instruction: 'slow clients', lang: 'en' }])
    const g = s.getState().proposals[KEY]
    expect(g).toMatchObject({ status: 'ready', instruction: 'slow clients', note: 'hello' })
    if (g.status !== 'ready') throw new Error()
    expect(g.items.map(i => [i.text, i.at, i.known])).toEqual([['timeout server 30s', 2, true], ['retries 3', -1, true]])
    expect(new Set(g.items.map(i => i.id)).size).toBe(2)
    expect(s.getState().undoStack).toHaveLength(0)
    expect(s.getState().files[0].text).toBe(CFG) // nothing applied
  })

  it('shows the pending state, with the instruction, while it waits', async () => {
    let release!: () => void
    const gate = new Promise<void>(r => { release = r })
    const { s } = setup(async () => { await gate; return { directives: [] } })
    const p = s.getState().proposeForSection(KEY, 'something')
    await vi.waitFor(() => expect(s.getState().proposals[KEY]).toEqual({ status: 'pending', instruction: 'something' }))
    release()
    await p
    expect(s.getState().proposals[KEY]).toMatchObject({ status: 'ready', items: [] })
  })

  it('keeps the message of a failed request and retrying works', async () => {
    let n = 0
    const { s } = setup(async () => { if (++n === 1) throw new Error('credentials rejected'); return { directives: [pd('retries 3')] } })
    await s.getState().proposeForSection(KEY, 'x')
    expect(s.getState().proposals[KEY]).toEqual({ status: 'error', instruction: 'x', message: 'credentials rejected' })
    await s.getState().proposeForSection(KEY, 'x')
    expect(s.getState().proposals[KEY].status).toBe('ready')
  })

  it('cancelling aborts the request and the late answer is ignored', async () => {
    let aborted = false
    const { s, reqs } = setup((_r, signal) => new Promise((_, rej) => signal?.addEventListener('abort', () => { aborted = true; rej(new Error('aborted')) })))
    const p = s.getState().proposeForSection(KEY, 'x')
    await vi.waitFor(() => expect(reqs).toHaveLength(1))
    s.getState().cancelProposal(KEY)
    await p
    expect(aborted).toBe(true)
    expect(s.getState().proposals[KEY]).toBeUndefined()
  })

  it('approving applies add_directive at the suggested line, removes the proposal and can be undone', async () => {
    const { s } = setup(async () => ({ directives: [pd('option httplog', 1)] }))
    await s.getState().proposeForSection(KEY, 'x')
    const id = (s.getState().proposals[KEY] as { items: { id: number }[] }).items[0].id
    expect(await s.getState().approveProposal(KEY, id)).toBe(true)
    expect(texts(s)).toEqual(['backend app', '    balance roundrobin', '    option httplog', '    server a 1.1.1.1:80', 'frontend web', '    bind :80'])
    expect(s.getState().proposals[KEY]).toBeUndefined()
    expect(s.getState().undoStack).toHaveLength(1)
    expect(s.getState().approved).toMatchObject({ key: KEY, texts: ['option httplog'] })
    expect(s.getState().toasts.at(-1)).toMatchObject({ msg: '1 directive added', kind: 'info' })
    s.getState().toasts.at(-1)!.action!.run()
    expect(s.getState().files[0].text).toBe(CFG)
  })

  it('without a suggested position (-1) the directive goes at the end of the section', async () => {
    const { s } = setup(async () => ({ directives: [pd('retries 3', -1)] }))
    await s.getState().proposeForSection(KEY, 'x')
    await s.getState().approveProposal(KEY, (s.getState().proposals[KEY] as { items: { id: number }[] }).items[0].id)
    expect(texts(s).slice(0, 4)).toEqual(['backend app', '    balance roundrobin', '    server a 1.1.1.1:80', '    retries 3'])
  })

  it('approving one keeps the others pointing to the right line', async () => {
    const { s } = setup(async () => ({ directives: [pd('option httplog', 1), pd('timeout server 30s', 2)] }))
    await s.getState().proposeForSection(KEY, 'x')
    const items = (s.getState().proposals[KEY] as { items: { id: number; at: number }[] }).items
    await s.getState().approveProposal(KEY, items[0].id)
    const left = s.getState().proposals[KEY] as { items: { text: string; at: number }[] }
    expect(left.items.map(i => [i.text, i.at])).toEqual([['timeout server 30s', 3]])
    await s.getState().approveProposal(KEY, items[1].id)
    expect(texts(s).slice(0, 5)).toEqual(['backend app', '    balance roundrobin', '    option httplog', '    server a 1.1.1.1:80', '    timeout server 30s'])
    expect(s.getState().proposals[KEY]).toBeUndefined()
  })

  it('approve all sends the ops from the highest position to the lowest, ends first, in one undo step', async () => {
    const batches: string[][] = []
    const { client } = fake(async () => ({ directives: [pd('A', 1), pd('B', 2), pd('C', -1), pd('D', 1)] }))
    const orig = client.apply.bind(client)
    client.apply = async (files, ops) => { batches.push(ops.map(o => `${o.text}@${o.at ?? 'end'}`)); return orig(files, ops) }
    const s = mk(client)
    s.setState({ files: [F(CFG)] })
    await s.getState().proposeForSection(KEY, 'x')
    expect(await s.getState().approveAll(KEY)).toBe(true)
    expect(batches).toEqual([['C@end', 'B@2', 'D@1', 'A@1']])
    expect(s.getState().undoStack).toHaveLength(1)
    // A ends up before D, as proposed
    expect(texts(s).slice(0, 7)).toEqual(['backend app', '    balance roundrobin', '    A', '    D', '    server a 1.1.1.1:80', '    B', '    C'])
    expect(s.getState().proposals[KEY]).toBeUndefined()
    expect(s.getState().toasts.at(-1)!.msg).toBe('4 directives added')
  })

  it('reject removes one, reject all removes the group', async () => {
    const { s } = setup(async () => ({ directives: [pd('A'), pd('B')] }))
    await s.getState().proposeForSection(KEY, 'x')
    const items = (s.getState().proposals[KEY] as { items: { id: number }[] }).items
    s.getState().rejectProposal(KEY, items[0].id)
    expect((s.getState().proposals[KEY] as { items: unknown[] }).items).toHaveLength(1)
    s.getState().rejectProposal(KEY, items[1].id)
    expect(s.getState().proposals[KEY]).toBeUndefined()
    await s.getState().proposeForSection(KEY, 'x')
    s.getState().rejectAll(KEY)
    expect(s.getState().proposals).toEqual({})
    expect(s.getState().files[0].text).toBe(CFG)
  })

  it('if the section no longer exists it warns and discards the proposals', async () => {
    const { s } = setup(async () => ({ directives: [pd('A')] }))
    await s.getState().proposeForSection(KEY, 'x')
    const id = (s.getState().proposals[KEY] as { items: { id: number }[] }).items[0].id
    s.setState({ files: [F('frontend web\n    bind :80')], rev: s.getState().rev + 1 })
    expect(await s.getState().approveProposal(KEY, id)).toBe(false)
    expect(s.getState().proposals[KEY]).toBeUndefined()
    expect(s.getState().toasts.at(-1)).toMatchObject({ kind: 'warn' })
    expect(s.getState().toasts.at(-1)!.msg).toContain('no longer exists')
  })

  it('proposals of sections that disappear after an analysis are dropped', async () => {
    const { s } = setup(async () => ({ directives: [pd('A')] }))
    await s.getState().proposeForSection(KEY, 'x')
    s.getState().setText(0, 'frontend web\n    bind :80')
    await vi.advanceTimersByTimeAsync(400)
    expect(s.getState().proposals).toEqual({})
  })

  it('when the section changed since proposing, the position is recalculated (after the same keyword, else the end) and the user is warned', async () => {
    const { s } = setup(async () => ({ directives: [pd('server b 2.2.2.2:80', 1), pd('retries 3', 1)] }))
    await s.getState().proposeForSection(KEY, 'x')
    // the user adds two lines at the top of the section: the suggested index 1 no longer fits
    s.getState().setText(0, CFG.replace('backend app\n', 'backend app\n    mode http\n    option httplog\n'))
    const items = (s.getState().proposals[KEY] as { items: { id: number }[] }).items
    expect(await s.getState().approveAll(KEY)).toBe(true)
    expect(s.getState().toasts.some(t => t.kind === 'warn' && t.msg.includes('recalculated'))).toBe(true)
    // server b after server a; retries at the end
    expect(texts(s).slice(0, 7)).toEqual(['backend app', '    mode http', '    option httplog', '    balance roundrobin', '    server a 1.1.1.1:80', '    server b 2.2.2.2:80', '    retries 3'])
    expect(items).toHaveLength(2)
  })

  it('a failing apply keeps the proposals', async () => {
    const { client } = fake(async () => ({ directives: [pd('A')] }))
    client.apply = async () => { throw new Error('boom') }
    const s = mk(client)
    s.setState({ files: [F(CFG)] })
    await s.getState().proposeForSection(KEY, 'x')
    expect(await s.getState().approveAll(KEY)).toBe(false)
    expect(s.getState().proposals[KEY].status).toBe('ready')
  })
})
