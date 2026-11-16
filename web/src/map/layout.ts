import type { Topology } from '../core/types'

// Three-column layout (entries → backends → servers). Geometry only: what
// connects to what is decided by the core (topology).

export interface PlacedNode {
  id: string
  kind: 'entry' | 'backend' | 'missing' | 'server'
  x: number
  y: number
  /** Section index (unified view) the click leads to; -1 if none. */
  section: number
  index?: number
}

export const COL_X = { entry: 0, backend: 340, server: 680 }
const H = { entry: 62, backend: 84, missing: 56, server: 50, gap: 14 }

export const entryId = (section: number) => `s${section}`
export const backendId = (section: number) => `s${section}`
export const missingId = (name: string) => `m:${name}`
export const serverId = (owner: number, i: number) => `v${owner}-${i}`

export function layoutTopology(t: Topology): { nodes: PlacedNode[]; height: number } {
  const nodes: PlacedNode[] = []
  let y = 0

  // Backends (y sus servidores) y backends inexistentes, en bloques apilados.
  for (const b of t.backends) {
    const block = Math.max(H.backend + H.gap, b.servers.length * (H.server + 6))
    nodes.push({ id: backendId(b.section), kind: 'backend', x: COL_X.backend, y: y + (block - H.backend) / 2, section: b.section })
    b.servers.forEach((_, i) => nodes.push({ id: serverId(b.section, i), kind: 'server', x: COL_X.server, y: y + i * (H.server + 6) + (block - b.servers.length * (H.server + 6)) / 2, section: b.section, index: i }))
    y += block + H.gap
  }
  for (const name of new Set(t.edges.filter(e => e.to < 0).map(e => e.toName))) {
    nodes.push({ id: missingId(name), kind: 'missing', x: COL_X.backend, y, section: -1 })
    y += H.missing + H.gap
  }

  // Entries: close to their targets; listens also contribute their own servers.
  const pos = new Map(nodes.map(n => [n.id, n.y]))
  let ey = 0
  let sy = y
  const entries = t.entries.map(e => {
    const targets = t.edges.filter(x => x.from === e.section).map(x => pos.get(x.to >= 0 ? backendId(x.to) : missingId(x.toName))).filter((v): v is number => v !== undefined)
    return { e, want: targets.length ? targets.reduce((a, b) => a + b, 0) / targets.length : ey }
  })
  for (const { e, want } of entries) {
    const top = Math.max(want, ey)
    nodes.push({ id: entryId(e.section), kind: 'entry', x: COL_X.entry, y: top, section: e.section })
    ey = top + H.entry + H.gap
    // The servers of a listen go below those of the backends.
    ;(e.servers ?? []).forEach((_, i) => {
      nodes.push({ id: serverId(e.section, i), kind: 'server', x: COL_X.server, y: sy, section: e.section, index: i })
      sy += H.server + 6
    })
  }
  return { nodes, height: Math.max(ey, sy, y) }
}
