import { Code, ConnectError, createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { ConfigService, CoreService } from '../gen/haro/v1/haro_pb'
import type { Analysis, ApplyResult, Capabilities, CheckResult, DiffOp, Knowledge, Op, PutResult, SourceFile } from './types'

// La interfaz no sabe con qué núcleo habla:
//  - ConnectCoreClient: `haro serve` local (Go nativo), con el cliente
//    generado por buf desde proto/haro/v1/haro.proto.
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

// Los errores de RPC llegan como Error con el mensaje del servidor (sin el
// prefijo «[código]» de ConnectError) y el código en `code`.
function fail(e: unknown): never {
  const err = ConnectError.from(e)
  throw Object.assign(new Error(err.rawMessage), { code: err.code })
}

async function rpc<T>(p: Promise<T>): Promise<T> {
  try { return await p } catch (e) { fail(e) }
}

// Los mensajes llevan $typeName; la interfaz trabaja con objetos planos y
// tipos más estrictos (ver types.ts). El núcleo siempre rellena doc y topology.
const plain = <T>(m: unknown) => m as T

export class ConnectCoreClient implements CoreClient {
  private core: ReturnType<typeof createClient<typeof CoreService>>
  private config: ReturnType<typeof createClient<typeof ConfigService>>

  constructor(baseUrl = '') {
    // JSON, no binario: mismo formato que los mensajes del contrato y fácil de depurar.
    const transport = createConnectTransport({ baseUrl, useBinaryFormat: false })
    this.core = createClient(CoreService, transport)
    this.config = createClient(ConfigService, transport)
  }

  async capabilities() { return plain<Capabilities>(await rpc(this.config.getCapabilities({}))) }
  async analyze(files: SourceFile[]) { return plain<Analysis>(await rpc(this.core.analyze({ files }))) }
  async apply(files: SourceFile[], ops: Op[]) { return plain<ApplyResult>(await rpc(this.core.apply({ files, ops }))) }
  async diff(before: string, after: string) { return plain<DiffOp[]>((await rpc(this.core.diff({ before, after }))).ops) }
  async check(files: SourceFile[]) { return plain<CheckResult>(await rpc(this.core.check({ files }))) }
  async knowledge() { return plain<Knowledge>(await rpc(this.core.getKnowledge({}))) }

  async getConfig() {
    try { return plain<SourceFile[]>((await this.config.getConfig({})).files) }
    catch (e) {
      if (ConnectError.from(e).code === Code.NotFound) return null // haro serve sin --file
      fail(e)
    }
  }

  putFile(path: string, text: string): Promise<PutResult> { return this.put({ path, text }) }
  putFiles(files: SourceFile[]): Promise<PutResult> { return this.put({ files }) }

  private async put(req: { path?: string; text?: string; files?: SourceFile[] }): Promise<PutResult> {
    try {
      const r = await this.config.putConfig(req)
      return {
        saved: r.saved,
        error: r.error || undefined,
        check: r.check ? plain<CheckResult>(r.check) : undefined,
        issue: r.issue ? plain<PutResult['issue']>(r.issue) : undefined,
        path: r.path || undefined,
      }
    } catch (e) {
      const err = ConnectError.from(e)
      return { saved: false, error: err.rawMessage, status: err.code === Code.PermissionDenied ? 403 : 400 }
    }
  }
}
