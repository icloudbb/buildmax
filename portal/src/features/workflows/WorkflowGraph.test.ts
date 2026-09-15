import { describe, expect, it } from "vitest"
import { computeGraphLayout, type GraphNode } from "./WorkflowGraph"

function colOf(positioned: { id: string; x: number }[], id: string): number {
  // Columns are evenly spaced, so distinct x values map to distinct columns.
  const xs = Array.from(new Set(positioned.map((p) => p.x))).sort((a, b) => a - b)
  const x = positioned.find((p) => p.id === id)!.x
  return xs.indexOf(x)
}

describe("computeGraphLayout", () => {
  it("places a node to the right of everything it depends on", () => {
    const nodes: GraphNode[] = [
      { id: "research" },
      { id: "analyze", needs: ["research"] },
      { id: "summarize", needs: ["research"] },
      { id: "report", needs: ["analyze", "summarize"] },
    ]
    const { positioned } = computeGraphLayout(nodes)
    expect(colOf(positioned, "research")).toBe(0)
    expect(colOf(positioned, "analyze")).toBe(1)
    expect(colOf(positioned, "summarize")).toBe(1)
    expect(colOf(positioned, "report")).toBe(2)
    // Siblings in one column get distinct rows.
    const analyze = positioned.find((p) => p.id === "analyze")!
    const summarize = positioned.find((p) => p.id === "summarize")!
    expect(analyze.y).not.toBe(summarize.y)
  })

  it("uses the longest path, not the shortest, for a node's column", () => {
    // report depends on a directly and on c (a->b->c), so it must sit past c.
    const nodes: GraphNode[] = [
      { id: "a" },
      { id: "b", needs: ["a"] },
      { id: "c", needs: ["b"] },
      { id: "report", needs: ["a", "c"] },
    ]
    const { positioned } = computeGraphLayout(nodes)
    expect(colOf(positioned, "report")).toBe(3)
  })

  it("gives an empty graph zero size", () => {
    const { positioned, width } = computeGraphLayout([])
    expect(positioned).toEqual([])
    expect(width).toBeLessThanOrEqual(16)
  })
})
