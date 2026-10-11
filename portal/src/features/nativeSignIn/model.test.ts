import { describe, expect, it } from "vitest"
import { ApiRequestError } from "../../lib/api/client"
import { decisionFailure, displayCode, lookUpFailure, sessionEndsSoon, type SignInPhase } from "./model"

const request: Extract<SignInPhase, { kind: "request" }> = {
  kind: "request",
  code: "WDJB-MJHT",
  request: {
    platform: "cli",
    client_label: "alice-mbp · macOS",
    created_at: "2026-03-04T15:25:00Z",
    expires_at: "2026-03-04T15:35:00Z",
    session_expires_at: "2026-03-05T03:25:00Z",
  },
  endsSoon: false,
  deciding: "confirm",
  error: null,
}

describe("displayCode", () => {
  it("shows the code as the client does, whatever was typed", () => {
    expect(displayCode("wdjbmjht")).toBe("WDJB-MJHT")
    expect(displayCode(" wdjb-mjht ")).toBe("WDJB-MJHT")
    expect(displayCode("WDJB MJHT")).toBe("WDJB-MJHT")
  })

  it("leaves a code of another length as typed, upper-cased", () => {
    expect(displayCode("abc")).toBe("ABC")
  })
})

describe("sessionEndsSoon", () => {
  const now = Date.parse("2026-03-04T15:30:00Z")

  it("is true under an hour from the end of the Portal session", () => {
    expect(sessionEndsSoon("2026-03-04T16:29:59Z", now)).toBe(true)
  })

  it("is false with an hour or more left", () => {
    expect(sessionEndsSoon("2026-03-04T16:30:00Z", now)).toBe(false)
  })

  it("is false for an instant it cannot read", () => {
    expect(sessionEndsSoon("not a time", now)).toBe(false)
  })
})

describe("lookUpFailure", () => {
  it("is one not-valid state for a wrong, expired, or decided code", () => {
    expect(lookUpFailure(new ApiRequestError("sign-in code is not valid", 404), "X", "fallback")).toEqual({
      kind: "invalid",
    })
  })

  it("gives the throttled 429 its own state", () => {
    expect(lookUpFailure(new ApiRequestError("too many", 429), "X", "fallback")).toEqual({ kind: "throttled" })
  })

  it("keeps the code to retry anything else", () => {
    expect(lookUpFailure(new ApiRequestError("unavailable", 503), "X", "fallback")).toEqual({
      kind: "failed",
      code: "X",
      message: "unavailable",
    })
    expect(lookUpFailure("boom", "X", "fallback")).toEqual({ kind: "failed", code: "X", message: "fallback" })
  })
})

describe("decisionFailure", () => {
  it("replaces the request when its code stopped being valid", () => {
    expect(decisionFailure(request, new ApiRequestError("gone", 404), "fallback")).toEqual({ kind: "invalid" })
    expect(decisionFailure(request, new ApiRequestError("slow", 429), "fallback")).toEqual({ kind: "throttled" })
  })

  it("keeps the request, undecided, with a refusal that may still be fixed", () => {
    const next = decisionFailure(
      request,
      new ApiRequestError("sign in to the Portal again to confirm this sign-in", 403),
      "fallback"
    )
    expect(next).toEqual({
      ...request,
      deciding: null,
      error: { message: "sign in to the Portal again to confirm this sign-in", reauthenticate: true },
    })
  })

  it("does not offer a fresh sign-in for a failure it would not fix", () => {
    const next = decisionFailure(request, new ApiRequestError("unavailable", 503), "fallback")
    expect(next).toMatchObject({ kind: "request", deciding: null, error: { reauthenticate: false } })
  })
})
