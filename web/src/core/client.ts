import type { Analysis, ApplyResult, Capabilities, CheckResult, DiffOp, Knowledge, Op, PutResult, SourceFile } from './types'

// La interfaz no sabe con qué núcleo habla:
//  - HttpCoreClient: `haro serve` local (Go nativo).
//  - WasmCoreClient (futuro): el mismo núcleo compilado a WebAssembly para la
//    web pública, sin servidor y sin que la configuración salga del navegador.
// Una configuración puede tener uno o varios archivos, en orden de carga.
export interface CoreClient {
  capabilities(): Promise<Capabilities>
  analyze(files: SourceFile[]): Promise<Analysis>
  apply(files: SourceFile[], ops: Op[]): Promise<ApplyResult>
  diff(before: string, after: string): Promise<DiffOp[]>
  check(files: SourceFile[]): Promise<CheckResult>
  getConfig(): Promise<SourceFile[] | null>
  putFile(path: string, text: string): Promise<PutResult>
  // Guarda varios archivos a la vez: se valida el conjunto y se guarda todo o nada.
  putFiles(files: SourceFile[]): Promise<PutResult>
  knowledge(): Promise<Knowledge>
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
  apply(files: SourceFile[], ops: Op[]) { return call<ApplyResult>('POST', '/api/v1/apply', { files, ops }) }
  async diff(before: string, after: string) { return (await call<{ ops: DiffOp[] }>('POST', '/api/v1/diff', { before, after })).ops }
  check(files: SourceFile[]) { return call<CheckResult>('POST', '/api/v1/check', { files }) }
  async getConfig() {
    try { return (await call<{ files: SourceFile[] }>('GET', '/api/v1/config')).files }
    catch (e) { if ((e as { status?: number }).status === 404) return null; throw e }
  }
  knowledge() { return call<Knowledge>('GET', '/api/v1/knowledge') }
  putFile(path: string, text: string): Promise<PutResult> { return this.put({ path, text }) }
  putFiles(files: SourceFile[]): Promise<PutResult> { return this.put({ files }) }
  private async put(body: unknown): Promise<PutResult> {
    try { return await call<PutResult>('PUT', '/api/v1/config', body) }
    catch (e) {
      const err = e as Error & { status?: number; data?: Partial<PutResult> }
      // El cuerpo del 422 trae check (salida de haproxy -c) o issue (detector).
      return { ...err.data, saved: false, error: err.message, status: err.status }
    }
  }
}
