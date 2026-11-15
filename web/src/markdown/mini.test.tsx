import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { parseMini, plainMini, Rich } from './mini'

describe('markdown mínimo', () => {
  it('interpreta negrita y código', () => {
    expect(parseMini('Envía al backend **x** — si `foo`')).toEqual([
      { t: 'text', v: 'Envía al backend ' }, { t: 'bold', v: [{ t: 'text', v: 'x' }] },
      { t: 'text', v: ' — si ' }, { t: 'code', v: 'foo' },
    ])
  })
  it('renderiza con elementos y escapa HTML', () => {
    const html = renderToStaticMarkup(<Rich text={'**a** `<img src=x onerror=1>` <b>x</b>'} />)
    expect(html).toBe('<b>a</b> <code>&lt;img src=x onerror=1&gt;</code> &lt;b&gt;x&lt;/b&gt;')
  })
  it('deja intacto lo que no es marcado', () => {
    expect(renderToStaticMarkup(<Rich text="a * b ** c ` d" />)).toBe('a * b ** c ` d')
  })
  it('permite código dentro de negrita', () => {
    expect(renderToStaticMarkup(<Rich text="**usa `x`**" />)).toBe('<b>usa <code>x</code></b>')
  })
  it('plainMini quita el formato', () => { expect(plainMini('**a** `b`')).toBe('a b') })
})
