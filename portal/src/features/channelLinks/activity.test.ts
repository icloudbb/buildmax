import { describe, expect, it } from "vitest"
import { chatLinkActivity } from "./activity"

describe("chatLinkActivity", () => {
  const now = Date.parse("2026-10-04T00:00:00Z")

  it("is active until the deadline", () => {
    expect(chatLinkActivity("2026-12-01T00:00:00Z", now)).toEqual({
      state: "active",
      until: new Date("2026-12-01T00:00:00Z"),
    })
  })

  it("lapses once the deadline has passed", () => {
    expect(chatLinkActivity("2026-10-03T23:59:59Z", now)).toEqual({ state: "lapsed" })
  })

  it("says nothing without a usable deadline", () => {
    expect(chatLinkActivity(undefined, now)).toBeNull()
    expect(chatLinkActivity("not a date", now)).toBeNull()
  })
})
