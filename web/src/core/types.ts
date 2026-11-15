// Tipos del núcleo. Reflejan proto/haro/v1/haro.proto; cuando se active
// Connect, este archivo se sustituye por el código generado con buf.

export type Severity = 'error' | 'warn' | 'info'

export interface SourceFile { path: string; text: string }

export interface Line {
  file: number
  kind: 'blank' | 'comment' | 'directive'
  line: number
  indent?: string
  text?: string
  keyword?: string
  args?: string[]
  comment?: string
}

export interface Section {
  file: number
  type: string
  name?: string
  line: number
  indent?: string
  text: string
  tokens: string[]
  comment?: string
  lines: Line[]
}

export interface Doc { pre: Line[]; sections: Section[]; files?: string[] }
export interface Issue { file: number; rule: string; severity: Severity; line: number; section: number; message: string }
export interface Explanation { text: string; category: string; known: boolean }
export interface LineExplanation { file: number; line: number; section: number; keyword: string; args: string; explanation: Explanation }
export interface Effective { mode: string; modeFrom: 'own' | 'defaults' | 'implicit'; timeouts: Record<string, boolean>; defaults: number }
export interface Bind { address: string; tls: boolean }
export interface ServerNode { name: string; address: string; backup?: boolean; check?: boolean; disabled?: boolean; template?: boolean }
export interface EntryNode { section: number; type: string; name: string; binds: Bind[]; servers?: ServerNode[] }
export interface BackendNode { section: number; name: string; balance: string; servers: ServerNode[] }
export interface Edge { file: number; from: number; to: number; toName: string; default: boolean; condition?: string; line: number }
export interface Topology { entries: EntryNode[]; backends: BackendNode[]; edges: Edge[]; missing: string[] }

export interface Analysis {
  doc: Doc
  issues: Issue[]
  explanations: LineExplanation[]
  effective: Effective[]
  topology: Topology
}

export interface DiffOp { op: '=' | '-' | '+'; text: string }
export interface CheckResult { available: boolean; ok: boolean; output: string }
export interface Capabilities { version: string; paths?: string[]; canRead: boolean; canWrite: boolean; canCheck: boolean; haproxyBin?: string }

export interface Op {
  op: 'add_section' | 'remove_section' | 'move_section' | 'rename_section' | 'add_directive' | 'update_directive' | 'remove_directive' | 'move_directive'
  section?: number
  type?: string
  name?: string
  template?: boolean
  file?: number
  at?: number
  to?: number
  text?: string
  lineIndex?: number
  delta?: number
  updateRefs?: boolean
}
export interface FileChange { file: number; path: string; text: string }
export interface ApplyResult { files: SourceFile[]; changes: FileChange[] }
