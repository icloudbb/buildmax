import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import type { ApiUsage } from "../../lib/api/types"

/**
 * The share of a limit at which the server records a warning, mirrored here so
 * the Portal warns at the same point the trail does.
 *
 * The two are separate constants for a reason worth stating: the server's is
 * the one that decides what is recorded, and this one only decides what is
 * shown. Reading a number the server did not send would be worse — it would
 * make the display authoritative about a policy it does not hold — so this
 * stays a display threshold, and `internal/service/quota/alert.go` stays the
 * decision.
 */
export const QUOTA_WARN_THRESHOLD = 0.8

export interface QuotaPressure {
  /** near — approaching a limit. reached — the limit is spent. */
  tone: "near" | "reached"
  text: string
}

function shareOf(used: number, max?: number): number | null {
  if (max == null || max <= 0) return null
  return used / max
}

/**
 * How close a space is to its quota, or null when it is not close.
 *
 * A tier with no limits reports nothing rather than reporting comfort: an
 * unknown limit and a generous one look identical from here, and only one of
 * them means there is nothing to worry about.
 *
 * Runs and tokens are reported together when both are under pressure, because
 * a space that is at its run limit and its token limit has one problem, not two,
 * and reading two separate warnings invites fixing only the first.
 *
 * Storage is reported apart from them even when both are tight. It is a stock
 * rather than a rate, so the remedy is different in kind: waiting clears a run
 * or token quota as the window moves, and only deleting an artifact clears
 * this one. Folding it into the same sentence would tell someone at their
 * storage limit to wait, which would never work.
 */
export function describeQuotaPressure(
  usage: ApiUsage | null,
  t: Translate<MessageKey>,
): QuotaPressure | null {
  if (!usage) return null
  const runs = shareOf(usage.run_count, usage.max_runs_per_period)
  const tokens = shareOf(usage.total_tokens, usage.max_tokens_per_period)
  const storage = shareOf(usage.storage_bytes ?? 0, usage.max_storage_bytes)

  const reached: Resource[] = []
  const near: Resource[] = []
  if (runs != null && runs >= 1) reached.push("runs")
  else if (runs != null && runs >= QUOTA_WARN_THRESHOLD) near.push("runs")
  if (tokens != null && tokens >= 1) reached.push("tokens")
  else if (tokens != null && tokens >= QUOTA_WARN_THRESHOLD) near.push("tokens")

  const days = usage.period_days
  const windowed = days > 0
  // A spent rate limit is the more urgent of the two, so it is said first.
  if (reached.length > 0) {
    const resources = resourcesLabel(reached, t)
    return {
      tone: "reached",
      text: windowed
        ? t("settings.quota.reachedWindow", { resources, days })
        : t("settings.quota.reached", { resources }),
    }
  }
  if (storage != null && storage >= 1) {
    return { tone: "reached", text: t("settings.quota.storageReached") }
  }
  if (near.length > 0) {
    const resources = resourcesLabel(near, t)
    return {
      tone: "near",
      text: windowed
        ? t("settings.quota.nearWindow", { resources, days })
        : t("settings.quota.near", { resources }),
    }
  }
  if (storage != null && storage >= QUOTA_WARN_THRESHOLD) {
    return { tone: "near", text: t("settings.quota.storageNear") }
  }
  return null
}

type Resource = "runs" | "tokens"

function resourcesLabel(resources: Resource[], t: Translate<MessageKey>): string {
  if (resources.length > 1) return t("settings.quota.runsAndTokens")
  return resources[0] === "runs" ? t("settings.quota.runs") : t("settings.quota.tokens")
}
