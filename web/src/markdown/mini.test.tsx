import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { parseMini, plainMini, Rich } from './mini'

describe('minimal markdown', () => {
  it('interprets bold and code', () => {
    expect(parseMini('Send to backend **x** — if `foo`')).toEqual([
      { t: 'text', v: 'Send to backend ' }, { t: 'bold', v: [{ t: 'text', v: 'x' }] },
      { t: 'text', v: ' — if ' }, { t: 'code', v: 'foo' },
    ])
  })
  it('renders with elements and escapes HTML', () => {
    const html = renderToStaticMarkup(<Rich text={'**a** `<img src=x onerror=1>` <b>x</b>'} />)
    expect(html).toBe('<b>a</b> <code>&lt;img src=x onerror=1&gt;</code> &lt;b&gt;x&lt;/b&gt;')
  })
  it('leaves non-markup untouched', () => {
    expect(renderToStaticMarkup(<Rich text="a * b ** c ` d" />)).toBe('a * b ** c ` d')
  })
  it('allows code inside bold', () => {
    expect(renderToStaticMarkup(<Rich text="**use `x`**" />)).toBe('<b>use <code>x</code></b>')
  })
  it('plainMini strips the formatting', () => { expect(plainMini('**a** `b`')).toBe('a b') })
})
