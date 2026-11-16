// Tipos del núcleo. Salen del contrato (proto/haro/v1/haro.proto) a través del
// código generado con buf (src/gen); aquí solo se convierten en objetos planos
// (sin $typeName) para que la interfaz pueda crearlos con literales, y se
// precisan las cadenas que en el contrato son enumeraciones.
import type { JsonValue, Message } from '@bufbuild/protobuf'
import type * as pb from '../gen/haro/v1/haro_pb'

type Meta = '$typeName' | '$unknown'
/** Mensaje protobuf → objeto plano, recursivamente. */
export type Plain<T> = T extends Message
  ? { [K in keyof Omit<T, Meta>]: Plain<T[K]> }
  : T extends readonly (infer U)[]
    ? Plain<U>[]
    : T extends object
      ? { [K in keyof T]: Plain<T[K]> }
      : T

export type Severity = 'error' | 'warn' | 'info'

/** Idioma de los textos que produce el núcleo (explicaciones y mensajes del detector). */
export type Lang = 'es' | 'en'
export const LANGS: { value: Lang; label: string }[] = [{ value: 'es', label: 'Español' }, { value: 'en', label: 'English' }]

/** Idioma por defecto: el del navegador si es inglés; si no, español (la interfaz está en español). */
export function browserLang(): Lang {
  try {
    if (typeof navigator !== 'undefined' && /^en\b/i.test(navigator.language || '')) return 'en'
  } catch { /* sin navigator */ }
  return 'es'
}

export type SourceFile = Plain<pb.SourceFile>
export type Line = Omit<Plain<pb.Line>, 'kind'> & { kind: 'blank' | 'comment' | 'directive' }
export type Section = Omit<Plain<pb.Section>, 'lines'> & { lines: Line[] }
export type Doc = Omit<Plain<pb.Doc>, 'pre' | 'sections'> & { pre: Line[]; sections: Section[] }
export type Issue = Omit<Plain<pb.Issue>, 'severity'> & { severity: Severity }
export type Explanation = Plain<pb.Explanation>
export type LineExplanation = Omit<Plain<pb.LineExplanation>, 'explanation'> & { explanation: Explanation }
export type Effective = Omit<Plain<pb.Effective>, 'modeFrom'> & { modeFrom: 'own' | 'defaults' | 'implicit' }
export type Bind = Plain<pb.Bind>
export type ServerNode = Plain<pb.ServerNode>
export type EntryNode = Plain<pb.EntryNode>
export type BackendNode = Plain<pb.BackendNode>
export type Edge = Plain<pb.Edge>
export type Topology = Plain<pb.Topology>

/** El núcleo siempre rellena doc y topology; el contrato los marca opcionales por ser mensajes. */
export type Analysis = Omit<Plain<pb.Analysis>, 'doc' | 'topology' | 'issues' | 'explanations' | 'effective'> & {
  doc: Doc
  topology: Topology
  issues: Issue[]
  explanations: LineExplanation[]
  effective: Effective[]
}

export type DiffOp = Omit<Plain<pb.DiffOp>, 'op'> & { op: '=' | '-' | '+' }
export type CheckResult = Plain<pb.CheckResult>
export type Capabilities = Plain<pb.Capabilities>

export type OpName = 'add_section' | 'remove_section' | 'move_section' | 'rename_section' | 'add_directive' | 'update_directive' | 'remove_directive' | 'move_directive'
/** Operación de edición (ver core/edit.go para qué campos usa cada una). */
export type Op = Partial<Omit<Plain<pb.Op>, 'op'>> & { op: OpName }
export type FileChange = Plain<pb.FileChange>
export type ApplyResult = Plain<pb.ApplyResponse>

export type SectionInfo = Omit<Plain<pb.SectionInfo>, 'named'> & { named: 'required' | 'optional' | 'none' }
export type DirectiveInfo = Plain<pb.DirectiveInfo>
export type SectionTemplate = Plain<pb.SectionTemplate>
export type Knowledge = Omit<Plain<pb.Knowledge>, 'sections'> & { sections: Record<string, SectionInfo> }

/**
 * Resultado de guardar. Si la validación rechaza el guardado, `saved` es false,
 * `error` explica por qué y llega `check` (salida de haproxy -c) o `issue`
 * (error del detector cuando no hay haproxy). Si falla por otro motivo (sin
 * permiso de escritura, archivo ajeno…), solo `error` y `status`.
 */
export interface PutResult {
  saved: boolean
  error?: string
  status?: number
  check?: CheckResult
  issue?: Issue
  path?: string
}

/** Valor tipado de un parámetro de plantilla: texto, número, bool o lista de textos (google.protobuf.Value en el contrato). */
export type ParamValue = JsonValue
export type ParamType = 'string' | 'int' | 'list' | 'bool'
export type TemplateParam = Omit<Plain<pb.TemplateParam>, 'type' | 'defaultValue'> & { type: ParamType; defaultValue?: ParamValue }
/** Plantilla completa por tecnología (Nextcloud, PostgreSQL…). No confundir con SectionTemplate. */
export type TechTemplate = Omit<Plain<pb.TechTemplate>, 'kind' | 'params'> & { kind: 'web' | 'tcp'; params: TemplateParam[] }
/** Resultado de renderizar una plantilla: las operaciones y el efecto de aplicarlas (como ApplyResult). */
export type RenderResult = Omit<Plain<pb.RenderTemplateResponse>, 'ops'> & { ops: Op[] }
