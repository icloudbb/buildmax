import { describe, expect, it } from "vitest"
import {
  WORKFLOW_FAILURE_CLASSES,
  ageSince,
  failureLabel,
  orderedFailures,
  timeUntil,
  waitingSummary,
} from "./runtime"

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

describe("timeUntil", () => {
  const now = "2026-09-28T12:00:00Z"

  it("measures ahead against the server clock", () => {
    expect(timeUntil("2026-09-28T14:30:00Z", now)).toBe("in 2 h 30 min")
  })

  it("calls a passed expiry overdue rather than a negative age", () => {
    expect(timeUntil("2026-09-28T11:00:00Z", now)).toBe("overdue")
  })

  it("has nothing to say when no expiry is set", () => {
    expect(timeUntil(undefined, now)).toBeNull()
  })
})

describe("workflow runtime", () => {
  it("summarizes waiting requests by kind and drops empty ones", () => {
    expect(waitingSummary({ question: 1, input: 2, other: 0 })).toBe("2 input requests, 1 Agent question")
    expect(waitingSummary({})).toBeNull()
    expect(waitingSummary(undefined)).toBeNull()
  })

  it("orders and labels Workflow failure classes apart from task run ones", () => {
    expect(
      orderedFailures({ run_deadline: 1, request_expired: 2, output_schema: 1 }, WORKFLOW_FAILURE_CLASSES),
    ).toEqual([
      ["output_schema", 1],
      ["request_expired", 2],
      ["run_deadline", 1],
    ])
    expect(failureLabel("request_declined", WORKFLOW_FAILURE_CLASSES)).toBe("Request declined")
    expect(failureLabel("request_declined")).toBe("request_declined")
  })
})
