import type { FileChange, Lang, SourceFile } from '../core/types'
import { translate } from '../i18n'

// Pure functions over the list of files (no state), easy to test.

/** Applies the `changes` returned by apply. Throws if they do not match the files. */
export function applyChanges(files: SourceFile[], changes: FileChange[], lang: Lang = 'en'): SourceFile[] {
  const out = files.slice()
  for (const c of changes) {
    const cur = out[c.file]
    if (!cur || cur.path !== c.path) {
      throw new Error(translate(lang, 'state.changeMismatch', { path: c.path, file: c.file }))
    }
    out[c.file] = { path: cur.path, text: c.text }
  }
  return out
}

export function sameFiles(a: SourceFile[], b: SourceFile[]): boolean {
  return a.length === b.length && a.every((f, i) => f.path === b[i].path && f.text === b[i].text)
}

/** Indexes of the files whose text differs from what was read from disk. */
export function dirtyIndexes(files: SourceFile[], disk: SourceFile[] | null): number[] {
  if (!disk) return []
  const byPath = new Map(disk.map(f => [f.path, f.text]))
  const out: number[] = []
  files.forEach((f, i) => { if (byPath.has(f.path) && byPath.get(f.path) !== f.text) out.push(i) })
  return out
}

export const baseName = (p: string) => p.split('/').pop() || p

/** Short label of each file; uses the full path if the name is repeated. */
export function fileLabels(files: SourceFile[]): string[] {
  const counts = new Map<string, number>()
  files.forEach(f => counts.set(baseName(f.path), (counts.get(baseName(f.path)) ?? 0) + 1))
  return files.map(f => (counts.get(baseName(f.path)) ?? 0) > 1 ? f.path : baseName(f.path))
}
