import { describe, expect, it } from "vitest"
import type { AccountSession } from "./api"
import { lastSeenAt, orderSessions, sessionMethodKey, sessionPlatformKey } from "./model"

function session(id: string, current = false, lastSeen = "0001-01-01T00:00:00Z"): AccountSession {
  return {
    session_id: id,
    platform: "cli",
    created_at: "2026-10-11T08:00:00Z",
    last_seen_at: lastSeen,
    expires_at: "2026-10-11T20:00:00Z",
    current,
  }
}

describe("sessionPlatformKey", () => {
  it("names each surface and does not guess an unknown one", () => {
    expect(sessionPlatformKey("portal")).toBe("account.sessions.platform.portal")
    expect(sessionPlatformKey("cli")).toBe("account.sessions.platform.cli")
    expect(sessionPlatformKey("desktop")).toBe("account.sessions.platform.desktop")
    expect(sessionPlatformKey("")).toBe("account.sessions.platform.unknown")
    expect(sessionPlatformKey(undefined)).toBe("account.sessions.platform.unknown")
  })
})

describe("sessionMethodKey", () => {
  it("names the known sign-in methods only", () => {
    expect(sessionMethodKey("oidc")).toBe("account.sessions.method.oidc")
    expect(sessionMethodKey("login_code")).toBe("account.sessions.method.loginCode")
    expect(sessionMethodKey("password")).toBe("account.sessions.method.password")
    expect(sessionMethodKey("something")).toBeNull()
  })
})

describe("lastSeenAt", () => {
  it("treats the zero time as never used", () => {
    expect(lastSeenAt(session("a"))).toBeNull()
    expect(lastSeenAt(session("a", false, "2026-10-11T09:00:00Z"))).toBe("2026-10-11T09:00:00Z")
    // An administrator's session row has the same field.
    expect(lastSeenAt({ last_seen_at: "0001-01-01T00:00:00Z" })).toBeNull()
    expect(lastSeenAt({})).toBeNull()
  })
})

describe("orderSessions", () => {
  it("puts this browser first and keeps the rest in server order", () => {
    const ordered = orderSessions([session("new"), session("mine", true), session("old")])
    expect(ordered.map((s) => s.session_id)).toEqual(["mine", "new", "old"])
  })
})
