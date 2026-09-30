type FailureClasses = Record<string, { label: string; owner: string }>

/**
 * Presentation of the server's run failure classes. Each says who acts: the
 * operator for a platform fault, the Space for its own configuration, or
 * whoever reads the run when it is the model or the Agent's own work.
 */
export const FAILURE_CLASSES: FailureClasses = {
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

/**
 * Presentation of the server's Workflow run failure classes. A Workflow run can
 * fail while every task run under it succeeded, so these are shown on their
 * own. A failed node's task run carries its own class above.
 */
export const WORKFLOW_FAILURE_CLASSES: FailureClasses = {
  node_failed: { label: "Node run failed", owner: "See the task run failures" },
  output_schema: { label: "Output schema not met", owner: "Space" },
  node_timeout: { label: "Node timed out", owner: "Space" },
  admission: { label: "Node not admitted", owner: "Space or Operator" },
  request_declined: { label: "Request declined", owner: "Space" },
  request_expired: { label: "Request expired", owner: "Space members" },
  run_deadline: { label: "Run deadline passed", owner: "Space" },
  unclassified: { label: "Unclassified", owner: "Check the run" },
}

/** What a pending Workflow request asks a Space member for, by kind. */
const REQUEST_KINDS: Record<string, string> = {
  input: "input request",
  question: "Agent question",
}

export function failureLabel(cls: string, classes: FailureClasses = FAILURE_CLASSES): string {
  return classes[cls]?.label ?? cls
}

function formatSpan(ms: number): string {
  const minutes = Math.max(0, Math.floor(ms / 60_000))
  if (minutes < 1) return "under a minute"
  if (minutes < 60) return `${minutes} min`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours} h ${minutes % 60} min`
  return `${Math.floor(hours / 24)} d`
}

/**
 * How long ago `since` was, measured against the server's clock rather than the
 * browser's, so a skewed laptop cannot make a stuck run look fresh.
 */
export function ageSince(since: string | undefined, serverTime: string | number): string | null {
  if (!since) return null
  const ms = new Date(serverTime).getTime() - new Date(since).getTime()
  if (!Number.isFinite(ms)) return null
  return formatSpan(ms)
}

/**
 * How long until `until` by the server's clock. A time already passed is
 * overdue: the server has not yet expired that request.
 */
export function timeUntil(until: string | undefined, serverTime: string | number): string | null {
  if (!until) return null
  const ms = new Date(until).getTime() - new Date(serverTime).getTime()
  if (!Number.isFinite(ms)) return null
  return ms <= 0 ? "overdue" : `in ${formatSpan(ms)}`
}

/** Failure counts in the server's class order, dropping empty ones. */
export function orderedFailures(
  failures: Record<string, number> | undefined,
  classes: FailureClasses = FAILURE_CLASSES,
): [string, number][] {
  const order = Object.keys(classes)
  return Object.entries(failures ?? {})
    .filter(([, n]) => n > 0)
    .sort(([a], [b]) => {
      const ia = order.indexOf(a)
      const ib = order.indexOf(b)
      return (ia < 0 ? order.length : ia) - (ib < 0 ? order.length : ib)
    })
}

/** "2 input requests, 1 Agent question", or null when nothing waits. */
export function waitingSummary(waiting: Record<string, number> | undefined): string | null {
  const parts = Object.entries(waiting ?? {})
    .filter(([, n]) => n > 0)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([kind, n]) => {
      const label = REQUEST_KINDS[kind] ?? kind
      return `${n} ${label}${n === 1 ? "" : "s"}`
    })
  return parts.length > 0 ? parts.join(", ") : null
}
