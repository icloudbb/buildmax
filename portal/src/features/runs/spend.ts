import type { Translate } from "@buildmax/gui"
import type { ApiLLMCallCost, ApiTaskRunLLMCall, ApiTaskRunTrace } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

/** One currency unit, in the nano-units the API reports amounts as. */
const NANO_PER_UNIT = 1_000_000_000

/** Per-model accounting, so a run that called two models is legible. */
export interface ModelSpend {
  model: string
  calls: number
  totalTokens: number
  /** Calls whose provider reported no usage. They are not free, only unmeasured. */
  unreported: number
}

export interface SpendSummary {
  calls: number
  succeeded: number
  failed: number
  /** Calls that were accepted and never reached a terminal status. */
  inFlight: number
  promptTokens: number
  completionTokens: number
  totalTokens: number
  /**
   * The cached parts of `promptTokens`, not tokens on top of it. A cache read
   * is prompt the provider served from its own store; a cache write is prompt
   * it stored. Adding either to the prompt total double-counts.
   */
  cacheReadTokens: number
  cacheWriteTokens: number
  /**
   * Calls that reported usage but no cache counts at all. Providers without
   * cache reporting are the common case, so zero reads must not be shown as a
   * measured miss.
   */
  cacheUnreported: number
  /**
   * What the run is estimated to have cost, or null when nothing in it could
   * be priced. Amounts are nano-units of `currency`.
   */
  cost: SpendCost | null
  /**
   * Calls that did work and could not be priced — an unpriced model, or one
   * quoted in a currency the rest of the run was not. The total above is then
   * missing part of the run and must be labelled, not shown as if complete.
   */
  unpriced: number
  /**
   * Calls whose usage the provider never reported. Kept separate from a zero
   * count: summing an unreported call as zero turns an unknown into a claim.
   */
  unreported: number
  retried: number
  byModel: ModelSpend[]
}

/** Tokens a call is known to have used, or null when nothing was reported. */
function callTokens(call: ApiTaskRunLLMCall): number | null {
  if (typeof call.total_tokens === "number") return call.total_tokens
  const prompt = call.prompt_tokens
  const completion = call.completion_tokens
  if (typeof prompt === "number" || typeof completion === "number") {
    return (prompt ?? 0) + (completion ?? 0)
  }
  return null
}

/**
 * Aggregate a run's ledger rows.
 *
 * Models are ordered by spend and then by name, so the model a run leaned on
 * is first and the order does not move between two runs that spent the same.
 */
export function summarizeSpend(calls: ApiTaskRunLLMCall[]): SpendSummary {
  const summary: SpendSummary = {
    calls: calls.length,
    succeeded: 0,
    failed: 0,
    inFlight: 0,
    promptTokens: 0,
    completionTokens: 0,
    totalTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    cacheUnreported: 0,
    cost: null,
    unpriced: 0,
    unreported: 0,
    retried: 0,
    byModel: [],
  }

  const models = new Map<string, ModelSpend>()
  for (const call of calls) {
    switch (call.status) {
      case "SUCCEEDED":
        summary.succeeded += 1
        break
      case "FAILED":
      case "CANCELED":
        summary.failed += 1
        break
      default:
        // ACCEPTED and anything a newer server adds. A call with no terminal
        // status is not a successful one, and lumping it in with either side
        // would misreport the run.
        summary.inFlight += 1
    }
    // Attempts counts tries, so the second one onward is a retry. A ledger
    // written before the field existed reports 0, which is not one attempt.
    if (typeof call.attempts === "number" && call.attempts > 1) {
      summary.retried += call.attempts - 1
    }

    summary.promptTokens += call.prompt_tokens ?? 0
    summary.completionTokens += call.completion_tokens ?? 0
    const tokens = callTokens(call)
    if (tokens === null) {
      summary.unreported += 1
    } else {
      summary.totalTokens += tokens
    }

    const cacheRead = call.cache_read_tokens
    const cacheWrite = call.cache_write_tokens
    if (typeof cacheRead !== "number" && typeof cacheWrite !== "number") {
      // Only a call that reported usage can be said to have reported no cache
      // counts. One that reported nothing at all is already counted above, and
      // counting it twice would read as two separate silences.
      if (tokens !== null) summary.cacheUnreported += 1
    } else {
      summary.cacheReadTokens += cacheRead ?? 0
      summary.cacheWriteTokens += cacheWrite ?? 0
    }

    if (call.cost) {
      const added = addCost(summary.cost, call.cost)
      // Two currencies, and the Portal holds no exchange rate. Inventing one
      // would produce a total that is wrong in both, so the call is counted as
      // unpriced and the earlier total stands, labelled incomplete.
      if (added) summary.cost = added
      else summary.unpriced += 1
    } else if (tokens !== null) {
      // A call that did work and carries no cost was run against a model
      // nobody priced. One with no usage at all is already counted above.
      summary.unpriced += 1
    }

    const key = call.model || "—"
    const entry = models.get(key) ?? { model: key, calls: 0, totalTokens: 0, unreported: 0 }
    entry.calls += 1
    if (tokens === null) entry.unreported += 1
    else entry.totalTokens += tokens
    models.set(key, entry)
  }

  summary.byModel = [...models.values()].sort(
    (a, b) => b.totalTokens - a.totalTokens || a.model.localeCompare(b.model)
  )
  return summary
}

/** A run's accumulated spend, in nano-units of one currency. */
export interface SpendCost {
  currency: string
  uncached: number
  cacheRead: number
  cacheWrite: number
  output: number
  total: number
  /** What the same tokens would have cost with no caching at all. */
  baseline: number
}

/** Add one call's cost to a running total, or null when the currencies differ. */
function addCost(into: SpendCost | null, call: ApiLLMCallCost): SpendCost | null {
  if (!into) {
    return {
      currency: call.currency,
      uncached: call.uncached,
      cacheRead: call.cache_read,
      cacheWrite: call.cache_write,
      output: call.output,
      total: call.total,
      baseline: call.baseline,
    }
  }
  if (into.currency !== call.currency) return null
  return {
    currency: into.currency,
    uncached: into.uncached + call.uncached,
    cacheRead: into.cacheRead + call.cache_read,
    cacheWrite: into.cacheWrite + call.cache_write,
    output: into.output + call.output,
    total: into.total + call.total,
    baseline: into.baseline + call.baseline,
  }
}

/**
 * What caching saved, or null when it cost more than it saved.
 *
 * A negative saving is not a saving. A run that wrote cache entries nothing
 * read back genuinely paid more than it would have uncached, and dressing that
 * up as a small win is the claim this view exists not to make.
 */
export function cacheSaving(cost: SpendCost | null): number | null {
  if (!cost || cost.baseline <= cost.total) return null
  return cost.baseline - cost.total
}

/**
 * Render nano-units as an amount.
 *
 * Six decimal places, because a single cheap call rounds to zero at two and a
 * reader would take that for free.
 */
export function formatAmount(nano: number, currency: string): string {
  const value = nano / NANO_PER_UNIT
  return `${value.toFixed(6)} ${currency}`
}

/**
 * Why a run shows no managed model calls.
 *
 * An empty list has causes that are not interchangeable to anyone reading a
 * run: the deployment does not account managed calls at all, the run reached
 * models directly so the server never saw them, or the run genuinely called no
 * model. One empty state for all of them would tell a reader that nothing was
 * spent, which is only true in the last case.
 *
 * A read that failed passes the server's own message through rather than
 * substituting a generic one — "managed model calls not configured" and a
 * missing run are different facts, and the server already distinguishes them.
 */
export function describeSpend(
  options: {
    calls: ApiTaskRunLLMCall[]
    /** The server's message when the ledger could not be read. */
    error: string | null
    trace: ApiTaskRunTrace | null
  },
  t: Translate<MessageKey>,
): string | null {
  if (options.error) return options.error
  if (options.calls.length > 0) return null
  const traced = options.trace?.llm_calls ?? 0
  if (traced > 0) return t("runs.spend.bypassed", { count: traced })
  return t("runs.spend.none")
}

/** How long a call took, to the second. */
export function callElapsed(call: ApiTaskRunLLMCall): string {
  if (!call.completed_at) return "—"
  const ms = new Date(call.completed_at).getTime() - new Date(call.accepted_at).getTime()
  const seconds = Math.floor(ms / 1000)
  if (seconds <= 0) return "<1 s"
  return `${seconds} s`
}
