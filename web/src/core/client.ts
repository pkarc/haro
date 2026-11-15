import type { Analysis, Capabilities, CheckResult, DiffOp } from './types'

// La interfaz no sabe con qué núcleo habla:
//  - HttpCoreClient: `haro serve` local (Go nativo).
//  - WasmCoreClient (futuro): el mismo núcleo compilado a WebAssembly para la
//    web pública, sin servidor y sin que la configuración salga del navegador.
export interface CoreClient {
  capabilities(): Promise<Capabilities>
  analyze(text: string): Promise<Analysis>
  diff(before: string, after: string): Promise<DiffOp[]>
  check(text: string): Promise<CheckResult>
  getConfig(): Promise<{ path: string; text: string } | null>
  putConfig(text: string): Promise<{ saved: boolean; error?: string }>
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { 'content-type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw Object.assign(new Error(data.error ?? `HTTP ${res.status}`), { status: res.status, data })
  return data as T
}

export class HttpCoreClient implements CoreClient {
  capabilities() { return call<Capabilities>('GET', '/api/v1/capabilities') }
  analyze(text: string) { return call<Analysis>('POST', '/api/v1/analyze', { text }) }
  async diff(before: string, after: string) { return (await call<{ ops: DiffOp[] }>('POST', '/api/v1/diff', { before, after })).ops }
  check(text: string) { return call<CheckResult>('POST', '/api/v1/check', { text }) }
  async getConfig() {
    try { return await call<{ path: string; text: string }>('GET', '/api/v1/config') }
    catch (e) { if ((e as { status?: number }).status === 404) return null; throw e }
  }
  async putConfig(text: string) {
    try { return await call<{ saved: boolean }>('PUT', '/api/v1/config', { text }) }
    catch (e) { return { saved: false, error: (e as Error).message } }
  }
}
