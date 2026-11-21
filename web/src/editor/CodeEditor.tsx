import { useEffect, useRef } from 'react'
import { acceptCompletion, autocompletion, closeBrackets } from '@codemirror/autocomplete'
import { defaultKeymap, indentMore, indentLess } from '@codemirror/commands'
import { forceLinting, linter, lintGutter, lintKeymap, setDiagnostics, type Diagnostic } from '@codemirror/lint'
import { Annotation, EditorState, StateEffect, StateField, type Extension } from '@codemirror/state'
import { Decoration, drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers } from '@codemirror/view'
import type { Issue, Knowledge } from '../core/types'
import { directiveCompletion } from './completion'
import { haproxyLanguage } from './language'
import { headerType } from './syntax'

const External = Annotation.define<boolean>()
// Marks cursor moves made by the app (reveal from the visual view), not by the user.
const Programmatic = Annotation.define<boolean>()

// Highlight of the line the visual view sent the user to. Stored as a document
// position so it follows edits above it, and cleared as soon as the cursor
// leaves that line.
const setTarget = StateEffect.define<number | null>()
const targetLine = Decoration.line({ class: 'cm-haro-target', attributes: { 'data-haro-target': 'true', 'aria-current': 'location' } })
const targetField = StateField.define<{ pos: number | null; deco: ReturnType<typeof Decoration.set> }>({
  create: () => ({ pos: null, deco: Decoration.none }),
  update(v, tr) {
    let pos = v.pos
    for (const e of tr.effects) if (e.is(setTarget)) pos = e.value
    if (pos === null) return v.deco === Decoration.none ? v : { pos: null, deco: Decoration.none }
    if (!tr.effects.some(e => e.is(setTarget))) pos = tr.changes.mapPos(pos, -1)
    const doc = tr.state.doc
    pos = Math.min(pos, doc.length)
    const line = doc.lineAt(pos)
    // The cursor moved to another line: the highlight is gone.
    if (!tr.effects.some(e => e.is(setTarget)) && tr.selection && doc.lineAt(tr.state.selection.main.head).number !== line.number) {
      return { pos: null, deco: Decoration.none }
    }
    return { pos: line.from, deco: Decoration.set([targetLine.range(line.from)]) }
  },
  provide: f => EditorView.decorations.from(f, v => v.deco),
})

const theme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'var(--bg)', color: 'var(--text)', fontSize: 'var(--fs-code)' },
  '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '19px', overflow: 'auto' },
  '.cm-content': { padding: '12px 0 40px', caretColor: 'var(--accent)' },
  '.cm-line': { padding: '0 16px' },
  '.cm-gutters': { backgroundColor: 'var(--panel)', color: 'var(--dim)', border: 'none', borderRight: '1px solid var(--line)' },
  '.cm-gutter.cm-lineNumbers .cm-gutterElement': { padding: '0 8px 0 12px', minWidth: '36px' },
  '.cm-line.cm-haro-target': { backgroundColor: 'rgba(94,234,212,.09)', borderLeft: '3px solid var(--accent)', paddingLeft: '13px' },
  '.cm-activeLine': { backgroundColor: 'rgba(255,255,255,.03)' },
  '.cm-activeLineGutter': { backgroundColor: 'rgba(255,255,255,.04)', color: 'var(--text)' },
  '&.cm-focused': { outline: 'none' },
  '&.cm-focused .cm-cursor': { borderLeftColor: 'var(--accent)' },
  '.cm-selectionBackground, &.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground': { backgroundColor: 'rgba(94,234,212,.22)' },
  '.cm-tooltip': { backgroundColor: 'var(--panel3)', border: '1px solid var(--line2)', color: 'var(--text)', borderRadius: 'var(--r)' },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'rgba(94,234,212,.15)', color: 'var(--text)' },
  '.cm-completionDetail': { color: 'var(--muted)', fontStyle: 'normal', marginLeft: '8px' },
  '.cm-gutter-lint': { width: '14px' },
}, { dark: true })

export interface CodeEditorProps {
  value: string
  knowledge: Knowledge | null
  /** Accessible name of the editor. */
  label: string
  issues: Issue[]
  /** Line (1-based) to jump to; `nonce` tells repeated requests apart. */
  focus: { line: number; nonce: number } | null
  onChange(text: string): void
  /** `user` is false when the app moved the cursor (reveal, initial state). */
  onCursorLine(line: number, user: boolean): void
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
        // The history is kept by the store (a single Ctrl+Z for code and visual modes).
        { key: 'Mod-z', run: () => { p().onUndo(); return true } },
        { key: 'Mod-y', run: () => { p().onRedo(); return true } },
        { key: 'Mod-Shift-z', run: () => { p().onRedo(); return true } },
        { key: 'Tab', run: v => acceptCompletion(v) || insertTab(v), shift: indentLess },
        { key: 'Enter', run: v => enter(v, kn) },
        ...lintKeymap, ...defaultKeymap,
      ]),
      theme,
      targetField,
      EditorView.contentAttributes.of({ 'aria-label': props.label, spellcheck: 'false', autocapitalize: 'off', autocorrect: 'off' }),
      EditorView.updateListener.of(u => {
        if (u.docChanged && !u.transactions.some(t => t.annotation(External))) p().onChange(u.state.doc.toString())
        if (u.selectionSet || u.docChanged) {
          const prog = u.transactions.some(t => t.annotation(Programmatic) || t.annotation(External))
          p().onCursorLine(u.state.doc.lineAt(u.state.selection.main.head).number, !prog)
        }
      }),
    ]
    const v = new EditorView({ parent: host.current!, state: EditorState.create({ doc: props.value, extensions }) })
    view.current = v
    props.onCursorLine(1, false)
    return () => { v.destroy(); view.current = null }
    // The editor is created once per mount (the parent remounts it when the language changes);
    // the rest arrives through refs and effects.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // External changes (undo, visual mode, import…): only the differing chunk is replaced.
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

  // New analysis: push the diagnostics straight in (the editor can mount before the first
  // analysis arrives, and a forced lint run alone did not pick those up).
  useEffect(() => {
    const v = view.current
    if (!v) return
    v.dispatch(setDiagnostics(v.state, props.issues.map(i => toDiagnostic(v, i)).filter((d): d is Diagnostic => d !== null)))
    forceLinting(v)
  }, [props.issues])

  useEffect(() => {
    const v = view.current
    if (!v || !props.focus) return
    const n = Math.min(Math.max(props.focus.line, 1), v.state.doc.lines)
    const l = v.state.doc.line(n)
    v.dispatch({
      selection: { anchor: l.from, head: l.to },
      effects: [EditorView.scrollIntoView(l.from, { y: 'center' }), setTarget.of(l.from)],
      annotations: Programmatic.of(true),
    })
    v.focus()
  }, [props.focus])

  return <div className="code-host" ref={host} data-testid="code-editor" />
}

function toDiagnostic(v: EditorView, i: Issue): Diagnostic | null {
  if (i.line < 1 || i.line > v.state.doc.lines) return null // analysis slightly behind the text
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

// Enter keeps the indentation; after a section header it indents 4 spaces.
function enter(v: EditorView, kn: Knowledge | null): boolean {
  const { state } = v
  const sel = state.selection.main
  const line = state.doc.lineAt(sel.from)
  let ind = /^[ \t]*/.exec(line.text.slice(0, sel.from - line.from))![0]
  if (headerType(line.text, kn)) ind = '    '
  v.dispatch(state.update(state.replaceSelection('\n' + ind), { scrollIntoView: true, userEvent: 'input' }))
  return true
}
