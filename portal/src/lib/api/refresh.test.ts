import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  apiFetch,
  ensureAccessToken,
  refreshAccessToken,
  TOKEN_REFRESHED_EVENT,
  UNAUTHORIZED_EVENT,
} from "./client"
import { currentAccessToken, currentRefreshToken, writeSession } from "./session"

/** A localStorage that behaves like the real one, for a node test environment. */
function stubStorage(): Storage {
  const map = new Map<string, string>()
  return {
    get length() {
      return map.size
    },
    clear: () => map.clear(),
    getItem: (k: string) => map.get(k) ?? null,
    key: (i: number) => [...map.keys()][i] ?? null,
    removeItem: (k: string) => void map.delete(k),
    setItem: (k: string, v: string) => void map.set(k, v),
  } as Storage
}

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

describe("token refresh", () => {
  let storage: Storage
  let events: ReturnType<typeof captureEvents>

  beforeEach(() => {
    storage = stubStorage()
    events = captureEvents()
    vi.stubGlobal("localStorage", storage)
    vi.stubGlobal("window", { ...events.target, __BUILDMAX_CONFIG__: { apiBase: "https://api.test" } })
    writeSession({
      accessToken: "access-1",
      refreshToken: "bmxrefresh_1",
      expiresAt: Date.now() + 3_600_000,
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("refreshes and replays the request when a call comes back 401", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockResolvedValueOnce(
        jsonResponse(200, {
          access_token: "access-2",
          refresh_token: "bmxrefresh_2",
          expires_in: 604800,
        })
      )
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }))
    vi.stubGlobal("fetch", fetchMock)

    const res = await apiFetch("https://api.test/api/spaces", {
      headers: { Authorization: "Bearer access-1" },
    })

    expect(res.status).toBe(200)
    expect(fetchMock).toHaveBeenCalledTimes(3)

    // The replay carries the new token, not the one the caller passed.
    const replayInit = fetchMock.mock.calls[2][1] as RequestInit
    expect(new Headers(replayInit.headers).get("Authorization")).toBe("Bearer access-2")

    // Both halves of the rotated pair are stored.
    expect(currentAccessToken()).toBe("access-2")
    expect(currentRefreshToken()).toBe("bmxrefresh_2")
    expect(events.seen).toContain(TOKEN_REFRESHED_EVENT)
    expect(events.seen).not.toContain(UNAUTHORIZED_EVENT)
  })

  // The reason single-flight exists. A page mounting several requests at once
  // gets several 401s in the same tick; if each exchanged the refresh token,
  // the server would see the same token presented repeatedly and — correctly —
  // revoke the session as replayed.
  it("shares one exchange between concurrent callers", async () => {
    // The stale token is refused; the rotated one is accepted. That is all the
    // server needs to do for this test to be about the client's coordination.
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (String(url).endsWith("/api/auth/token/refresh")) {
        return Promise.resolve(
          jsonResponse(200, {
            access_token: "access-2",
            refresh_token: "bmxrefresh_2",
            expires_in: 604800,
          })
        )
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

    const refreshCalls = fetchMock.mock.calls.filter((c) =>
      String(c[0]).endsWith("/api/auth/token/refresh")
    )
    expect(refreshCalls).toHaveLength(1)
  })

  it("ends the session when the server rejects the refresh token", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockResolvedValueOnce(jsonResponse(401, { error: "invalid refresh token" }))
    vi.stubGlobal("fetch", fetchMock)

    const res = await apiFetch("https://api.test/api/spaces", {
      headers: { Authorization: "Bearer access-1" },
    })

    expect(res.status).toBe(401)
    expect(currentAccessToken()).toBeNull()
    expect(currentRefreshToken()).toBeNull()
    expect(events.seen).toContain(UNAUTHORIZED_EVENT)
  })

  // Being offline is not the same as being signed out. On a deployment where
  // signing back in means asking an operator for a login code, discarding a
  // usable session because the network blipped is an expensive mistake.
  it("keeps the session when the refresh call cannot reach the server", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response("", { status: 401 }))
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
    vi.stubGlobal("fetch", fetchMock)

    await apiFetch("https://api.test/api/spaces", {
      headers: { Authorization: "Bearer access-1" },
    })

    expect(currentRefreshToken()).toBe("bmxrefresh_1")
  })

  it("leaves an unauthenticated request alone", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response("", { status: 401 }))
    vi.stubGlobal("fetch", fetchMock)

    await apiFetch("https://api.test/api/auth/login", { method: "POST" })

    // One call, no refresh: a login that fails is not a session that expired.
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(currentRefreshToken()).toBe("bmxrefresh_1")
  })

  it("does nothing when there is no refresh token to exchange", async () => {
    writeSession({ accessToken: "access-1", refreshToken: null, expiresAt: null })
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    expect(await refreshAccessToken()).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe("ensureAccessToken", () => {
  let events: ReturnType<typeof captureEvents>

  beforeEach(() => {
    events = captureEvents()
    vi.stubGlobal("localStorage", stubStorage())
    vi.stubGlobal("window", { ...events.target, __BUILDMAX_CONFIG__: { apiBase: "https://api.test" } })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("hands back the stored token while it is still good", async () => {
    writeSession({
      accessToken: "access-1",
      refreshToken: "bmxrefresh_1",
      expiresAt: Date.now() + 3_600_000,
    })
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    expect(await ensureAccessToken()).toBe("access-1")
    expect(fetchMock).not.toHaveBeenCalled()
  })

  // The WebSocket asks in advance because a rejected upgrade tells it nothing.
  it("refreshes before the deadline rather than at it", async () => {
    writeSession({
      accessToken: "access-1",
      refreshToken: "bmxrefresh_1",
      // Inside the skew window: still valid, but not for long enough to open a
      // connection that is meant to last.
      expiresAt: Date.now() + 5_000,
    })
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        access_token: "access-2",
        refresh_token: "bmxrefresh_2",
        expires_in: 604800,
      })
    )
    vi.stubGlobal("fetch", fetchMock)

    expect(await ensureAccessToken()).toBe("access-2")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  // A session stored before expiry was recorded still has to work.
  it("uses a token of unknown expiry rather than refusing it", async () => {
    writeSession({ accessToken: "access-1", refreshToken: "bmxrefresh_1", expiresAt: null })
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    expect(await ensureAccessToken()).toBe("access-1")
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
