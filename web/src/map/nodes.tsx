import { useT } from '../i18n/react'
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
  const t = useT()
  return (
    <div className="node fe" style={{ '--c': data.color } as React.CSSProperties} data-testid="map-entry">
      {handles(true, true)}
      <div className="node-t"><span className="mini">{data.type}</span> {data.name}</div>
      <div className="node-s">
        {data.binds.length
          ? data.binds.map((b, i) => <span key={i}>{i > 0 && ', '}{b.address}{b.tls && <span title="TLS"> 🔒</span>}</span>)
          : <span className="warn">{t('map.noBind')}</span>}
      </div>
    </div>
  )
}

export function BackendNode({ data }: NodeProps<Node<BackendData>>) {
  const t = useT()
  return (
    <div className="node be" data-testid="map-backend">
      {handles(true, true)}
      <div className="node-t">{data.name}</div>
      <div className="node-s">{data.balance}</div>
      <div className="node-c">
        {data.incoming.map((e, i) => e.def ? <span key={i} className="def">{t('map.default')}</span> : <span key={i} className="cnd">{t('map.if', { cond: e.condition ?? '' })}</span>)}
        {!data.incoming.length && <span className="muted">{t('map.noTraffic')}</span>}
      </div>
    </div>
  )
}

export function MissingNode({ data }: NodeProps<Node<MissingData>>) {
  const t = useT()
  return (
    <div className="node missing" data-testid="map-missing">
      {handles(true, false)}
      <div className="node-t">{data.name}</div>
      <div className="node-s">{t('map.notExist')}</div>
    </div>
  )
}

export function ServerNode({ data }: NodeProps<Node<ServerData>>) {
  const t = useT()
  return (
    <div className={`node srv ${data.backup ? 'backup' : ''} ${data.disabled ? 'disabled' : ''}`} data-testid="map-server">
      {handles(true, false)}
      <div className="node-t">{data.name}{data.check && <span className="dot ok" title={t('map.healthCheck')} />}</div>
      <div className="node-s">{data.address}{data.backup ? ` · ${t('map.backup')}` : ''}{data.disabled ? ` · ${t('map.disabled')}` : ''}</div>
    </div>
  )
}

export interface TipEdgeData extends Record<string, unknown> { tip?: string }

// Edge with a tooltip (SVG <title>) over a wide band that is easy to point at.
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
