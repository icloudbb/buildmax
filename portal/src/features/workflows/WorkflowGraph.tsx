import { useMemo } from "react"
import { statusLabel } from "../../lib/statusLabels"

/** One node to draw. `needs` are the ids this node depends on (its inbound
 *  edges). `onOpen`, when set, makes the node a button that opens its detail. */
export interface GraphNode {
  id: string
  label?: string
  sublabel?: string
  status?: string
  needs?: string[] | null
  onOpen?: () => void
}

export interface Positioned extends GraphNode {
  x: number
  y: number
}

const NODE_W = 220
const NODE_H = 104
const COL_GAP = 56
const ROW_GAP = 20
const PAD = 8

/** longestDepth returns each node's column: the length of the longest chain of
 *  `needs` reaching it, so a node always sits to the right of everything it
 *  depends on. Edges to unknown ids are ignored, and a cycle (which the server
 *  rejects, so this is only defensive) stops rather than recursing forever. */
function longestDepth(nodes: GraphNode[]): Map<string, number> {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const depth = new Map<string, number>()
  const visit = (id: string, stack: Set<string>): number => {
    const cached = depth.get(id)
    if (cached !== undefined) return cached
    if (stack.has(id)) return 0
    stack.add(id)
    const node = byId.get(id)
    let d = 0
    for (const need of node?.needs ?? []) {
      if (byId.has(need)) d = Math.max(d, visit(need, stack) + 1)
    }
    stack.delete(id)
    depth.set(id, d)
    return d
  }
  for (const n of nodes) visit(n.id, new Set())
  return depth
}

/** layout assigns every node a column (its longest depth) and a row (its order
 *  among the nodes sharing that column, preserving the input order, which the
 *  caller supplies in topological order). */
export function computeGraphLayout(nodes: GraphNode[]): { positioned: Positioned[]; width: number; height: number } {
  const depth = longestDepth(nodes)
  const rows = new Map<number, number>() // column -> next free row
  const positioned = nodes.map((n): Positioned => {
    const col = depth.get(n.id) ?? 0
    const row = rows.get(col) ?? 0
    rows.set(col, row + 1)
    return { ...n, x: PAD + col * (NODE_W + COL_GAP), y: PAD + row * (NODE_H + ROW_GAP) }
  })
  if (positioned.length === 0) {
    return { positioned, width: 0, height: 0 }
  }
  const cols = Math.max(0, ...Array.from(depth.values())) + 1
  const maxRows = Math.max(0, ...Array.from(rows.values()))
  return {
    positioned,
    width: PAD * 2 + cols * NODE_W + (cols - 1) * COL_GAP,
    height: PAD * 2 + maxRows * NODE_H + (maxRows - 1) * ROW_GAP,
  }
}

/**
 * A read-only visual DAG: node boxes laid out left-to-right by dependency depth
 * with edges drawn from each node it `needs`. The graph is derived state, so it
 * takes plain nodes and never fetches. A node with `onOpen` is a button that
 * opens its detail (its Task, in the run view).
 */
export function WorkflowGraph({ nodes, emptyLabel = "No nodes to graph." }: { nodes: GraphNode[]; emptyLabel?: string }) {
  const { positioned, width, height } = useMemo(() => computeGraphLayout(nodes), [nodes])
  const posById = useMemo(() => new Map(positioned.map((p) => [p.id, p])), [positioned])

  if (nodes.length === 0) {
    return <p className="page-activity__empty">{emptyLabel}</p>
  }

  const edges = positioned.flatMap((node) =>
    (node.needs ?? []).flatMap((need) => {
      const from = posById.get(need)
      if (!from) return []
      const x1 = from.x + NODE_W
      const y1 = from.y + NODE_H / 2
      const x2 = node.x
      const y2 = node.y + NODE_H / 2
      const dx = Math.max(16, (x2 - x1) / 2)
      return [{ key: `${need}->${node.id}`, d: `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}` }]
    }),
  )

  return (
    <div className="wf-graph__scroll">
      <div className="wf-graph" style={{ width, height }}>
        <svg className="wf-graph__edges" width={width} height={height} aria-hidden="true">
          {edges.map((e) => (
            <path key={e.key} className="wf-graph__edge" d={e.d} fill="none" />
          ))}
        </svg>
        {positioned.map((node) => {
          const cls = `wf-graph__node wf-graph__node--${node.status ?? "unknown"}`
          const style = { left: node.x, top: node.y, width: NODE_W, height: NODE_H }
          const body = (
            <>
              <span className="wf-graph__node-id">{node.label ?? node.id}</span>
              {node.sublabel ? <span className="wf-graph__node-sub">{node.sublabel}</span> : null}
              {node.status ? <span className="wf-graph__node-status">{statusLabel(node.status)}</span> : null}
            </>
          )
          return node.onOpen ? (
            <button key={node.id} type="button" className={cls} style={style} onClick={node.onOpen}>
              {body}
            </button>
          ) : (
            <div key={node.id} className={cls} style={style}>
              {body}
            </div>
          )
        })}
      </div>
    </div>
  )
}
