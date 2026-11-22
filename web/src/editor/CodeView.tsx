import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useT } from '../i18n/react'
import { useApp } from '../store'
import { useAnalysisIndex } from '../store/selectors'
import { CodeEditor } from './CodeEditor'
import { CodeStatus } from './CodeStatus'
import { FileTabs } from './FileTabs'
import { useMediaQuery } from '../components/useMediaQuery'

export function CodeView() {
  const t = useT()
  const files = useApp(s => s.files)
  const active = useApp(s => s.active)
  const analysis = useApp(s => s.analysis)
  const knowledge = useApp(s => s.knowledge)
  const focus = useApp(s => s.codeFocus)
  const setText = useApp(s => s.setText)
  const clearFocus = useApp(s => s.clearCodeFocus)
  const setCursor = useApp(s => s.setCursor)
  const undo = useApp(s => s.undo)
  const redo = useApp(s => s.redo)
  const index = useAnalysisIndex(analysis)
  // On desktop the visual view sits beside the code and already explains the current directive.
  const mobile = useMediaQuery('(max-width: 899px)')
  const [line, setLine] = useState(1)
  const issues = useMemo(() => analysis?.issues.filter(i => i.file === active) ?? [], [analysis, active])
  const focusHere = useMemo(() => focus && focus.file === active ? { line: focus.line, nonce: focus.nonce } : null, [focus, active])
  // The child editor already consumed the jump request (its effects run first).
  useEffect(() => { if (focusHere) clearFocus() }, [focusHere, clearFocus])
  // Cursor line goes to the store (the visual view mirrors it), debounced so typing stays cheap.
  const cursorTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(cursorTimer.current), [])
  const onCursorLine = useCallback((l: number, user: boolean) => {
    setLine(l)
    clearTimeout(cursorTimer.current)
    if (!user) { setCursor(active, l, false); return }
    cursorTimer.current = setTimeout(() => setCursor(active, l, true), 80)
  }, [active, setCursor])
  const file = files[active]
  if (!file) return null
  return (
    <div className="code-view">
      <FileTabs />
      {/* key: one editor per file, each with its own state; the language is part of the key so the accessible name updates */}
      <div className="editor">
        <CodeEditor
          key={`${active}:${file.path}:${t.lang}`}
          value={file.text}
          knowledge={knowledge}
          label={t('editor.label')}
          issues={issues}
          focus={focusHere}
          onChange={t => setText(active, t, { coalesce: true })}
          onCursorLine={onCursorLine}
          onUndo={undo}
          onRedo={redo}
        />
      </div>
      {mobile && <CodeStatus analysis={analysis} index={index} knowledge={knowledge} file={active} line={line} />}
    </div>
  )
}
