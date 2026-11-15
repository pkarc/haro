import { useEffect, useRef } from 'react'
import { acceptCompletion, autocompletion, closeBrackets } from '@codemirror/autocomplete'
import { defaultKeymap, indentMore, indentLess } from '@codemirror/commands'
import { forceLinting, linter, lintGutter, lintKeymap, type Diagnostic } from '@codemirror/lint'
import { Annotation, EditorState, type Extension } from '@codemirror/state'
import { drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers } from '@codemirror/view'
import type { Issue, Knowledge } from '../core/types'
import { directiveCompletion } from './completion'
import { haproxyLanguage } from './language'
import { headerType } from './syntax'

const External = Annotation.define<boolean>()

const theme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'var(--bg)', color: 'var(--text)', fontSize: '13px' },
  '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '20px', overflow: 'auto' },
  '.cm-content': { padding: '12px 0 40px', caretColor: 'var(--accent)' },
  '.cm-line': { padding: '0 16px' },
  '.cm-gutters': { backgroundColor: 'var(--panel)', color: 'var(--dim)', border: 'none', borderRight: '1px solid var(--line)' },
  '.cm-gutter.cm-lineNumbers .cm-gutterElement': { padding: '0 8px 0 12px', minWidth: '36px' },
  '.cm-activeLine': { backgroundColor: 'rgba(255,255,255,.03)' },
  '.cm-activeLineGutter': { backgroundColor: 'rgba(255,255,255,.04)', color: 'var(--text)' },
  '&.cm-focused': { outline: 'none' },
  '&.cm-focused .cm-cursor': { borderLeftColor: 'var(--accent)' },
  '.cm-selectionBackground, &.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground': { backgroundColor: 'rgba(94,234,212,.22)' },
  '.cm-tooltip': { backgroundColor: 'var(--panel3)', border: '1px solid var(--line2)', color: 'var(--text)', borderRadius: '8px' },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'rgba(94,234,212,.15)', color: 'var(--text)' },
  '.cm-completionDetail': { color: 'var(--muted)', fontStyle: 'normal', marginLeft: '8px' },
  '.cm-gutter-lint': { width: '14px' },
}, { dark: true })

export interface CodeEditorProps {
  value: string
  knowledge: Knowledge | null
  issues: Issue[]
  /** Línea (1-based) a la que saltar; `nonce` distingue peticiones repetidas. */
  focus: { line: number; nonce: number } | null
  onChange(text: string): void
  onCursorLine(line: number): void
  onUndo(): void
  onRedo(): void
}

export function CodeEditor(props: CodeEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const latest = useRef(props)
  latest.current = props

  useEffect(() => {
    const p = () => latest.current
    const kn = props.knowledge
    const extensions: Extension[] = [
      lineNumbers(), highlightActiveLine(), highlightActiveLineGutter(), drawSelection(), closeBrackets(),
      EditorState.tabSize.of(4),
      haproxyLanguage(kn),
      autocompletion({ override: [directiveCompletion(kn)], icons: false }),
      linter(v => latest.current.issues.map(i => toDiagnostic(v, i)).filter((d): d is Diagnostic => d !== null), { delay: 100 }),
      lintGutter(),
      keymap.of([
        // El historial lo lleva el almacén (un único Ctrl+Z para modo código y visual).
        { key: 'Mod-z', run: () => { p().onUndo(); return true } },
        { key: 'Mod-y', run: () => { p().onRedo(); return true } },
        { key: 'Mod-Shift-z', run: () => { p().onRedo(); return true } },
        { key: 'Tab', run: v => acceptCompletion(v) || insertTab(v), shift: indentLess },
        { key: 'Enter', run: v => enter(v, kn) },
        ...lintKeymap, ...defaultKeymap,
      ]),
      theme,
      EditorView.contentAttributes.of({ 'aria-label': 'Configuración de HAProxy', spellcheck: 'false', autocapitalize: 'off', autocorrect: 'off' }),
      EditorView.updateListener.of(u => {
        if (u.docChanged && !u.transactions.some(t => t.annotation(External))) p().onChange(u.state.doc.toString())
        if (u.selectionSet || u.docChanged) p().onCursorLine(u.state.doc.lineAt(u.state.selection.main.head).number)
      }),
    ]
    const v = new EditorView({ parent: host.current!, state: EditorState.create({ doc: props.value, extensions }) })
    view.current = v
    props.onCursorLine(1)
    return () => { v.destroy(); view.current = null }
    // El editor se crea una vez por montaje; el resto llega por refs y efectos.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Cambios externos (deshacer, modo visual, importar…): se reemplaza solo el trozo distinto.
  useEffect(() => {
    const v = view.current
    if (!v) return
    const cur = v.state.doc.toString()
    if (cur === props.value) return
    let a = 0
    const max = Math.min(cur.length, props.value.length)
    while (a < max && cur[a] === props.value[a]) a++
    let ce = cur.length, ne = props.value.length
    while (ce > a && ne > a && cur[ce - 1] === props.value[ne - 1]) { ce--; ne-- }
    v.dispatch({ changes: { from: a, to: ce, insert: props.value.slice(a, ne) }, annotations: External.of(true) })
  }, [props.value])

  useEffect(() => { if (view.current) forceLinting(view.current) }, [props.issues])

  useEffect(() => {
    const v = view.current
    if (!v || !props.focus) return
    const n = Math.min(Math.max(props.focus.line, 1), v.state.doc.lines)
    const l = v.state.doc.line(n)
    v.dispatch({ selection: { anchor: l.from, head: l.to }, effects: EditorView.scrollIntoView(l.from, { y: 'center' }) })
    v.focus()
  }, [props.focus])

  return <div className="code-host" ref={host} data-testid="code-editor" />
}

function toDiagnostic(v: EditorView, i: Issue): Diagnostic | null {
  if (i.line < 1 || i.line > v.state.doc.lines) return null // análisis algo atrasado respecto al texto
  const l = v.state.doc.line(i.line)
  const from = l.from + (l.text.length - l.text.trimStart().length)
  const to = Math.max(l.to, Math.min(from + 1, v.state.doc.length))
  return { from: Math.min(from, to), to, severity: i.severity === 'warn' ? 'warning' : i.severity, message: i.message, source: i.rule }
}

function insertTab(v: EditorView): boolean {
  const sel = v.state.selection.main
  if (sel.empty) {
    v.dispatch(v.state.replaceSelection('    '), { scrollIntoView: true })
    return true
  }
  return indentMore(v)
}

// Enter conserva la sangría; tras una cabecera de sección sangra 4 espacios.
function enter(v: EditorView, kn: Knowledge | null): boolean {
  const { state } = v
  const sel = state.selection.main
  const line = state.doc.lineAt(sel.from)
  let ind = /^[ \t]*/.exec(line.text.slice(0, sel.from - line.from))![0]
  if (headerType(line.text, kn)) ind = '    '
  v.dispatch(state.update(state.replaceSelection('\n' + ind), { scrollIntoView: true, userEvent: 'input' }))
  return true
}
