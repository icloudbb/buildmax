import { describe, expect, it } from "vitest"
import { blockingGrantCount } from "./grantProblems"

describe("blockingGrantCount", () => {
  it("counts the grants that would fail a run, not the optional ones a run skips", () => {
    expect(
      blockingGrantCount([
        { secret_id: "sec_1", item: "TOKEN", problem: "disabled" },
        { secret_id: "sec_2", problem: "unavailable", optional: true },
        { secret_id: "sec_3", item: "KEY", problem: "item_missing" },
      ]),
    ).toBe(2)
  })

  it("is zero when the server reported nothing", () => {
    expect(blockingGrantCount(undefined)).toBe(0)
    expect(blockingGrantCount([])).toBe(0)
  })
})
