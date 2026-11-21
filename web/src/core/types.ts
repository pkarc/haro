// Core types. They come from the contract (proto/haro/v1/haro.proto) through the
// code generated with buf (src/gen); here they are only converted into plain
// objects (no $typeName) so the UI can create them with literals, and the
// strings that are enumerations in the contract are narrowed.
import type { JsonValue, Message } from '@bufbuild/protobuf'
import type * as pb from '../gen/haro/v1/haro_pb'

type Meta = '$typeName' | '$unknown'
/** Protobuf message → plain object, recursively. */
export type Plain<T> = T extends Message
  ? { [K in keyof Omit<T, Meta>]: Plain<T[K]> }
  : T extends readonly (infer U)[]
    ? Plain<U>[]
    : T extends object
      ? { [K in keyof T]: Plain<T[K]> }
      : T

export type Severity = 'error' | 'warn' | 'info'

/** Language of the interface and of the texts produced by the core (explanations and detector messages). */
export type Lang = 'en' | 'es'
// Each language is named in its own language.
export const LANGS: { value: Lang; label: string }[] = [{ value: 'en', label: 'English' }, { value: 'es', label: 'Español' }]

/** Default language: Spanish only if the browser language starts with «es»; English otherwise. */
export function browserLang(): Lang {
  try {
    if (typeof navigator !== 'undefined' && /^es\b/i.test(navigator.language || '')) return 'es'
  } catch { /* no navigator */ }
  return 'en'
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

/** The core always fills in doc and topology; the contract marks them optional because they are messages. */
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
/** Edit operation (see core/edit.go for the fields each one uses). */
export type Op = Partial<Omit<Plain<pb.Op>, 'op'>> & { op: OpName }
export type FileChange = Plain<pb.FileChange>
export type ApplyResult = Plain<pb.ApplyResponse>

export type SectionInfo = Omit<Plain<pb.SectionInfo>, 'named'> & { named: 'required' | 'optional' | 'none' }
export type DirectiveInfo = Plain<pb.DirectiveInfo>
export type SectionTemplate = Plain<pb.SectionTemplate>
export type Knowledge = Omit<Plain<pb.Knowledge>, 'sections'> & { sections: Record<string, SectionInfo> }

/**
 * Result of saving. If validation rejects the save, `saved` is false, `error`
 * explains why and `check` (haproxy -c output) or `issue` (detector error when
 * there is no haproxy) is included. If it fails for another reason (no write
 * permission, foreign file…), only `error` and `status` are set.
 */
export interface PutResult {
  saved: boolean
  error?: string
  status?: number
  check?: CheckResult
  issue?: Issue
  path?: string
}

/** Typed value of a template parameter: text, number, bool or list of texts (google.protobuf.Value in the contract). */
export type ParamValue = JsonValue
export type ParamType = 'string' | 'int' | 'list' | 'bool'
export type TemplateParam = Omit<Plain<pb.TemplateParam>, 'type' | 'defaultValue'> & { type: ParamType; defaultValue?: ParamValue }
/** Complete per-technology template (Nextcloud, PostgreSQL…). Not to be confused with SectionTemplate. */
export type TechTemplate = Omit<Plain<pb.TechTemplate>, 'kind' | 'params'> & { kind: 'web' | 'tcp'; params: TemplateParam[] }
/** Result of rendering a template: the operations and the effect of applying them (like ApplyResult). */
export type RenderResult = Omit<Plain<pb.RenderTemplateResponse>, 'ops'> & { ops: Op[] }

/** What matters for the assistant is already in Capabilities (canAssist, assistantModel). */
export interface AssistMessage { role: 'user' | 'assistant'; content: string }
export interface AssistRequest { files: SourceFile[]; messages: AssistMessage[]; lang: Lang }
/** Assistant proposal: the full text of each file that changes and the problems of the resulting configuration. */
export interface AssistProposal { changes: FileChange[]; issuesAfter: Issue[]; unmatched: number; truncated: boolean }
export interface AssistDone { stopReason: string; refusalCategory: string; inputTokens: number; outputTokens: number }
/** Request for directives of ONE section from a natural-language description. */
export interface ProposeDirectivesRequest { files: SourceFile[]; section: number; instruction: string; lang: Lang }
/** A directive proposed by the assistant; `at` is a line index within the unmodified section (-1 = at the end). */
export interface ProposedDirective { text: string; explanation: string; issuesAfter: Issue[]; known: boolean; at: number }
export interface ProposeDirectivesResult { directives: ProposedDirective[]; note: string; inputTokens: number; outputTokens: number }
/** Assistant stream events: text chunks, the proposal (if any) and the end. */
export type AssistEvent =
  | { type: 'text'; text: string }
  | { type: 'proposal'; proposal: AssistProposal }
  | { type: 'done'; done: AssistDone }
