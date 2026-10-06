import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import type { ApiAdminUserAfterDisable } from "../../lib/api/types"

const stepLabels: Record<string, MessageKey> = {
  sessions: "admin.disable.step.sessions",
  webhook_keys: "admin.disable.step.webhook_keys",
  schedules: "admin.disable.step.schedules",
  runs: "admin.disable.step.runs",
}

/**
 * Describes what a disable did. The account gate commits before cleanup, so a
 * failed cleanup step still leaves the account disabled; the message says that
 * first, then what failed, so the operator neither reverses nor forgets it.
 */
export function describeDisableOutcome(
  after: ApiAdminUserAfterDisable,
  t: Translate<MessageKey>,
): {
  message: string
  incomplete: boolean
} {
  const separator = t("admin.listSeparator")
  const parts = [t("admin.disable.sessionsRevoked", { count: after.sessions_revoked })]
  if (after.schedules_paused) parts.push(t("admin.disable.schedulesPaused", { count: after.schedules_paused }))
  if (after.runs_canceled) parts.push(t("admin.disable.runsCanceled", { count: after.runs_canceled }))
  if (after.webhook_keys_retired) {
    parts.push(t("admin.disable.webhookKeysRetired", { count: after.webhook_keys_retired }))
  }
  const failed = after.cleanup_failed ?? []
  if (failed.length === 0) {
    return {
      message: t("admin.disable.done", { email: after.email, parts: parts.join(separator) }),
      incomplete: false,
    }
  }
  const steps = failed.map((s) => (stepLabels[s] ? t(stepLabels[s]) : s)).join(separator)
  return {
    message: t("admin.disable.incomplete", { email: after.email, steps, parts: parts.join(separator) }),
    incomplete: true,
  }
}
