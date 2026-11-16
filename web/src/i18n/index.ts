import type { Lang } from '../core/types'
import { en, type Key } from './en'
import { es } from './es'

export type { Key }
export type Params = Record<string, string | number>

const dicts: Record<Lang, Record<Key, string>> = { en, es }

/** Base of the keys that have `.one` / `.other` forms (see `T.n`). */
export type PluralKey = { [K in Key]: K extends `${infer B}.one` ? B : never }[Key]

/** Translates `key` into `lang`, replacing `{param}` placeholders. Falls back to English. */
export function translate(lang: Lang, key: Key, params?: Params): string {
  const s = dicts[lang]?.[key] ?? en[key]
  return params ? s.replace(/\{(\w+)\}/g, (m, k: string) => (k in params ? String(params[k]) : m)) : s
}

export interface T {
  (key: Key, params?: Params): string
  /** Plural form: picks `<base>.one` when n is 1 and `<base>.other` otherwise; `{n}` is filled in. */
  n(base: PluralKey, n: number, params?: Params): string
  readonly lang: Lang
}

const cache = new Map<Lang, T>()

/** Translator bound to a language (stable per language, safe to use in effect dependencies). */
export function makeT(lang: Lang): T {
  let t = cache.get(lang)
  if (!t) {
    const fn = (key: Key, params?: Params) => translate(lang, key, params)
    t = Object.assign(fn, {
      lang,
      n: (base: PluralKey, n: number, params?: Params) => translate(lang, `${base}.${n === 1 ? 'one' : 'other'}` as Key, { n, ...params }),
    })
    cache.set(lang, t)
  }
  return t
}

// Language for code that runs outside React and outside the store (core client,
// configuration stores). The app store keeps it in sync with the chosen language.
let active: Lang = 'en'
export const getActiveLang = (): Lang => active
export const setActiveLang = (l: Lang) => { active = l }
/** Translator for the active language. */
export const tr = (key: Key, params?: Params): string => translate(active, key, params)
