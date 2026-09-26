import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { UNAUTHORIZED_EVENT } from "./client"
import { clearAccessToken, setAccessToken } from "./session"
import { BuildMaxWebSocket } from "./ws"

/**
 * A browser WebSocket the test drives by hand. A refused upgrade reaches the
 * page as nothing but a close with code 1006 and no open before it, which is
 * exactly what `refuse` produces.
 */
class FakeSocket {
  static instances: FakeSocket[] = []
  static OPEN = 1
  readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: { code: number; reason: string; wasClean: boolean }) => void) | null = null
  onerror: ((err: unknown) => void) | null = null

  constructor(readonly url: string) {
    FakeSocket.instances.push(this)
  }

  get token(): string | null {
    return new URL(this.url).searchParams.get("token")
  }

  accept(): void {
    this.readyState = FakeSocket.OPEN
    this.onopen?.()
  }

  refuse(): void {
    this.readyState = 3
    this.onclose?.({ code: 1006, reason: "", wasClean: false })
  }

  send(): void {}

  close(): void {
    this.readyState = 3
  }
}

const SESSION_URL = "https://api.test/api/auth/portal/session"

function sessionResponse(status: number, body?: unknown): Response {
  return new Response(body === undefined ? "" : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

/** Let the socket's awaited refresh settle, then fire the reconnect timer. */
async function reconnect(): Promise<void> {
  await vi.runOnlyPendingTimersAsync()
  await vi.runOnlyPendingTimersAsync()
}

describe("BuildMaxWebSocket", () => {
  let seen: string[]

  beforeEach(() => {
    vi.useFakeTimers()
    seen = []
    FakeSocket.instances = []
    vi.stubGlobal("window", {
      __BUILDMAX_CONFIG__: { apiBase: "https://api.test" },
      location: { origin: "https://portal.test" },
      dispatchEvent: (event: Event) => {
        seen.push(event.type)
        return true
      },
    })
    vi.stubGlobal("WebSocket", FakeSocket)
    vi.spyOn(console, "log").mockImplementation(() => {})
    // A token that is nowhere near expiry: only the server knows it is dead,
    // which is what a rotated JWT secret leaves behind.
    setAccessToken("stale", Date.now() + 3_600_000)
  })

  afterEach(() => {
    clearAccessToken()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("refreshes before reconnecting after a refused upgrade", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => sessionResponse(200, { access_token: "fresh", expires_in: 900 }))
    vi.stubGlobal("fetch", fetchMock)
    const ws = new BuildMaxWebSocket()

    ws.connect("stale", "space-1")
    await vi.runOnlyPendingTimersAsync()
    expect(FakeSocket.instances[0].token).toBe("stale")
    expect(fetchMock).not.toHaveBeenCalled()

    FakeSocket.instances[0].refuse()
    await reconnect()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(String(fetchMock.mock.calls[0][0])).toBe(SESSION_URL)
    expect(FakeSocket.instances).toHaveLength(2)
    expect(FakeSocket.instances[1].token).toBe("fresh")
    ws.close()
  })

  it("renews once per run of failures, not on every retry", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => sessionResponse(200, { access_token: "fresh", expires_in: 900 }))
    vi.stubGlobal("fetch", fetchMock)
    const ws = new BuildMaxWebSocket()

    ws.connect("stale", "space-1")
    await vi.runOnlyPendingTimersAsync()
    FakeSocket.instances[0].refuse()
    await reconnect()
    // The server is down rather than refusing the token: further failures
    // retry on the backoff without rotating the session each time.
    FakeSocket.instances[1].refuse()
    await reconnect()
    FakeSocket.instances[2].refuse()
    await reconnect()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(FakeSocket.instances).toHaveLength(4)

    // A connection that opens ends the run: after it drops, the next refused
    // upgrade renews again.
    FakeSocket.instances[3].accept()
    FakeSocket.instances[3].refuse()
    await reconnect()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    FakeSocket.instances[4].refuse()
    await reconnect()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    ws.close()
  })

  it("signs out instead of retrying when the session itself is gone", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => sessionResponse(401))
    vi.stubGlobal("fetch", fetchMock)
    const ws = new BuildMaxWebSocket()

    ws.connect("stale", "space-1")
    await vi.runOnlyPendingTimersAsync()
    FakeSocket.instances[0].refuse()
    await reconnect()
    await vi.runAllTimersAsync()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(seen).toContain(UNAUTHORIZED_EVENT)
    expect(FakeSocket.instances).toHaveLength(1)
  })
})
