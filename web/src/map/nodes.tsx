import { Handle, Position, type NodeProps, type Node, BaseEdge, getBezierPath, type EdgeProps, type Edge } from '@xyflow/react'

export interface EntryData extends Record<string, unknown> { type: string; name: string; binds: { address: string; tls: boolean }[]; color: string }
export interface BackendData extends Record<string, unknown> { name: string; balance: string; incoming: { def: boolean; condition?: string }[] }
export interface MissingData extends Record<string, unknown> { name: string }
export interface ServerData extends Record<string, unknown> { name: string; address: string; backup?: boolean; check?: boolean; disabled?: boolean }

const handles = (left: boolean, right: boolean) => <>
  {left && <Handle type="target" position={Position.Left} isConnectable={false} />}
  {right && <Handle type="source" position={Position.Right} isConnectable={false} />}
</>

export function EntryNode({ data }: NodeProps<Node<EntryData>>) {
  return (
    <div className="node fe" style={{ '--c': data.color } as React.CSSProperties} data-testid="map-entry">
      {handles(true, true)}
      <div className="node-t"><span className="mini">{data.type}</span> {data.name}</div>
      <div className="node-s">
        {data.binds.length
          ? data.binds.map((b, i) => <span key={i}>{i > 0 && ', '}{b.address}{b.tls && <span title="TLS"> 🔒</span>}</span>)
          : <span className="warn">sin bind</span>}
      </div>
    </div>
  )
}

export function BackendNode({ data }: NodeProps<Node<BackendData>>) {
  return (
    <div className="node be" data-testid="map-backend">
      {handles(true, true)}
      <div className="node-t">{data.name}</div>
      <div className="node-s">{data.balance}</div>
      <div className="node-c">
        {data.incoming.map((e, i) => e.def ? <span key={i} className="def">por defecto</span> : <span key={i} className="cnd">si {e.condition}</span>)}
        {!data.incoming.length && <span className="muted">sin tráfico entrante</span>}
      </div>
    </div>
  )
}

export function MissingNode({ data }: NodeProps<Node<MissingData>>) {
  return (
    <div className="node missing" data-testid="map-missing">
      {handles(true, false)}
      <div className="node-t">{data.name}</div>
      <div className="node-s">⚠ no existe</div>
    </div>
  )
}

export function ServerNode({ data }: NodeProps<Node<ServerData>>) {
  return (
    <div className={`node srv ${data.backup ? 'backup' : ''} ${data.disabled ? 'disabled' : ''}`} data-testid="map-server">
      {handles(true, false)}
      <div className="node-t">{data.name}{data.check && <span className="dot ok" title="health check" />}</div>
      <div className="node-s">{data.address}{data.backup ? ' · respaldo' : ''}{data.disabled ? ' · deshabilitado' : ''}</div>
    </div>
  )
}

export interface TipEdgeData extends Record<string, unknown> { tip?: string }

// Arista con tooltip (<title> de SVG) sobre una banda ancha, fácil de apuntar.
export function TipEdge(p: EdgeProps<Edge<TipEdgeData>>) {
  const [path] = getBezierPath(p)
  return (
    <g>
      <BaseEdge id={p.id} path={path} style={p.style} markerEnd={p.markerEnd} />
      <path d={path} fill="none" stroke="transparent" strokeWidth={14} style={{ pointerEvents: 'stroke' }}>
        {p.data?.tip && <title>{p.data.tip}</title>}
      </path>
    </g>
  )
}
