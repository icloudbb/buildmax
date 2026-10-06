import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import type { ApiUsage } from "../../lib/api/types"
import { translate, type MessageKey } from "../../i18n"
import { describeQuotaPressure as describeIn } from "./pressure"

// The English catalog, which every expectation below is written against.
const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)
const describeQuotaPressure = (u: ApiUsage | null) => describeIn(u, t)

function usage(overrides: Partial<ApiUsage> = {}): ApiUsage {
  return {
    run_count: 0,
    total_tokens: 0,
    tier: "free_trial",
    period_days: 30,
    ...overrides,
  }
}

describe("describeQuotaPressure", () => {
  it("says nothing when there is nothing to say", () => {
    expect(
      describeQuotaPressure(usage({ run_count: 2, max_runs_per_period: 100 }))
    ).toBeNull()
  })

  it("treats a tier with no limits as unknown rather than as comfortable", () => {
    // An unknown limit and a generous one look identical from here, and only
    // one of them means there is nothing to worry about.
    expect(describeQuotaPressure(usage({ run_count: 10_000 }))).toBeNull()
    expect(describeQuotaPressure(null)).toBeNull()
  })

  it("warns before the refusals start", () => {
    const got = describeQuotaPressure(usage({ run_count: 8, max_runs_per_period: 10 }))
    expect(got?.tone).toBe("near")
    expect(got?.text).toContain("runs")
  })

  it("separates being at the limit from being near it", () => {
    const got = describeQuotaPressure(usage({ run_count: 10, max_runs_per_period: 10 }))
    expect(got?.tone).toBe("reached")
    expect(got?.text).toContain("refused")
  })

  it("reports both limits together rather than only the first", () => {
    // A space at both limits has one problem, and two separate warnings invite
    // fixing only one of them.
    const got = describeQuotaPressure(
      usage({
        run_count: 9,
        max_runs_per_period: 10,
        total_tokens: 90_000,
        max_tokens_per_period: 100_000,
      })
    )
    expect(got?.tone).toBe("near")
    expect(got?.text).toContain("runs and tokens")
  })

  it("leads with the limit that is spent when only one of them is", () => {
    const got = describeQuotaPressure(
      usage({
        run_count: 5,
        max_runs_per_period: 10,
        total_tokens: 100_000,
        max_tokens_per_period: 100_000,
      })
    )
    expect(got?.tone).toBe("reached")
    expect(got?.text).toContain("tokens")
    expect(got?.text).not.toContain("runs")
  })

  it("names the window, so a rolling count is not read as a lifetime one", () => {
    const got = describeQuotaPressure(
      usage({ run_count: 9, max_runs_per_period: 10, period_days: 30 })
    )
    expect(got?.text).toContain("30-day")
  })
})

describe("describeQuotaPressure and artifact storage", () => {
  it("says nothing when the tier sets no storage limit", () => {
    // The case every deployment that seeded its tiers before storage was
    // measured is in: bytes held, no limit, nothing to report.
    expect(describeQuotaPressure(usage({ storage_bytes: 5_000_000 }))).toBeNull()
  })

  it("warns before artifacts start being refused", () => {
    const got = describeQuotaPressure(
      usage({ storage_bytes: 850, max_storage_bytes: 1000 })
    )
    expect(got?.tone).toBe("near")
    expect(got?.text).toContain("artifact storage")
  })

  // Storage does not free itself as the window moves, so its message must not
  // tell anyone to wait — deleting is the only remedy.
  it("tells someone at the limit to delete rather than to wait", () => {
    const got = describeQuotaPressure(
      usage({ storage_bytes: 1200, max_storage_bytes: 1000 })
    )
    expect(got?.tone).toBe("reached")
    expect(got?.text).toContain("deleted")
    expect(got?.text).not.toContain("window")
  })

  // A spent run quota is the more urgent problem and stays the headline, but
  // the storage message must never be the one carrying the run remedy.
  it("reports a spent rate limit ahead of storage", () => {
    const got = describeQuotaPressure(
      usage({
        run_count: 10,
        max_runs_per_period: 10,
        storage_bytes: 1200,
        max_storage_bytes: 1000,
      })
    )
    expect(got?.text).toContain("runs")
    expect(got?.text).toContain("window")
  })

  // A full space with comfortable rates must still be reported: folding storage
  // into the rate sentence would have hidden this case entirely.
  it("reports storage when the rates are fine", () => {
    const got = describeQuotaPressure(
      usage({
        run_count: 1,
        max_runs_per_period: 100,
        storage_bytes: 1000,
        max_storage_bytes: 1000,
      })
    )
    expect(got?.tone).toBe("reached")
    expect(got?.text).toContain("artifact storage")
  })
})
