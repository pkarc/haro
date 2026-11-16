import type { ParamValue, TechTemplate, TemplateParam } from '../core/types'

// State and conversion of the technology template form. The form is generated
// from the core's metadata (TechTemplate); nothing here is specific to any
// technology.

/** Text for string, int and list (one item per line); boolean for bool. */
export type FormValues = Record<string, string | boolean>

const listText = (v: ParamValue | undefined) => (Array.isArray(v) ? v.join('\n') : '')

export function initialValues(t: TechTemplate): FormValues {
  const values: FormValues = {}
  for (const p of t.params) {
    const d = p.defaultValue
    if (p.type === 'bool') values[p.key] = d === true
    else if (p.type === 'list') values[p.key] = listText(d)
    else values[p.key] = d === undefined || d === null ? '' : String(d)
  }
  return values
}

/** A parameter with `dependsOn` is only shown (and sent) if that bool is enabled. */
export function isVisible(p: TemplateParam, values: FormValues): boolean {
  return !p.dependsOn || values[p.dependsOn] === true
}

/** Items of a list written with one item per line (commas are also accepted). */
export function splitList(text: string): string[] {
  return text.split(/[\n,;]+/).map(x => x.trim()).filter(Boolean)
}

/** Keys of the visible required parameters that are empty. */
export function missingRequired(t: TechTemplate, values: FormValues): string[] {
  return t.params.filter(p => {
    if (!p.required || !isVisible(p, values)) return false
    const v = values[p.key]
    if (p.type === 'list') return splitList(String(v ?? '')).length === 0
    return typeof v === 'string' ? v.trim() === '' : false
  }).map(p => p.key)
}

/**
 * Parameters for the core, with their type. Empty ones are omitted (so the
 * defaults apply) and hidden ones are not sent.
 */
export function toParams(t: TechTemplate, values: FormValues): Record<string, ParamValue> {
  const out: Record<string, ParamValue> = {}
  for (const p of t.params) {
    if (!isVisible(p, values)) continue
    const v = values[p.key]
    switch (p.type) {
      case 'bool':
        out[p.key] = v === true
        break
      case 'list': {
        const items = splitList(String(v ?? ''))
        if (items.length) out[p.key] = items
        break
      }
      case 'int': {
        const s = String(v ?? '').trim()
        if (s) out[p.key] = /^-?\d+$/.test(s) ? Number(s) : s // a non-numeric text is rejected by the core with its message
        break
      }
      default: {
        const s = String(v ?? '').trim()
        if (s) out[p.key] = s
      }
    }
  }
  return out
}
