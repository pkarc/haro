import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ChatMarkdown, parseChat } from './chat'

describe('markdown del chat', () => {
  it('interpreta párrafos, listas, títulos y bloques de código', () => {
    const html = renderToStaticMarkup(<ChatMarkdown text={'## Cambios\n\nPon **dos** cosas:\n\n- usa `a`\n- usa `b`\n\n1. uno\n2. dos\n\n```bash\nhaproxy -c -f x\n```'} />)
    expect(html).toBe('<p class="chat-h"><b>Cambios</b></p><p>Pon <b>dos</b> cosas:</p><ul><li>usa <code>a</code></li><li>usa <code>b</code></li></ul><ol><li>uno</li><li>dos</li></ol><pre class="chat-code" data-lang="bash"><code>haproxy -c -f x</code></pre>')
  })
  it('no pinta los bloques de configuración completa', () => {
    const html = renderToStaticMarkup(<ChatMarkdown text={'Listo.\n\n```haproxy path=a.cfg\nfrontend web\n    bind *:80\n```\n\nFin.'} />)
    expect(html).not.toContain('frontend web')
    expect(html).toContain('data-testid="config-block"')
    expect(html).toContain('2 líneas')
    expect(html).toContain('<p>Fin.</p>')
  })
  it('un bloque sin lenguaje cuenta como configuración, igual que en el núcleo', () => {
    expect(parseChat('```\nx\n```')).toEqual([{ t: 'config', lines: 1, open: false }])
    expect(parseChat('```json\n{}\n```')[0]).toMatchObject({ t: 'code', lang: 'json' })
  })
  it('un bloque sin cerrar (streaming) se anuncia sin mostrar su contenido', () => {
    const open = renderToStaticMarkup(<ChatMarkdown streaming text={'```haproxy\nglobal\n    maxconn 10'} />)
    expect(open).toContain('Escribiendo')
    expect(open).not.toContain('maxconn')
    expect(renderToStaticMarkup(<ChatMarkdown text={'```haproxy\nglobal'} />)).toContain('cortada')
  })
  it('escapa el HTML en todas partes', () => {
    const evil = '<img src=x onerror=1>'
    const html = renderToStaticMarkup(<ChatMarkdown text={`${evil}\n\n- ${evil}\n\n# ${evil}\n\n\`\`\`bash\n${evil}\n\`\`\``} />)
    expect(html).not.toContain('<img')
    expect(html.match(/&lt;img src=x onerror=1&gt;/g)).toHaveLength(4)
  })
  it('un código en bloque abierto se muestra tal cual mientras llega', () => {
    expect(parseChat('```bash\nls')).toEqual([{ t: 'code', lang: 'bash', v: 'ls', open: true }])
  })
})
