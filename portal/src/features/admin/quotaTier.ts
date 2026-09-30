import type { ApiQuotaTier } from "../../lib/api/types"

/**
 * The limits a tier imposes, in one line, so an operator choosing a tier sees
 * what it means rather than only its name. Storage is a stock rather than a
 * rate, so it is named apart from the period, and zero reads as no limit.
 */
export function describeTierLimits(tier: ApiQuotaTier): string {
  const n = (value: number) => value.toLocaleString("en-US")
  const rates = `${n(tier.max_runs_per_period)} runs, ${n(tier.max_tokens_per_period)} tokens per ${tier.period_days} days`
  const storage =
    tier.max_storage_bytes > 0 ? `${n(tier.max_storage_bytes)} bytes of storage` : "unlimited storage"
  return `${rates}; ${storage}`
}
