import type { Translate } from "@buildmax/gui"
import type { ApiTaskRunTrace, ApiTraceBoundary, ApiTraceMCP } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

/** How a boundary should read to someone deciding whether to trust a run. */
export interface BoundaryDescription {
  /**
   * open — nothing confined the run.
   * sandboxed — a sandbox was in effect.
   * unknown — the trace predates boundary recording.
   *
   * "unknown" is deliberately not folded into "open": a run nobody checked and
   * a run checked and found unconfined are different facts, and only the second
   * is something the deployment chose.
   */
  tone: "open" | "sandboxed" | "unknown"
  text: string
  /** The layer chain that decided it, already joined for display. */
  sources: string | null
}

export function describeBoundary(
  boundary: ApiTraceBoundary | undefined,
  t: Translate<MessageKey>,
): BoundaryDescription {
  if (!boundary) {
    return { tone: "unknown", text: t("runs.boundary.unknown"), sources: null }
  }
  const sources = boundary.sources?.length ? boundary.sources.join(" → ") : null
  if (!boundary.sandboxed) {
    return { tone: "open", text: t("runs.boundary.open"), sources }
  }
  const { backend, mode } = boundary
  const sentences = [
    backend && mode
      ? t("runs.boundary.sandboxedViaMode", { backend, mode })
      : backend
        ? t("runs.boundary.sandboxedVia", { backend })
        : mode
          ? t("runs.boundary.sandboxedMode", { mode })
          : t("runs.boundary.sandboxed"),
  ]
  if (boundary.downgraded) sentences.push(t("runs.boundary.downgraded"))
  return { tone: "sandboxed", text: sentences.join(t("runs.sentenceSeparator")), sources }
}

/** How a run's MCP treatment should read beside the boundary. */
export interface MCPDescription {
  /**
   * enforced — the unattended-worker profile disabled stdio.
   * open — stdio was allowed (a local surface).
   * unknown — the trace predates MCP-treatment recording.
   *
   * "unknown" is deliberately not folded into "open": a run whose treatment was
   * never recorded and a run recorded as allowing stdio are different facts.
   */
  tone: "enforced" | "open" | "unknown"
  text: string
}

export function describeMCP(mcp: ApiTraceMCP | undefined, t: Translate<MessageKey>): MCPDescription {
  if (!mcp) {
    return { tone: "unknown", text: t("runs.mcp.unknown") }
  }
  // Transport names are protocol identifiers, listed as recorded.
  const remote = mcp.remote_transports?.length
    ? t("runs.mcp.remote", { transports: mcp.remote_transports.join(", ") })
    : t("runs.mcp.noRemote")
  if (mcp.stdio_disabled) {
    return { tone: "enforced", text: t("runs.mcp.enforced", { remote }) }
  }
  return { tone: "open", text: t("runs.mcp.open", { remote }) }
}

export function formatDuration(ms?: number): string {
  if (!ms || ms < 0) return "—"
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

/** Wall-clock time between the run's first and last record. */
export function runElapsed(trace: ApiTaskRunTrace): string {
  if (!trace.started_at || !trace.ended_at) return "—"
  const start = Date.parse(trace.started_at)
  const end = Date.parse(trace.ended_at)
  if (Number.isNaN(start) || Number.isNaN(end)) return "—"
  return formatDuration(end - start)
}
