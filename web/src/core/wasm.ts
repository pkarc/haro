import { fromJson, toJson, type DescMessage, type MessageShape } from '@bufbuild/protobuf'
import { ValueSchema } from '@bufbuild/protobuf/wkt'
import { AnalysisSchema, ApplyResponseSchema, DiffResponseSchema, KnowledgeSchema, ListTemplatesResponseSchema, RenderTemplateResponseSchema } from '../gen/haro/v1/haro_pb'
import { tr } from '../i18n'
import type { CoreClient } from './client'
import type { Analysis, ApplyResult, AssistEvent, AssistRequest, Capabilities, CheckResult, DiffOp, Knowledge, Lang, Op, ParamValue, ProposeDirectivesResult, PutResult, RenderResult, SourceFile, TechTemplate } from './types'

// WasmCoreClient: the same Go core compiled to WebAssembly (see wasm/ and
// `make wasm`). For the serverless public web: the configuration never leaves
// the browser. There is no disk or haproxy: files cannot be read or saved (the
// web uses browser storage) and haproxy -c cannot be run.
//
// The module speaks JSON with the same field names as the proto contract; here
// it is validated and completed with the generated code (fromJson), so the UI
// receives exactly the same as with ConnectCoreClient.

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
    el.onerror = () => reject(new Error(tr('wasm.loadFailed', { src })))
    document.head.appendChild(el)
  })
}

export class WasmCoreClient implements CoreClient {
  private ready: Promise<void> | null = null

  /** `base` is the URL of the directory with haro.wasm and wasm_exec.js. */
  constructor(private base = `${import.meta.env.BASE_URL}wasm/`) {}

  private load(): Promise<void> {
    this.ready ??= (async () => {
      if (!scope.Go) await loadScript(`${this.base}wasm_exec.js`)
      const go = new scope.Go()
      const { instance } = await WebAssembly.instantiateStreaming(fetch(`${this.base}haro.wasm`), go.importObject)
      void go.run(instance) // never ends: the module serves calls until the page is closed
      for (let i = 0; i < 100 && !scope.haroCore; i++) await new Promise(r => setTimeout(r, 20))
      if (!scope.haroCore) throw new Error(tr('wasm.notStarted'))
    })()
    // If loading fails, allow a retry on the next call.
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

  async analyze(files: SourceFile[], lang: Lang = 'en') { return (await this.call('analyze', { files, lang }, AnalysisSchema)) as unknown as Analysis }
  async apply(files: SourceFile[], ops: Op[]) { return (await this.call('apply', { files, ops }, ApplyResponseSchema)) as unknown as ApplyResult }
  async diff(before: string, after: string) { return (await this.call('diff', { before, after }, DiffResponseSchema)).ops as unknown as DiffOp[] }
  async knowledge(lang: Lang = 'en') { return (await this.call('knowledge', { lang }, KnowledgeSchema)) as unknown as Knowledge }

  async templates(lang: Lang = 'en') {
    const res = await this.call('listTemplates', { lang }, ListTemplatesResponseSchema)
    return res.templates.map(t => ({
      ...t,
      params: t.params.map(p => ({ ...p, defaultValue: p.defaultValue ? toJson(ValueSchema, p.defaultValue) : undefined })),
    })) as unknown as TechTemplate[]
  }

  async renderTemplate(files: SourceFile[], id: string, params: Record<string, ParamValue>, lang: Lang = 'en') {
    return (await this.call('renderTemplate', { files, id, params, lang }, RenderTemplateResponseSchema)) as unknown as RenderResult
  }

  // The assistant needs the local server (the API key must never be in the browser).
  async *assist(_req: AssistRequest, _signal?: AbortSignal): AsyncGenerator<AssistEvent> {
    throw new Error(tr('wasm.noAssistant'))
  }

  async proposeDirectives(): Promise<ProposeDirectivesResult> {
    throw new Error(tr('wasm.noAssistant'))
  }

  // No haproxy or disk in the browser.
  async check(): Promise<CheckResult> { return { available: false, ok: false, output: '' } }
  async getConfig(): Promise<SourceFile[] | null> { return null }
  async putFile(): Promise<PutResult> { return { saved: false, error: tr('wasm.noDisk'), status: 400 } }
  async putFiles(): Promise<PutResult> { return this.putFile() }
}
