import type { Analysis, Capabilities, CheckResult, DiffOp, SourceFile } from './types'

// La interfaz no sabe con qué núcleo habla:
//  - HttpCoreClient: `haro serve` local (Go nativo).
//  - WasmCoreClient (futuro): el mismo núcleo compilado a WebAssembly para la
//    web pública, sin servidor y sin que la configuración salga del navegador.
// Una configuración puede tener uno o varios archivos, en orden de carga.
export interface CoreClient {
  capabilities(): Promise<Capabilities>
  analyze(files: SourceFile[]): Promise<Analysis>
  diff(before: string, after: string): Promise<DiffOp[]>
  check(files: SourceFile[]): Promise<CheckResult>
  getConfig(): Promise<SourceFile[] | null>
  putFile(path: string, text: string): Promise<{ saved: boolean; error?: string }>
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
  analyze(files: SourceFile[]) { return call<Analysis>('POST', '/api/v1/analyze', { files }) }
  async diff(before: string, after: string) { return (await call<{ ops: DiffOp[] }>('POST', '/api/v1/diff', { before, after })).ops }
  check(files: SourceFile[]) { return call<CheckResult>('POST', '/api/v1/check', { files }) }
  async getConfig() {
    try { return (await call<{ files: SourceFile[] }>('GET', '/api/v1/config')).files }
    catch (e) { if ((e as { status?: number }).status === 404) return null; throw e }
  }
  async putFile(path: string, text: string) {
    try { return await call<{ saved: boolean }>('PUT', '/api/v1/config', { path, text }) }
    catch (e) { return { saved: false, error: (e as Error).message } }
  }
}
