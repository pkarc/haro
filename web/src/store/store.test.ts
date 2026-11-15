import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CoreClient } from '../core/client'
import type { Analysis, SourceFile } from '../core/types'
import { applyChanges, dirtyIndexes } from './files'
import { createAppStore, COALESCE_MS } from './store'

const emptyAnalysis = (tag: string): Analysis => ({
  doc: { pre: [], sections: [], files: [tag] }, issues: [], explanations: [], effective: [],
  topology: { entries: [], backends: [], edges: [], missing: [] },
})

function fakeClient(over: Partial<CoreClient> = {}): CoreClient {
  return {
    capabilities: async () => ({ version: 't', canRead: false, canWrite: false, canCheck: false }),
    analyze: async f => emptyAnalysis(f[0].text),
    apply: async files => ({ files, changes: [] }),
    diff: async () => [],
    check: async () => ({ available: false, ok: false, output: '' }),
    getConfig: async () => null,
    putFile: async () => ({ saved: true }),
    knowledge: async () => ({ categories: {}, sections: {}, directives: {}, rules: {}, templates: {} }),
    ...over,
  }
}
const mk = (client = fakeClient()) => {
  const mem = new Map<string, string>()
  return createAppStore(client, { storage: { getItem: k => mem.get(k) ?? null, setItem: (k, v) => { mem.set(k, v) }, removeItem: k => { mem.delete(k) } } })
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
