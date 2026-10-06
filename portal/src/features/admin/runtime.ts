import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"

type FailureClasses = Record<string, { label: MessageKey; owner: MessageKey }>

/**
 * Presentation of the server's run failure classes. Each says who acts: the
 * operator for a platform fault, the Space for its own configuration, or
 * whoever reads the run when it is the model or the Agent's own work.
 */
export const FAILURE_CLASSES: FailureClasses = {
  dispatch: { label: "admin.failure.dispatch", owner: "admin.actor.operator" },
  worker_lost: { label: "admin.failure.worker_lost", owner: "admin.actor.operator" },
  abandoned: { label: "admin.failure.abandoned", owner: "admin.actor.operator" },
  interrupted: { label: "admin.failure.interrupted", owner: "admin.actor.operator" },
  infrastructure: { label: "admin.failure.infrastructure", owner: "admin.actor.operator" },
  space_configuration: { label: "admin.failure.space_configuration", owner: "admin.actor.spaceOwner" },
  model: { label: "admin.failure.model", owner: "admin.actor.operatorOrSpace" },
  run: { label: "admin.failure.run", owner: "admin.actor.space" },
  unclassified: { label: "admin.failure.unclassified", owner: "admin.actor.checkRun" },
}

/**
 * Presentation of the server's Workflow run failure classes. A Workflow run can
 * fail while every task run under it succeeded, so these are shown on their
 * own. A failed node's task run carries its own class above.
 */
export const WORKFLOW_FAILURE_CLASSES: FailureClasses = {
  node_failed: { label: "admin.failure.node_failed", owner: "admin.actor.seeTaskRunFailures" },
  output_schema: { label: "admin.failure.output_schema", owner: "admin.actor.space" },
  node_timeout: { label: "admin.failure.node_timeout", owner: "admin.actor.space" },
  admission: { label: "admin.failure.admission", owner: "admin.actor.spaceOrOperator" },
  request_declined: { label: "admin.failure.request_declined", owner: "admin.actor.space" },
  request_expired: { label: "admin.failure.request_expired", owner: "admin.actor.spaceMembers" },
  run_deadline: { label: "admin.failure.run_deadline", owner: "admin.actor.space" },
  unclassified: { label: "admin.failure.unclassified", owner: "admin.actor.checkRun" },
}

/** What a pending Workflow request asks a Space member for, by kind. */
const REQUEST_KINDS: Record<string, MessageKey> = {
  input: "admin.request.input",
  question: "admin.request.question",
}

export function failureLabel(
  cls: string,
  t: Translate<MessageKey>,
  classes: FailureClasses = FAILURE_CLASSES,
): string {
  const entry = classes[cls]
  return entry ? t(entry.label) : cls
}

/** Who acts on a failure class, or null for a class this build does not know. */
export function failureOwner(
  cls: string,
  t: Translate<MessageKey>,
  classes: FailureClasses = FAILURE_CLASSES,
): string | null {
  const entry = classes[cls]
  return entry ? t(entry.owner) : null
}

function formatSpan(ms: number, t: Translate<MessageKey>): string {
  const minutes = Math.max(0, Math.floor(ms / 60_000))
  if (minutes < 1) return t("admin.span.underMinute")
  if (minutes < 60) return t("admin.span.minutes", { minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return t("admin.span.hours", { hours, minutes: minutes % 60 })
  return t("admin.span.days", { days: Math.floor(hours / 24) })
}

/**
 * How long ago `since` was, measured against the server's clock rather than the
 * browser's, so a skewed laptop cannot make a stuck run look fresh.
 */
export function ageSince(
  since: string | undefined,
  serverTime: string | number,
  t: Translate<MessageKey>,
): string | null {
  if (!since) return null
  const ms = new Date(serverTime).getTime() - new Date(since).getTime()
  if (!Number.isFinite(ms)) return null
  return formatSpan(ms, t)
}

/**
 * How long until `until` by the server's clock. A time already passed is
 * overdue: the server has not yet expired that request.
 */
export function timeUntil(
  until: string | undefined,
  serverTime: string | number,
  t: Translate<MessageKey>,
): string | null {
  if (!until) return null
  const ms = new Date(until).getTime() - new Date(serverTime).getTime()
  if (!Number.isFinite(ms)) return null
  return ms <= 0 ? t("admin.span.overdue") : t("admin.span.in", { span: formatSpan(ms, t) })
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
export function waitingSummary(
  waiting: Record<string, number> | undefined,
  t: Translate<MessageKey>,
): string | null {
  const parts = Object.entries(waiting ?? {})
    .filter(([, n]) => n > 0)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([kind, count]) => {
      const key = REQUEST_KINDS[kind]
      return key ? t(key, { count }) : t("admin.request.other", { count, kind })
    })
  return parts.length > 0 ? parts.join(t("admin.listSeparator")) : null
}
