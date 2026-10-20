import type { Translate } from "@buildmax/gui"
import { describe, expect, it } from "vitest"
import { translate, type MessageKey } from "../../i18n"
import {
  FAILURE_CLASSES,
  WORKFLOW_FAILURE_CLASSES,
  ageSince,
  failureLabel,
  failureNextStep,
  orderedFailures,
  timeUntil,
  waitingSummary,
} from "./runtime"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)
const zh: Translate<MessageKey> = (key, vars) => translate("zh-CN", key, vars)

describe("ageSince", () => {
  const now = "2026-09-28T12:00:00Z"

  it("measures against the server clock", () => {
    expect(ageSince("2026-09-28T11:48:00Z", now, t)).toBe("12 min")
    expect(ageSince("2026-09-28T09:30:00Z", now, t)).toBe("2 h 30 min")
    expect(ageSince("2026-09-25T12:00:00Z", now, t)).toBe("3 d")
    expect(ageSince("2026-09-28T11:59:40Z", now, t)).toBe("under a minute")
  })

  it("has no age for nothing waiting", () => {
    expect(ageSince(undefined, now, t)).toBeNull()
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
    expect(failureLabel("space_configuration", t)).toBe("Space configuration")
    expect(failureLabel("mystery", t)).toBe("mystery")
  })

  // The operator relays a step, so it names who acts and what they check.
  it("gives every class a next step naming who acts", () => {
    expect(failureNextStep("space_configuration", t)).toBe(
      "Space owner: check the Agent's Secret grants and plugin activations. A spent usage quota is the operator's to raise, by changing the Space's tier.",
    )
    expect(failureNextStep("space_configuration", zh)).toMatch(/^Space 所有者：检查该 Agent 的密钥授权/)
    for (const classes of [FAILURE_CLASSES, WORKFLOW_FAILURE_CLASSES]) {
      for (const cls of Object.keys(classes)) {
        expect(failureNextStep(cls, t, classes)).toMatch(/^[^:]+: \S/)
        expect(failureNextStep(cls, zh, classes)).toMatch(/^[^：]+：\S/)
      }
    }
  })

  it("steps a class this build does not know as unclassified", () => {
    expect(failureNextStep("mystery", t)).toBe(failureNextStep("unclassified", t))
  })
})

describe("timeUntil", () => {
  const now = "2026-09-28T12:00:00Z"

  it("measures ahead against the server clock", () => {
    expect(timeUntil("2026-09-28T14:30:00Z", now, t)).toBe("in 2 h 30 min")
  })

  it("calls a passed expiry overdue rather than a negative age", () => {
    expect(timeUntil("2026-09-28T11:00:00Z", now, t)).toBe("overdue")
  })

  it("has nothing to say when no expiry is set", () => {
    expect(timeUntil(undefined, now, t)).toBeNull()
  })
})

describe("workflow runtime", () => {
  it("summarizes waiting requests by kind and drops empty ones", () => {
    expect(waitingSummary({ question: 1, input: 2, other: 0 }, t)).toBe("2 input requests, 1 Agent question")
    expect(waitingSummary({}, t)).toBeNull()
    expect(waitingSummary(undefined, t)).toBeNull()
  })

  it("orders and labels Workflow failure classes apart from task run ones", () => {
    expect(
      orderedFailures({ run_deadline: 1, request_expired: 2, output_schema: 1 }, WORKFLOW_FAILURE_CLASSES),
    ).toEqual([
      ["output_schema", 1],
      ["request_expired", 2],
      ["run_deadline", 1],
    ])
    expect(failureLabel("request_declined", t, WORKFLOW_FAILURE_CLASSES)).toBe("Request declined")
    expect(failureLabel("request_declined", t)).toBe("request_declined")
  })
})
