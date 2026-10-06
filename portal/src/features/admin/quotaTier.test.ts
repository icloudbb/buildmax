import type { Translate } from "@buildmax/gui"
import { describe, expect, it } from "vitest"
import { translate, type MessageKey } from "../../i18n"
import { describeTierLimits } from "./quotaTier"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

describe("describeTierLimits", () => {
  it("names the rates and the period", () => {
    expect(
      describeTierLimits(
        {
          tier_name: "pro",
          max_runs_per_period: 1000,
          max_tokens_per_period: 10_000_000,
          max_storage_bytes: 0,
          period_days: 30,
        },
        t,
      ),
    ).toBe("1,000 runs, 10,000,000 tokens per 30 days; unlimited storage")
  })

  // Storage is a stock, not a rate, so it is never "per period".
  it("names a storage cap apart from the period", () => {
    expect(
      describeTierLimits(
        {
          tier_name: "capped",
          max_runs_per_period: 10,
          max_tokens_per_period: 100_000,
          max_storage_bytes: 1_048_576,
          period_days: 7,
        },
        t,
      ),
    ).toBe("10 runs, 100,000 tokens per 7 days; 1,048,576 bytes of storage")
  })
})
