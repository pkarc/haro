import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { setActiveLang } from '../i18n'
import { ChatMarkdown, parseChat } from './chat'

setActiveLang('en') // outside an app, texts follow the active language

describe('chat markdown', () => {
  it('interprets paragraphs, lists, headings and code blocks', () => {
    const html = renderToStaticMarkup(<ChatMarkdown text={'## Changes\n\nPut **two** things:\n\n- use `a`\n- use `b`\n\n1. one\n2. two\n\n```bash\nhaproxy -c -f x\n```'} />)
    expect(html).toBe('<p class="chat-h"><b>Changes</b></p><p>Put <b>two</b> things:</p><ul><li>use <code>a</code></li><li>use <code>b</code></li></ul><ol><li>one</li><li>two</li></ol><pre class="chat-code" data-lang="bash"><code>haproxy -c -f x</code></pre>')
  })
  it('does not render complete configuration blocks', () => {
    const html = renderToStaticMarkup(<ChatMarkdown text={'Done.\n\n```haproxy path=a.cfg\nfrontend web\n    bind *:80\n```\n\nEnd.'} />)
    expect(html).not.toContain('frontend web')
    expect(html).toContain('data-testid="config-block"')
    expect(html).toContain('2 lines')
    expect(html).toContain('<p>End.</p>')
  })
  it('a block without a language counts as configuration, like in the core', () => {
    expect(parseChat('```\nx\n```')).toEqual([{ t: 'config', lines: 1, open: false }])
    expect(parseChat('```json\n{}\n```')[0]).toMatchObject({ t: 'code', lang: 'json' })
  })
  it('an unclosed block (streaming) is announced without showing its content', () => {
    const open = renderToStaticMarkup(<ChatMarkdown streaming text={'```haproxy\nglobal\n    maxconn 10'} />)
    expect(open).toContain('Writing')
    expect(open).not.toContain('maxconn')
    expect(renderToStaticMarkup(<ChatMarkdown text={'```haproxy\nglobal'} />)).toContain('cut off')
  })
  it('escapes HTML everywhere', () => {
    const evil = '<img src=x onerror=1>'
    const html = renderToStaticMarkup(<ChatMarkdown text={`${evil}\n\n- ${evil}\n\n# ${evil}\n\n\`\`\`bash\n${evil}\n\`\`\``} />)
    expect(html).not.toContain('<img')
    expect(html.match(/&lt;img src=x onerror=1&gt;/g)).toHaveLength(4)
  })
  it('an open code block is shown as is while it arrives', () => {
    expect(parseChat('```bash\nls')).toEqual([{ t: 'code', lang: 'bash', v: 'ls', open: true }])
  })
})
