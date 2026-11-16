import { fromJson, toJson, type DescMessage, type MessageShape } from '@bufbuild/protobuf'
import { ValueSchema } from '@bufbuild/protobuf/wkt'
import { AnalysisSchema, ApplyResponseSchema, DiffResponseSchema, KnowledgeSchema, ListTemplatesResponseSchema, RenderTemplateResponseSchema } from '../gen/haro/v1/haro_pb'
import type { CoreClient } from './client'
import type { Analysis, ApplyResult, AssistEvent, AssistRequest, Capabilities, CheckResult, DiffOp, Knowledge, Lang, Op, ParamValue, PutResult, RenderResult, SourceFile, TechTemplate } from './types'

// WasmCoreClient: el mismo núcleo de Go compilado a WebAssembly (ver wasm/ y
// `make wasm`). Para la web pública sin servidor: la configuración nunca sale
// del navegador. No hay disco ni haproxy: no se puede leer ni guardar en
// archivos (la web usa el almacenamiento del navegador) ni validar con
// haproxy -c.
//
// El módulo habla JSON con los mismos nombres de campo que el contrato proto;
// aquí se valida y completa con el código generado (fromJson), de modo que la
// interfaz recibe exactamente lo mismo que con ConnectCoreClient.

interface GoGlobal {
  Go: new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): Promise<void> }
  haroCore?: { call(method: string, request: string): string }
}

const scope = globalThis as unknown as GoGlobal

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const el = document.createElement('script')
    el.src = src
    el.onload = () => resolve()
    el.onerror = () => reject(new Error(`No se pudo cargar ${src}`))
    document.head.appendChild(el)
  })
}

export class WasmCoreClient implements CoreClient {
  private ready: Promise<void> | null = null

  /** `base` es la URL del directorio con haro.wasm y wasm_exec.js. */
  constructor(private base = `${import.meta.env.BASE_URL}wasm/`) {}

  private load(): Promise<void> {
    this.ready ??= (async () => {
      if (!scope.Go) await loadScript(`${this.base}wasm_exec.js`)
      const go = new scope.Go()
      const { instance } = await WebAssembly.instantiateStreaming(fetch(`${this.base}haro.wasm`), go.importObject)
      void go.run(instance) // no termina: el módulo atiende llamadas hasta que se cierra la página
      for (let i = 0; i < 100 && !scope.haroCore; i++) await new Promise(r => setTimeout(r, 20))
      if (!scope.haroCore) throw new Error('El módulo WebAssembly no arrancó')
    })()
    // Si falla la carga, permite reintentar en la siguiente llamada.
    this.ready.catch(() => { this.ready = null })
    return this.ready
  }

  private async call<S extends DescMessage>(method: string, request: unknown, schema: S): Promise<MessageShape<S>> {
    await this.load()
    const out = JSON.parse(scope.haroCore!.call(method, JSON.stringify(request))) as { result?: unknown; error?: { code: string; message: string } }
    if (out.error) throw Object.assign(new Error(out.error.message), { code: out.error.code })
    return fromJson(schema, out.result as never, { ignoreUnknownFields: true })
  }

  async capabilities(): Promise<Capabilities> {
    return { version: 'wasm', paths: [], canRead: false, canWrite: false, canCheck: false, haproxyBin: '', canAssist: false, assistantModel: '' }
  }

  async analyze(files: SourceFile[], lang: Lang = 'es') { return (await this.call('analyze', { files, lang }, AnalysisSchema)) as unknown as Analysis }
  async apply(files: SourceFile[], ops: Op[]) { return (await this.call('apply', { files, ops }, ApplyResponseSchema)) as unknown as ApplyResult }
  async diff(before: string, after: string) { return (await this.call('diff', { before, after }, DiffResponseSchema)).ops as unknown as DiffOp[] }
  async knowledge(lang: Lang = 'es') { return (await this.call('knowledge', { lang }, KnowledgeSchema)) as unknown as Knowledge }

  async templates(lang: Lang = 'es') {
    const res = await this.call('listTemplates', { lang }, ListTemplatesResponseSchema)
    return res.templates.map(t => ({
      ...t,
      params: t.params.map(p => ({ ...p, defaultValue: p.defaultValue ? toJson(ValueSchema, p.defaultValue) : undefined })),
    })) as unknown as TechTemplate[]
  }

  async renderTemplate(files: SourceFile[], id: string, params: Record<string, ParamValue>, lang: Lang = 'es') {
    return (await this.call('renderTemplate', { files, id, params, lang }, RenderTemplateResponseSchema)) as unknown as RenderResult
  }

  // El asistente necesita el servidor local (la clave de la API nunca debe estar en el navegador).
  async *assist(_req: AssistRequest, _signal?: AbortSignal): AsyncGenerator<AssistEvent> {
    throw new Error('El asistente no está disponible en la versión sin servidor: inicia haro serve --assistant')
  }

  // Sin haproxy ni disco en el navegador.
  async check(): Promise<CheckResult> { return { available: false, ok: false, output: '' } }
  async getConfig(): Promise<SourceFile[] | null> { return null }
  async putFile(): Promise<PutResult> { return { saved: false, error: 'Guardar en disco no está disponible en la versión sin servidor', status: 400 } }
  async putFiles(): Promise<PutResult> { return this.putFile() }
}
