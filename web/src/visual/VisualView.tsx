import { useEffect, useRef } from 'react'
import { addSectionBox } from '../components/dialogs'
import { useT } from '../i18n/react'
import { templateBox } from '../templates/TemplateDialog'
import { MapPanel } from '../map/MapPanel'
import { useApp } from '../store'
import { fileLabels } from '../store/files'
import { useAnalysisIndex } from '../store/selectors'
import { opAddSection, sectionKey } from './ops'
import { SectionCard } from './SectionCard'

export function VisualView() {
  const t = useT()
  const analysis = useApp(s => s.analysis)
  const knowledge = useApp(s => s.knowledge)
  const files = useApp(s => s.files)
  const stale = useApp(s => s.analysisRev !== s.rev)
  const focus = useApp(s => s.focus)
  const clearFocus = useApp(s => s.clearFocus)
  const setMode = useApp(s => s.setMode)
  const toggleCollapsed = useApp(s => s.toggleCollapsed)
  const applyOps = useApp(s => s.applyOps)
  const idx = useAnalysisIndex(analysis)
  const root = useRef<HTMLDivElement>(null)

  // Jump to a line or section (problems panel, map).
  useEffect(() => {
    if (!focus || !analysis || stale) return
    const el = root.current?.querySelector<HTMLElement>(`[data-file="${focus.file}"][data-line="${focus.line}"]`)
    if (!el) { setMode('code'); return } // lines that visual mode does not render (e.g. blanks)
    const sec = el.closest<HTMLElement>('.sec')
    if (sec?.classList.contains('collapsed')) {
      const s = analysis.doc.sections[Number(sec.dataset.section)]
      if (s) toggleCollapsed(`${s.file}:${s.type}:${s.name ?? ''}`)
    }
    requestAnimationFrame(() => {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('flash')
      setTimeout(() => el.classList.remove('flash'), 1400)
    })
    clearFocus()
  }, [focus, analysis, stale, clearFocus, setMode, toggleCollapsed])

  if (!analysis || !knowledge) return <div className="visual-view"><p className="muted pad">{t('visual.analyzing')}</p></div>

  const labels = fileLabels(files)
  const sections = analysis.doc.sections
  const addable = Object.keys(knowledge.templates).filter(k => k in knowledge.sections)
  const types = [...new Set(sections.map(s => s.type))]
  // Key that is stable when line numbering changes (keeps focus and local state).
  const seen = new Map<string, number>()
  const keys = sections.map(s => { const k = sectionKey(s), n = seen.get(k) ?? 0; seen.set(k, n + 1); return `${k}#${n}` })
  const pre = analysis.doc.pre.filter(l => l.kind !== 'blank')

  const add = async (type: string) => {
    const named = knowledge.sections[type].named !== 'none'
    let name = '', file: number | undefined
    if (files.length > 1 || named) {
      const r = await addSectionBox(type, named, files.map(f => f.path), files.length - 1)
      if (!r) return
      name = r.name
      file = files.length > 1 ? r.file : undefined
    }
    await applyOps([opAddSection(type, name, file)])
  }

  return (
    <div className={`visual-view${stale ? ' stale' : ''}`} ref={root} data-testid="visual-view">
      <div className="v-toolbar">
        <span className="muted">{t('visual.addSection')}</span>
        {addable.map(k => (
          <button key={k} className="chip" style={{ '--c': knowledge.sections[k].color } as React.CSSProperties} onClick={() => add(k)}>+ {k}</button>
        ))}
        <button className="chip tpl-btn" data-testid="template-button" onClick={() => void templateBox()}>{t('visual.fromTemplate')}</button>
      </div>
      <MapPanel analysis={analysis} />
      {pre.length > 0 && (
        <div className="pre-notes">
          {pre.map(l => l.kind === 'comment'
            ? <div key={`${l.file}:${l.line}`} className="cmt" data-file={l.file} data-line={l.line}>{l.text}</div>
            : <div key={`${l.file}:${l.line}`} className="iss sev-error" data-file={l.file} data-line={l.line}><span>{t('common.error')}</span> <code>{l.text}</code> {t('visual.outside')}</div>)}
        </div>
      )}
      {sections.map((s, si) => (
        <SectionCard
          key={keys[si]}
          section={s} index={si} total={sections.length} analysis={analysis} idx={idx} knowledge={knowledge}
          showFile={files.length > 1} fileLabel={labels[s.file] ?? ''}
        />
      ))}
      {!sections.length && (
        <div className="empty"><p>{t('visual.empty')}</p><p className="muted">{t('visual.emptyHint')}</p></div>
      )}
      {types.map(k => (
        <datalist key={k} id={`dl-${k}`}>
          {Object.entries(knowledge.directives).filter(([, d]) => d.sections.includes(k)).map(([name]) => <option key={name} value={name} />)}
        </datalist>
      ))}
    </div>
  )
}
