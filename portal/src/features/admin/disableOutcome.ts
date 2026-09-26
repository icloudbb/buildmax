import type { ApiAdminUserAfterDisable } from "../../lib/api/types"

const stepLabels: Record<string, string> = {
  sessions: "revoking sessions",
  webhook_keys: "retiring webhook keys",
  schedules: "pausing schedules",
  runs: "canceling runs",
}

function plural(n: number, noun: string, verb: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"} ${verb}`
}

/**
 * Describes what a disable did. The account gate commits before cleanup, so a
 * failed cleanup step still leaves the account disabled; the message says that
 * first, then what failed, so the operator neither reverses nor forgets it.
 */
export function describeDisableOutcome(after: ApiAdminUserAfterDisable): {
  message: string
  incomplete: boolean
} {
  const parts = [plural(after.sessions_revoked, "session", "revoked")]
  if (after.schedules_paused) parts.push(plural(after.schedules_paused, "schedule", "paused"))
  if (after.runs_canceled) parts.push(plural(after.runs_canceled, "run", "canceled"))
  if (after.webhook_keys_retired) parts.push(plural(after.webhook_keys_retired, "webhook key", "retired"))
  const failed = after.cleanup_failed ?? []
  if (failed.length === 0) {
    return { message: `${after.email} is disabled. ${parts.join(", ")}.`, incomplete: false }
  }
  const steps = failed.map((s) => stepLabels[s] ?? s).join(", ")
  return {
    message:
      `${after.email} is disabled, but part of its cleanup failed: ${steps}. ` +
      `Done so far: ${parts.join(", ")}. Retrying is safe — it runs the cleanup again.`,
    incomplete: true,
  }
}
