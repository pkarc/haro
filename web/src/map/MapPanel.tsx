import { useMemo } from 'react'
import { Background, Controls, MarkerType, ReactFlow, type Edge, type Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { Analysis } from '../core/types'
import { useT } from '../i18n/react'
import { useApp } from '../store'
import { backendId, entryId, layoutTopology, missingId, serverId } from './layout'
import { BackendNode, EntryNode, MissingNode, ServerNode, TipEdge } from './nodes'

const nodeTypes = { entry: EntryNode, backend: BackendNode, missing: MissingNode, server: ServerNode }
const edgeTypes = { tip: TipEdge }

const STYLE = {
  def: { stroke: '#38bdf8', strokeWidth: 1.8 },
  cond: { stroke: '#f472b6', strokeWidth: 1.6, strokeDasharray: '5 4' },
  missing: { stroke: '#f87171', strokeWidth: 1.6, strokeDasharray: '3 3' },
  srv: { stroke: '#475569', strokeWidth: 1.4 },
  backup: { stroke: '#475569', strokeWidth: 1.4, strokeDasharray: '2 4' },
}

/** The map as a page of its own: the flow fills the area, the legend sits below. */
export function MapPage() {
  const tr = useT()
  const analysis = useApp(s => s.analysis)
  if (!analysis) return <div className="map-page"><p className="muted pad">{tr('visual.analyzing')}</p></div>
  const topo = analysis.topology
  const empty = !topo.entries.length && !topo.backends.length
  return (
    <div className="map-page" data-testid="map">
      {empty
        ? <p className="muted pad">{tr('map.empty')}</p>
        : <><MapFlow analysis={analysis} /><Legend /></>}
    </div>
  )
}

function Legend() {
  const t = useT()
  return (
    <div className="legend">
      <span><i className="lg solid" />{t('map.legend.default')}</span>
      <span><i className="lg dash" />{t('map.legend.conditional')}</span>
      <span><i className="lg srvl" />{t('map.legend.server')}</span>
      <span><i className="lg miss" />{t('map.legend.missing')}</span>
      <span><span className="dot ok" /> {t('map.legend.check')}</span>
    </div>
  )
}

function MapFlow({ analysis }: { analysis: Analysis }) {
  const tr = useT()
  const knowledge = useApp(s => s.knowledge)
  const goto = useApp(s => s.goto)
  const t = analysis.topology

  const { nodes, edges } = useMemo(() => {
    const placed = layoutTopology(t)
    const nodes: Node[] = placed.nodes.map(p => {
      const base = { id: p.id, type: p.kind, position: { x: p.x, y: p.y }, draggable: false, connectable: false }
      if (p.kind === 'entry') {
        const e = t.entries.find(x => x.section === p.section)!
        return { ...base, data: { type: e.type, name: e.name, binds: e.binds, color: knowledge?.sections[e.type]?.color ?? '#38bdf8' } }
      }
      if (p.kind === 'backend') {
        const b = t.backends.find(x => x.section === p.section)!
        const incoming = t.edges.filter(x => x.to === p.section).map(x => ({ def: x.default, condition: x.condition }))
        return { ...base, data: { name: b.name, balance: b.balance, incoming } }
      }
      if (p.kind === 'missing') return { ...base, data: { name: p.id.slice(2) } }
      const owner = t.backends.find(x => x.section === p.section) ?? t.entries.find(x => x.section === p.section)!
      const sv = (owner.servers ?? [])[p.index!]
      return { ...base, data: { name: sv.name, address: sv.address, backup: sv.backup, check: sv.check, disabled: sv.disabled } }
    })

    const ids = new Set(nodes.map(n => n.id))
    const edges: Edge[] = []
    t.edges.forEach((e, k) => {
      const target = e.to < 0 ? missingId(e.toName) : ids.has(backendId(e.to)) ? backendId(e.to) : entryId(e.to)
      const kind = e.to < 0 ? 'missing' : e.default ? 'def' : 'cond'
      const tip = e.to < 0 ? tr('map.edge.missing', { name: e.toName }) : e.default ? tr('map.edge.default') : tr('map.edge.if', { cond: e.condition })
      edges.push({
        id: `e${k}`, source: entryId(e.from), target, type: 'tip', data: { tip, file: e.file, line: e.line },
        style: STYLE[kind], markerEnd: { type: MarkerType.ArrowClosed, color: STYLE[kind].stroke, width: 16, height: 16 },
      })
    })
    const serverEdges = (section: number, source: string, servers: { backup?: boolean }[]) =>
      servers.forEach((v, i) => {
        if (!ids.has(serverId(section, i))) return
        edges.push({ id: `sv${section}-${i}`, source, target: serverId(section, i), type: 'tip', style: v.backup ? STYLE.backup : STYLE.srv, data: { tip: v.backup ? tr('map.edge.backup') : tr('map.edge.server') } })
      })
    t.backends.forEach(b => serverEdges(b.section, backendId(b.section), b.servers))
    t.entries.forEach(e => serverEdges(e.section, entryId(e.section), e.servers ?? []))
    return { nodes, edges }
  }, [t, knowledge, tr])

  const toSection = (section: number) => {
    const s = analysis.doc.sections[section]
    if (!s) return
    goto(s.file, s.line, { mode: 'visual' })
  }

  return (
    <div className="map-flow" data-testid="map-flow">
      <ReactFlow
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes}
        nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}
        fitView fitViewOptions={{ padding: 0.12, maxZoom: 1 }} minZoom={0.3} proOptions={{ hideAttribution: false }}
        colorMode="dark" panOnScroll={false} zoomOnScroll preventScrolling
        onNodeClick={(_, n) => {
          const sec = n.id.startsWith('m:') ? -1 : Number(/^[sv](\d+)/.exec(n.id)?.[1])
          if (sec >= 0) toSection(sec)
        }}
        onEdgeClick={(_, e) => {
          const d = e.data as { file?: number; line?: number } | undefined
          if (d?.file !== undefined && d.line) goto(d.file, d.line, { mode: 'visual' })
        }}
      >
        <Background gap={24} color="#1d2330" />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}
