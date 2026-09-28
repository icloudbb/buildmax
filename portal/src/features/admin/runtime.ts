/**
 * Presentation of the server's run failure classes. Each says who acts: the
 * operator for a platform fault, the Space for its own configuration, or
 * whoever reads the run when it is the model or the Agent's own work.
 */
export const FAILURE_CLASSES: Record<string, { label: string; owner: string }> = {
  dispatch: { label: "Dispatch", owner: "Operator" },
  worker_lost: { label: "Worker lost", owner: "Operator" },
  abandoned: { label: "Abandoned", owner: "Operator" },
  interrupted: { label: "Interrupted", owner: "Operator" },
  infrastructure: { label: "Infrastructure", owner: "Operator" },
  space_configuration: { label: "Space configuration", owner: "Space owner" },
  model: { label: "Model provider", owner: "Operator or Space" },
  run: { label: "Agent run", owner: "Space" },
  unclassified: { label: "Unclassified", owner: "Check the run" },
}

export function failureLabel(cls: string): string {
  return FAILURE_CLASSES[cls]?.label ?? cls
}

/**
 * How long ago `since` was, measured against the server's clock rather than the
 * browser's, so a skewed laptop cannot make a stuck run look fresh.
 */
export function ageSince(since: string | undefined, serverTime: string | number): string | null {
  if (!since) return null
  const ms = new Date(serverTime).getTime() - new Date(since).getTime()
  if (!Number.isFinite(ms)) return null
  const minutes = Math.max(0, Math.floor(ms / 60_000))
  if (minutes < 1) return "under a minute"
  if (minutes < 60) return `${minutes} min`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours} h ${minutes % 60} min`
  return `${Math.floor(hours / 24)} d`
}

/** Failure counts in the server's class order, dropping empty ones. */
export function orderedFailures(failures: Record<string, number> | undefined): [string, number][] {
  const order = Object.keys(FAILURE_CLASSES)
  return Object.entries(failures ?? {})
    .filter(([, n]) => n > 0)
    .sort(([a], [b]) => {
      const ia = order.indexOf(a)
      const ib = order.indexOf(b)
      return (ia < 0 ? order.length : ia) - (ib < 0 ? order.length : ib)
    })
}
