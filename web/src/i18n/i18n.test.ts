import { describe, expect, it } from 'vitest'
import { en } from './en'
import { es } from './es'
import { makeT, translate, type Key } from './index'

const keys = (d: Record<string, string>) => Object.keys(d).sort()
const params = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort()

describe('dictionaries', () => {
  it('en and es have exactly the same keys', () => {
    expect(keys(es)).toEqual(keys(en))
  })

  it('no string is empty', () => {
    for (const [lang, d] of Object.entries({ en, es })) {
      for (const [k, v] of Object.entries(d)) expect(v.trim(), `${lang}:${k}`).not.toBe('')
    }
  })

  it('both languages use the same {parameters} in every string', () => {
    for (const k of Object.keys(en) as Key[]) expect(params(es[k]), k).toEqual(params(en[k]))
  })

  it('every plural has both forms and uses {n}', () => {
    for (const k of Object.keys(en)) {
      if (k.endsWith('.one')) expect(en, `${k} without .other`).toHaveProperty(k.replace(/\.one$/, '.other'))
      if (k.endsWith('.other')) expect(en, `${k} without .one`).toHaveProperty(k.replace(/\.other$/, '.one'))
      if (/\.(one|other)$/.test(k)) expect(params(en[k as Key]), k).toContain('n')
    }
  })

  it('Spanish is neutral Latin American: no «vosotros» forms or Spain-only vocabulary', () => {
    const banned = /\b(vosotros|vuestr[oa]s?|ordenador(es)?|vale|fichero(s)?|pinchar|pincha)\b/i
    for (const [k, v] of Object.entries(es)) expect(v, k).not.toMatch(banned)
  })
})

describe('translate', () => {
  it('replaces parameters and keeps unknown placeholders', () => {
    expect(translate('en', 'save.title', { label: 'disk' })).toBe('Save changes to disk')
    expect(translate('es', 'save.title', { label: 'disco' })).toBe('Guardar cambios en disco')
    expect(translate('en', 'save.title')).toBe('Save changes to {label}')
  })

  it('picks the plural form', () => {
    const t = makeT('en')
    expect(t.n('problems.error', 1)).toBe('1 error')
    expect(t.n('problems.error', 0)).toBe('0 errors')
    expect(makeT('es').n('problems.info', 2)).toBe('2 sugerencias')
  })

  it('makeT is stable per language', () => {
    expect(makeT('en')).toBe(makeT('en'))
    expect(makeT('en').lang).toBe('en')
  })
})
