import { useMemo } from 'react'
import { Background, MarkerType, ReactFlow, type Edge, type Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { Analysis } from '../core/types'
import { useApp } from '../store'
import { backendId, entryId, layoutTopology, missingId, serverId } from './layout'
import { BackendNode, EntryNode, MissingNode, ServerNode, TipEdge } from './nodes'

const nodeTypes = { entry: EntryNode, backend: BackendNode, missing: MissingNode, server: ServerNode }
const edgeTypes = { tip: TipEdge }

const STYLE = {
  def: { stroke: '#38bdf8', strokeWidth: 1.8 },
  cond: { stroke: '#f472b6', strokeWidth: 1.6, strokeDasharray: '5 4' },
  missing: { stroke: '#f87171', strokeWidth: 1.6, strokeDasharray: '3 3' },
  srv: { stroke: '#52525b', strokeWidth: 1.4 },
  backup: { stroke: '#52525b', strokeWidth: 1.4, strokeDasharray: '2 4' },
}

export function MapPanel({ analysis }: { analysis: Analysis }) {
  const open = useApp(s => s.ui.mapOpen)
  const setOpen = useApp(s => s.setMapOpen)
  const t = analysis.topology
  const empty = !t.entries.length && !t.backends.length
  return (
    <details className="map" open={open} onToggle={e => setOpen(e.currentTarget.open)} data-testid="map">
      <summary><span>Mapa del tráfico</span><span className="muted sm">cómo fluye una petición: entrada → backend → servidor</span></summary>
      {open && (empty
        ? <p className="muted pad">Añade un frontend y un backend para ver el flujo del tráfico.</p>
        : <><MapFlow analysis={analysis} /><Legend /></>)}
    </details>
  )
}

function Legend() {
  return (
    <div className="legend">
      <span><i className="lg solid" />backend por defecto</span>
      <span><i className="lg dash" />regla condicional (la condición, al pasar el ratón)</span>
      <span><i className="lg srvl" />servidor</span>
      <span><i className="lg miss" />backend inexistente</span>
      <span><span className="dot ok" /> con health check</span>
    </div>
  )
}

function MapFlow({ analysis }: { analysis: Analysis }) {
  const knowledge = useApp(s => s.knowledge)
  const goto = useApp(s => s.goto)
  const setMode = useApp(s => s.setMode)
  const t = analysis.topology

  const { nodes, edges, height } = useMemo(() => {
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
      const tip = e.to < 0 ? `El backend «${e.toName}» no existe` : e.default ? 'Backend por defecto' : `Si ${e.condition}`
      edges.push({
        id: `e${k}`, source: entryId(e.from), target, type: 'tip', data: { tip, file: e.file, line: e.line },
        style: STYLE[kind], markerEnd: { type: MarkerType.ArrowClosed, color: STYLE[kind].stroke, width: 16, height: 16 },
      })
    })
    const serverEdges = (section: number, source: string, servers: { backup?: boolean }[]) =>
      servers.forEach((v, i) => {
        if (!ids.has(serverId(section, i))) return
        edges.push({ id: `sv${section}-${i}`, source, target: serverId(section, i), type: 'tip', style: v.backup ? STYLE.backup : STYLE.srv, data: { tip: v.backup ? 'Servidor de respaldo' : 'Servidor' } })
      })
    t.backends.forEach(b => serverEdges(b.section, backendId(b.section), b.servers))
    t.entries.forEach(e => serverEdges(e.section, entryId(e.section), e.servers ?? []))
    return { nodes, edges, height: Math.min(Math.max(placed.height + 40, 220), 560) }
  }, [t, knowledge])

  const toSection = (section: number) => {
    const s = analysis.doc.sections[section]
    if (!s) return
    setMode('visual')
    goto(s.file, s.line)
  }

  return (
    <div className="map-flow" style={{ height }} data-testid="map-flow">
      <ReactFlow
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes}
        nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}
        fitView fitViewOptions={{ padding: 0.12, maxZoom: 1 }} minZoom={0.3} proOptions={{ hideAttribution: false }}
        colorMode="dark" panOnScroll={false} zoomOnScroll={false} preventScrolling={false}
        onNodeClick={(_, n) => {
          const sec = n.id.startsWith('m:') ? -1 : Number(/^[sv](\d+)/.exec(n.id)?.[1])
          if (sec >= 0) toSection(sec)
        }}
        onEdgeClick={(_, e) => {
          const d = e.data as { file?: number; line?: number } | undefined
          if (d?.file !== undefined && d.line) { setMode('visual'); goto(d.file, d.line) }
        }}
      >
        <Background gap={24} color="#1f1f23" />
      </ReactFlow>
    </div>
  )
}
