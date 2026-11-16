import type { ParamValue, TechTemplate, TemplateParam } from '../core/types'

// Estado y conversión del formulario de plantillas por tecnología. El
// formulario se genera a partir de los metadatos del núcleo (TechTemplate);
// aquí no hay nada específico de ninguna tecnología.

/** Texto para string, int y list (un elemento por línea); booleano para bool. */
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

/** Un parámetro con `dependsOn` solo se muestra (y se envía) si ese bool está activado. */
export function isVisible(p: TemplateParam, values: FormValues): boolean {
  return !p.dependsOn || values[p.dependsOn] === true
}

/** Elementos de una lista escrita con un elemento por línea (también admite comas). */
export function splitList(text: string): string[] {
  return text.split(/[\n,;]+/).map(x => x.trim()).filter(Boolean)
}

/** Claves de los parámetros obligatorios visibles que están vacíos. */
export function missingRequired(t: TechTemplate, values: FormValues): string[] {
  return t.params.filter(p => {
    if (!p.required || !isVisible(p, values)) return false
    const v = values[p.key]
    if (p.type === 'list') return splitList(String(v ?? '')).length === 0
    return typeof v === 'string' ? v.trim() === '' : false
  }).map(p => p.key)
}

/**
 * Parámetros para el núcleo, con su tipo. Los vacíos se omiten (así se aplican
 * los valores por defecto) y los ocultos no se envían.
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
        if (s) out[p.key] = /^-?\d+$/.test(s) ? Number(s) : s // un texto no numérico lo rechaza el núcleo con su mensaje
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
