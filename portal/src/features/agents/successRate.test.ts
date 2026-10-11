import { describe, expect, it } from "vitest"
import { formatSuccessRate, successRate } from "./successRate"

function repeat(status: string, n: number): string[] {
  return Array.from({ length: n }, () => status)
}

describe("successRate", () => {
  it("leaves stopped runs out", () => {
    // The audit's QA Writer: 14 done and 2 stopped is every run that finished
    // on its own succeeding, not 88%.
    const rate = successRate([...repeat("SUCCEEDED", 14), ...repeat("CANCELED", 2)])
    expect(formatSuccessRate(rate)).toBe("100%")
  })

  it("counts failed runs against the Agent", () => {
    expect(successRate([...repeat("SUCCEEDED", 3), "FAILED", "CANCELED"])).toBe(0.75)
  })

  it("has no rate when every finished run was stopped", () => {
    expect(successRate(repeat("CANCELED", 3))).toBeNull()
    expect(formatSuccessRate(successRate(repeat("CANCELED", 3)))).toBe("—")
  })

  it("has no rate before any run finished", () => {
    expect(successRate([])).toBeNull()
    expect(successRate(["RUNNING", "PENDING"])).toBeNull()
  })

  it("reads the server's status in either case", () => {
    expect(successRate(["succeeded", "failed"])).toBe(0.5)
    expect(successRate(["SUCCESS"])).toBe(1)
  })
})
