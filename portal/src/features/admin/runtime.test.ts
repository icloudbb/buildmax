import { describe, expect, it } from "vitest"
import { ageSince, failureLabel, orderedFailures } from "./runtime"

describe("ageSince", () => {
  const now = "2026-09-28T12:00:00Z"

  it("measures against the server clock", () => {
    expect(ageSince("2026-09-28T11:48:00Z", now)).toBe("12 min")
    expect(ageSince("2026-09-28T09:30:00Z", now)).toBe("2 h 30 min")
    expect(ageSince("2026-09-25T12:00:00Z", now)).toBe("3 d")
    expect(ageSince("2026-09-28T11:59:40Z", now)).toBe("under a minute")
  })

  it("has no age for nothing waiting", () => {
    expect(ageSince(undefined, now)).toBeNull()
  })
})

describe("failure classes", () => {
  it("orders by the server's classes and drops empty counts", () => {
    expect(orderedFailures({ run: 2, infrastructure: 1, dispatch: 0, mystery: 4 })).toEqual([
      ["infrastructure", 1],
      ["run", 2],
      ["mystery", 4],
    ])
  })

  it("shows an unknown class verbatim", () => {
    expect(failureLabel("space_configuration")).toBe("Space configuration")
    expect(failureLabel("mystery")).toBe("mystery")
  })
})
