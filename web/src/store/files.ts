import type { FileChange, SourceFile } from '../core/types'

// Funciones puras sobre la lista de archivos (sin estado), fáciles de probar.

/** Aplica los `changes` devueltos por apply. Lanza si no encajan con los archivos. */
export function applyChanges(files: SourceFile[], changes: FileChange[]): SourceFile[] {
  const out = files.slice()
  for (const c of changes) {
    const cur = out[c.file]
    if (!cur || cur.path !== c.path) {
      throw new Error(`El cambio para «${c.path}» no corresponde al archivo ${c.file}`)
    }
    out[c.file] = { path: cur.path, text: c.text }
  }
  return out
}

export function sameFiles(a: SourceFile[], b: SourceFile[]): boolean {
  return a.length === b.length && a.every((f, i) => f.path === b[i].path && f.text === b[i].text)
}

/** Índices de los archivos cuyo texto difiere del que se leyó de disco. */
export function dirtyIndexes(files: SourceFile[], disk: SourceFile[] | null): number[] {
  if (!disk) return []
  const byPath = new Map(disk.map(f => [f.path, f.text]))
  const out: number[] = []
  files.forEach((f, i) => { if (byPath.has(f.path) && byPath.get(f.path) !== f.text) out.push(i) })
  return out
}

export const baseName = (p: string) => p.split('/').pop() || p

/** Etiqueta corta de cada archivo; usa la ruta completa si el nombre se repite. */
export function fileLabels(files: SourceFile[]): string[] {
  const counts = new Map<string, number>()
  files.forEach(f => counts.set(baseName(f.path), (counts.get(baseName(f.path)) ?? 0) + 1))
  return files.map(f => (counts.get(baseName(f.path)) ?? 0) > 1 ? f.path : baseName(f.path))
}
