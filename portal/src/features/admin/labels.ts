import type { Locale, Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"

type Labels = Record<string, MessageKey>

// The server's identifiers for what Overview and Space detail report. Each is
// shown by its label; a value this build does not know is humanized rather than
// shown as a raw identifier, and the config values an operator types keep the
// raw value beside the label.
const DEPENDENCIES: Labels = {
  database: "admin.dependency.database",
  object_storage: "admin.dependency.object_storage",
}

const DEPENDENCY_STATUSES: Labels = {
  ok: "admin.dependencyStatus.ok",
  failed: "admin.dependencyStatus.failed",
}

const WORKER_RUN_MODES: Labels = {
  k8s_job: "admin.runMode.k8s_job",
  local_process: "admin.runMode.local_process",
}

const WORKER_TRANSPORTS: Labels = {
  direct: "admin.transport.direct",
  buildmax: "admin.transport.buildmax",
}

const QUOTA_TIERS: Labels = {
  free_trial: "admin.tierName.free_trial",
  pro: "admin.tierName.pro",
}

function humanize(value: string): string {
  return value.replace(/_/g, " ").replace(/^./, (first) => first.toUpperCase())
}

function labelOf(labels: Labels, value: string, t: Translate<MessageKey>): string {
  const key = labels[value]
  return key ? t(key) : humanize(value)
}

export const dependencyLabel = (name: string, t: Translate<MessageKey>) => labelOf(DEPENDENCIES, name, t)
export const dependencyStatusLabel = (status: string, t: Translate<MessageKey>) =>
  labelOf(DEPENDENCY_STATUSES, status, t)
export const workerRunModeLabel = (mode: string, t: Translate<MessageKey>) => labelOf(WORKER_RUN_MODES, mode, t)
export const workerTransportLabel = (transport: string, t: Translate<MessageKey>) =>
  labelOf(WORKER_TRANSPORTS, transport, t)
export const quotaTierLabel = (tier: string, t: Translate<MessageKey>) => labelOf(QUOTA_TIERS, tier, t)

/** A count with the locale's digit grouping: 10,000,000 rather than 10000000. */
export function formatCount(n: number, locale: Locale): string {
  return n.toLocaleString(locale === "en" ? "en-US" : locale)
}

// Task run statuses in lifecycle order, so the counts read as the work moves.
const RUN_STATUS_ORDER = ["PENDING", "SCHEDULED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELED"]

/** Status counts in lifecycle order; a status this build does not know goes last. */
export function orderedRunStatuses(counts: Record<string, number>): [string, number][] {
  const rank = (status: string) => {
    const i = RUN_STATUS_ORDER.indexOf(status.toUpperCase())
    return i < 0 ? RUN_STATUS_ORDER.length : i
  }
  return Object.entries(counts).sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
}
