import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  apiFetch,
  ensureAccessToken,
  refreshAccessToken,
  TOKEN_REFRESHED_EVENT,
  UNAUTHORIZED_EVENT,
} from "./client"
import { clearAccessToken, currentAccessToken, setAccessToken } from "./session"

/** Collects events the way AuthContext listens for them. */
function captureEvents() {
  const seen: string[] = []
  const listeners = new Map<string, Set<(e: Event) => void>>()
  const target = {
    addEventListener: (type: string, cb: (e: Event) => void) => {
      const set = listeners.get(type) ?? new Set()
      set.add(cb)
      listeners.set(type, set)
    },
    removeEventListener: (type: string, cb: (e: Event) => void) => {
      listeners.get(type)?.delete(cb)
    },
    dispatchEvent: (event: Event) => {
      seen.push(event.type)
      listeners.get(event.type)?.forEach((cb) => cb(event))
      return true
    },
  }
  return { seen, target }
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

const SESSION_URL = "/api/auth/portal/session"

describe("token refresh", () => {
  let events: ReturnType<typeof captureEvents>

  beforeEach(() => {
    events = captureEvents()
    vi.stubGlobal("window", { ...events.target, __BUILDMAX_CONFIG__: { apiBase: "https://api.test" } })
    // The access token lives in module memory; reset it before each test.
    setAccessToken("access-1", Date.now() + 3_600_000)
  })

  afterEach(() => {
    clearAccessToken()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("refreshes and replays the request when a call comes back 401", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockResolvedValueOnce(jsonResponse(200, { access_token: "access-2", expires_in: 604800 }))
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }))
    vi.stubGlobal("fetch", fetchMock)

    const res = await apiFetch("https://api.test/api/spaces", {
      headers: { Authorization: "Bearer access-1" },
    })

    expect(res.status).toBe(200)
    expect(fetchMock).toHaveBeenCalledTimes(3)

    // The session exchange sends the cookie, no body, no bearer.
    const sessionInit = fetchMock.mock.calls[1][1] as RequestInit
    expect(String(fetchMock.mock.calls[1][0])).toContain(SESSION_URL)
    expect(sessionInit.credentials).toBe("include")
    expect(sessionInit.body).toBeUndefined()

    // The replay carries the new token, not the one the caller passed.
    const replayInit = fetchMock.mock.calls[2][1] as RequestInit
    expect(new Headers(replayInit.headers).get("Authorization")).toBe("Bearer access-2")

    // The new access token is held in memory.
    expect(currentAccessToken()).toBe("access-2")
    expect(events.seen).toContain(TOKEN_REFRESHED_EVENT)
    expect(events.seen).not.toContain(UNAUTHORIZED_EVENT)
  })

  // The reason single-flight exists. A page mounting several requests at once
  // gets several 401s in the same tick; if each hit the session endpoint, the
  // server would see the rotating refresh cookie presented repeatedly and —
  // correctly — revoke the session as replayed.
  it("shares one exchange between concurrent callers", async () => {
    // The stale token is refused; the rotated one is accepted. That is all the
    // server needs to do for this test to be about the client's coordination.
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (String(url).endsWith(SESSION_URL)) {
        return Promise.resolve(jsonResponse(200, { access_token: "access-2", expires_in: 604800 }))
      }
      const auth = new Headers(init?.headers).get("Authorization")
      return Promise.resolve(
        auth === "Bearer access-2"
          ? jsonResponse(200, { ok: true })
          : new Response("", { status: 401 })
      )
    })
    vi.stubGlobal("fetch", fetchMock)

    await Promise.all([
      apiFetch("https://api.test/api/spaces", { headers: { Authorization: "Bearer access-1" } }),
      apiFetch("https://api.test/api/spaces", { headers: { Authorization: "Bearer access-1" } }),
      apiFetch("https://api.test/api/spaces", { headers: { Authorization: "Bearer access-1" } }),
    ])

    const sessionCalls = fetchMock.mock.calls.filter((c) => String(c[0]).endsWith(SESSION_URL))
    expect(sessionCalls).toHaveLength(1)
  })

  it("ends the session when the server rejects the exchange", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockResolvedValueOnce(jsonResponse(401, { error: "no session" }))
    vi.stubGlobal("fetch", fetchMock)

    const res = await apiFetch("https://api.test/api/spaces", {
      headers: { Authorization: "Bearer access-1" },
    })

    expect(res.status).toBe(401)
    // On 401 the server has cleared the cookie; the in-memory token goes too.
    expect(currentAccessToken()).toBeNull()
    expect(events.seen).toContain(UNAUTHORIZED_EVENT)
  })

  // Being offline is not the same as being signed out. On a deployment where
  // signing back in means asking an operator for a login code, discarding a
  // usable session because the network blipped is an expensive mistake.
  it("keeps the token when the exchange cannot reach the server", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
    vi.stubGlobal("fetch", fetchMock)

    await apiFetch("https://api.test/api/spaces", {
      headers: { Authorization: "Bearer access-1" },
    })

    // A network error is not a dead session: the token stays.
    expect(currentAccessToken()).toBe("access-1")
  })

  it("leaves an unauthenticated request alone", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("", { status: 401 }))
    vi.stubGlobal("fetch", fetchMock)

    await apiFetch("https://api.test/api/auth/portal/login", { method: "POST" })

    // One call, no refresh: a login that fails is not a session that expired.
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(currentAccessToken()).toBe("access-1")
  })

  it("exchanges the refresh cookie for a new access token", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { access_token: "access-2", expires_in: 604800 }))
    vi.stubGlobal("fetch", fetchMock)

    expect(await refreshAccessToken()).toBe("access-2")
    expect(currentAccessToken()).toBe("access-2")
    expect(String(fetchMock.mock.calls[0][0])).toContain(SESSION_URL)
  })
})

describe("ensureAccessToken", () => {
  let events: ReturnType<typeof captureEvents>

  beforeEach(() => {
    events = captureEvents()
    vi.stubGlobal("window", { ...events.target, __BUILDMAX_CONFIG__: { apiBase: "https://api.test" } })
  })

  afterEach(() => {
    clearAccessToken()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("hands back the current token while it is still good", async () => {
    setAccessToken("access-1", Date.now() + 3_600_000)
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    expect(await ensureAccessToken()).toBe("access-1")
    expect(fetchMock).not.toHaveBeenCalled()
  })

  // The WebSocket asks in advance because a rejected upgrade tells it nothing.
  it("refreshes before the deadline rather than at it", async () => {
    // Inside the skew window: still valid, but not for long enough to open a
    // connection that is meant to last.
    setAccessToken("access-1", Date.now() + 5_000)
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse(200, { access_token: "access-2", expires_in: 604800 }))
    vi.stubGlobal("fetch", fetchMock)

    expect(await ensureAccessToken()).toBe("access-2")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  // A token with no recorded expiry still has to work.
  it("uses a token of unknown expiry rather than refusing it", async () => {
    setAccessToken("access-1", null)
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    expect(await ensureAccessToken()).toBe("access-1")
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
