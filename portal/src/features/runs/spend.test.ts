import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import type { ApiTaskRunLLMCall, ApiTaskRunTrace } from "../../lib/api/types"
import { translate, type MessageKey } from "../../i18n"
import { cacheSaving, callElapsed, describeSpend, formatAmount, summarizeSpend } from "./spend"

// The English catalog, which every expectation below is written against.
const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function call(overrides: Partial<ApiTaskRunLLMCall> = {}): ApiTaskRunLLMCall {
  return {
    id: "lc_1",
    streaming: false,
    accepted_at: "1970-01-01T00:16:40Z",
    status: "SUCCEEDED",
    ...overrides,
  }
}

function trace(overrides: Partial<ApiTaskRunTrace> = {}): ApiTaskRunTrace {
  return {
    task_run_id: "tr_1",
    llm_calls: 0,
    tool_calls: 0,
    compactions: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    complete: true,
    ...overrides,
  }
}

describe("summarizeSpend", () => {
  it("keeps an unreported usage count out of the total", () => {
    // A provider that reported nothing and one that reported zero are different
    // facts. Summing the first as zero would present an unknown as a free call.
    const got = summarizeSpend([
      call({ id: "lc_1", total_tokens: 300 }),
      call({ id: "lc_2" }),
    ])
    expect(got.totalTokens).toBe(300)
    expect(got.unreported).toBe(1)
    expect(got.calls).toBe(2)
  })

  it("derives a total from the two halves when the server sent only those", () => {
    const got = summarizeSpend([call({ prompt_tokens: 120, completion_tokens: 30 })])
    expect(got.totalTokens).toBe(150)
    expect(got.unreported).toBe(0)
    expect(got.promptTokens).toBe(120)
    expect(got.completionTokens).toBe(30)
  })

  it("totals cached prompt without adding it to the prompt count", () => {
    // The cache counts are a breakdown of prompt_tokens. A summary that added
    // them would report 190 tokens of input for a call that sent 100.
    const got = summarizeSpend([
      call({ prompt_tokens: 100, completion_tokens: 4, cache_read_tokens: 80, cache_write_tokens: 10 }),
    ])
    expect(got.promptTokens).toBe(100)
    expect(got.cacheReadTokens).toBe(80)
    expect(got.cacheWriteTokens).toBe(10)
    expect(got.cacheUnreported).toBe(0)
  })

  it("separates a provider that reported no cache from one that reported zero", () => {
    const got = summarizeSpend([
      call({ id: "lc_1", prompt_tokens: 100, cache_read_tokens: 0, cache_write_tokens: 0 }),
      call({ id: "lc_2", prompt_tokens: 100 }),
    ])
    expect(got.cacheReadTokens).toBe(0)
    expect(got.cacheUnreported).toBe(1)
  })

  it("does not count a call with no usage at all as a cache silence", () => {
    // It is already counted as unreported. Counting it twice would present one
    // silent call as two separate gaps in the record.
    const got = summarizeSpend([call()])
    expect(got.unreported).toBe(1)
    expect(got.cacheUnreported).toBe(0)
  })

  it("totals cost from the rates each call recorded", () => {
    const got = summarizeSpend([
      call({
        id: "lc_1",
        prompt_tokens: 100,
        cost: { currency: "USD", uncached: 300, cache_read: 27, cache_write: 0, output: 15, total: 342, baseline: 400 },
      }),
      call({
        id: "lc_2",
        prompt_tokens: 100,
        cost: { currency: "USD", uncached: 100, cache_read: 3, cache_write: 0, output: 5, total: 108, baseline: 200 },
      }),
    ])
    expect(got.cost?.currency).toBe("USD")
    expect(got.cost?.total).toBe(342 + 108)
    expect(got.cost?.baseline).toBe(600)
    expect(got.unpriced).toBe(0)
    expect(cacheSaving(got.cost)).toBe(600 - 450)
  })

  it("counts a call the operator never priced rather than treating it as free", () => {
    const got = summarizeSpend([
      call({ id: "lc_1", prompt_tokens: 100, cost: { currency: "USD", uncached: 300, cache_read: 0, cache_write: 0, output: 15, total: 315, baseline: 315 } }),
      call({ id: "lc_2", prompt_tokens: 100 }),
    ])
    expect(got.cost?.total).toBe(315)
    expect(got.unpriced).toBe(1)
  })

  it("does not count an unmeasured call as unpriced", () => {
    // It reported no usage at all, which is already surfaced as unreported.
    // Counting it twice would present one gap as two.
    const got = summarizeSpend([call()])
    expect(got.unreported).toBe(1)
    expect(got.unpriced).toBe(0)
  })

  it("refuses to add two currencies rather than invent an exchange rate", () => {
    const got = summarizeSpend([
      call({ id: "lc_1", prompt_tokens: 100, cost: { currency: "USD", uncached: 300, cache_read: 0, cache_write: 0, output: 0, total: 300, baseline: 300 } }),
      call({ id: "lc_2", prompt_tokens: 100, cost: { currency: "EUR", uncached: 200, cache_read: 0, cache_write: 0, output: 0, total: 200, baseline: 200 } }),
    ])
    expect(got.cost?.currency).toBe("USD")
    expect(got.cost?.total).toBe(300)
    expect(got.unpriced).toBe(1)
  })

  it("reports no saving when caching cost more than it saved", () => {
    // A run that wrote a cache entry nothing read back paid more than it would
    // have uncached. Reporting that as a small win is the false claim this
    // view exists to avoid.
    const got = summarizeSpend([
      call({
        id: "lc_1",
        prompt_tokens: 100,
        cost: { currency: "USD", uncached: 30, cache_read: 0, cache_write: 375, output: 15, total: 420, baseline: 315 },
      }),
    ])
    expect(got.cost?.total).toBe(420)
    expect(cacheSaving(got.cost)).toBeNull()
  })

  it("counts a call with no terminal status as neither succeeded nor failed", () => {
    const got = summarizeSpend([
      call({ id: "lc_1", status: "ACCEPTED" }),
      call({ id: "lc_2", status: "FAILED" }),
      call({ id: "lc_3", status: "CANCELED" }),
      call({ id: "lc_4", status: "SUCCEEDED" }),
    ])
    expect(got.inFlight).toBe(1)
    expect(got.failed).toBe(2)
    expect(got.succeeded).toBe(1)
  })

  it("reports retries rather than attempts, and reads a zero as no record", () => {
    // Attempts counts tries, so only the second onward is a retry. A ledger row
    // written before the field existed reports 0, which is not one attempt.
    const got = summarizeSpend([
      call({ id: "lc_1", attempts: 3 }),
      call({ id: "lc_2", attempts: 1 }),
      call({ id: "lc_3", attempts: 0 }),
    ])
    expect(got.retried).toBe(2)
  })

  it("breaks spend down by model, heaviest first", () => {
    const got = summarizeSpend([
      call({ id: "lc_1", model: "Fast", total_tokens: 10 }),
      call({ id: "lc_2", model: "Deep", total_tokens: 900 }),
      call({ id: "lc_3", model: "Deep", total_tokens: 100 }),
    ])
    expect(got.byModel.map((entry) => entry.model)).toEqual(["Deep", "Fast"])
    expect(got.byModel[0]).toMatchObject({ calls: 2, totalTokens: 1000, unreported: 0 })
  })

  it("orders equal models by name so two runs do not disagree about order", () => {
    const got = summarizeSpend([
      call({ id: "lc_1", model: "Zeta", total_tokens: 5 }),
      call({ id: "lc_2", model: "Alpha", total_tokens: 5 }),
    ])
    expect(got.byModel.map((entry) => entry.model)).toEqual(["Alpha", "Zeta"])
  })
})

describe("describeSpend", () => {
  it("says nothing when there are calls to show", () => {
    expect(describeSpend({ calls: [call()], error: null, trace: trace() }, t)).toBeNull()
  })

  it("distinguishes a run that bypassed the gateway from one that called nothing", () => {
    // Both have an empty ledger, and only the second spent nothing. Telling a
    // reader "no spend" for the first would hide every direct-mode call.
    const bypassed = describeSpend({ calls: [], error: null, trace: trace({ llm_calls: 4 }) }, t)
    expect(bypassed).toContain("4 times")
    expect(bypassed).toContain("direct mode")

    const quiet = describeSpend({ calls: [], error: null, trace: trace({ llm_calls: 0 }) }, t)
    expect(quiet).toBe("This run called no model through the managed gateway.")
  })

  it("says nothing about direct mode when there is no trace to compare against", () => {
    expect(describeSpend({ calls: [], error: null, trace: null }, t)).toBe(
      "This run called no model through the managed gateway."
    )
  })

  it("passes the server's own explanation through", () => {
    // "managed model calls not configured" and a missing run mean different
    // things to an operator, and the server already distinguishes them.
    const got = describeSpend(
      { calls: [], error: "managed model calls not configured", trace: trace({ llm_calls: 2 }) },
      t,
    )
    expect(got).toBe("managed model calls not configured")
  })
})

describe("callElapsed", () => {
  it("reports an unfinished call as unknown rather than as instant", () => {
    expect(callElapsed(call({ completed_at: undefined }))).toBe("—")
  })

  it("does not round a sub-second call down to zero", () => {
    // The ledger stamps seconds, so a fast call and an instant one look alike.
    expect(callElapsed(call({ accepted_at: "1970-01-01T00:16:40Z", completed_at: "1970-01-01T00:16:40Z" }))).toBe("<1 s")
  })

  it("reports elapsed seconds", () => {
    expect(callElapsed(call({ accepted_at: "1970-01-01T00:16:40Z", completed_at: "1970-01-01T00:16:47Z" }))).toBe("7 s")
  })
})

describe("formatAmount", () => {
  it("shows six places so a cheap call does not read as free", () => {
    expect(formatAmount(1_000_000_000, "USD")).toBe("1.000000 USD")
    expect(formatAmount(72_000_000, "USD")).toBe("0.072000 USD")
    // Two places would show this as 0.00, which a reader takes for nothing.
    expect(formatAmount(4_000, "USD")).toBe("0.000004 USD")
  })
})
