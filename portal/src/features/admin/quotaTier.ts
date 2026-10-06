import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import type { ApiQuotaTier } from "../../lib/api/types"

/**
 * The limits a tier imposes, in one line, so an operator choosing a tier sees
 * what it means rather than only its name. Storage is a stock rather than a
 * rate, so it is named apart from the period, and zero reads as no limit.
 */
export function describeTierLimits(tier: ApiQuotaTier, t: Translate<MessageKey>): string {
  const n = (value: number) => value.toLocaleString("en-US")
  const rates = t("admin.tier.rates", {
    runs: n(tier.max_runs_per_period),
    tokens: n(tier.max_tokens_per_period),
    days: tier.period_days,
  })
  const storage =
    tier.max_storage_bytes > 0
      ? t("admin.tier.storage", { bytes: n(tier.max_storage_bytes) })
      : t("admin.tier.unlimitedStorage")
  return t("admin.tier.limits", { rates, storage })
}
