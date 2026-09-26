import { ensureAccessToken, getApiBase, refreshAccessToken, UNAUTHORIZED_EVENT } from "./client"
import { currentAccessToken } from "./session"

export interface WsEnvelope {
  type: string
  payload: unknown
}

// `any` rather than `unknown`: handlers are registered per event type with a
// concrete payload type, and a parameter typed `unknown` would reject every one
// of them under contravariance.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type EventHandler = (payload: any) => void

/**
 * Dispatched to `on` handlers when the socket opens, including every reconnect.
 *
 * A socket that was down missed whatever the server broadcast while it was, so
 * a view holding server state reloads it here rather than trusting what it last
 * saw. It is a local event: no server message carries this type.
 */
export const SOCKET_OPEN_EVENT = "socket.open"

const RECONNECT_MIN = 1000
const RECONNECT_MAX = 30000
const STABLE_CONNECTION_MS = 10000

/**
 * The absolute ws(s):// origin to open the socket against.
 *
 * An absolute http(s) API base becomes its ws(s) form directly. A same-origin
 * base — "" or a relative path, the reverse-proxy setup where the Portal and
 * server share a hostname — has no scheme to swap, so the origin comes from
 * `window.location`: a WebSocket URL must be absolute, unlike a fetch path.
 */
function wsBaseFrom(httpBase: string): string {
  if (/^https?:\/\//.test(httpBase)) {
    return httpBase.replace(/^http/, "ws")
  }
  return window.location.origin.replace(/^http/, "ws")
}

/**
 * BuildMaxWebSocket manages a persistent WebSocket connection to the server.
 * Supports typed events, auto-reconnect with exponential backoff.
 */
export class BuildMaxWebSocket {
  private ws: WebSocket | null = null
  private token: string | null = null
  private spaceId: string | null = null
  private handlers = new Map<string, Set<EventHandler>>()
  private intentionalClose = false
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = RECONNECT_MIN
  private connectedAt = 0
  private sendQueue: string[] = []
  // Bumped by connect() and close(), so an open that awaited a refresh, or a
  // replaced socket's late close event, can tell it no longer speaks for this
  // connection.
  private generation = 0
  // A socket that closed without ever opening may have been refused at the
  // upgrade, which the browser reports as nothing more than an abnormal close.
  // The next attempt renews first, once per run of failures: an outage looks
  // the same, and renewing on every retry would rotate the session for nothing.
  private renewBeforeOpen = false
  private renewedSinceOpen = false

  /** Lifecycle callback: called when the connection opens (or re-opens). */
  onOpen: (() => void) | null = null
  /** Lifecycle callback: called when the connection closes unexpectedly. */
  onClose: (() => void) | null = null

  connect(token: string, spaceId?: string | null): void {
    this.generation++
    this.token = token
    this.spaceId = spaceId ?? null
    this.intentionalClose = false
    this.openSocket()
  }

  private openSocket(): void {
    void this.openSocketWithFreshToken()
  }

  /**
   * Open the socket, refreshing the access token first if it is due.
   *
   * The token is not the one connect() was handed. A socket outlives its
   * token: this one reconnects for as long as the tab is open, and the
   * upgrade is the only moment the server checks. Reconnecting with the token
   * from the original connect() would work until it expired and then fail
   * every attempt, which reads as "the server is down" rather than "sign in
   * again". Expiry is not the only way a token dies — a rotated JWT secret
   * refuses one that still looks valid — so a socket that never opened also
   * renews before the next attempt.
   */
  private async openSocketWithFreshToken(): Promise<void> {
    if (!this.token) return
    if (!this.spaceId) return

    const generation = this.generation
    let token: string | null
    if (this.renewBeforeOpen) {
      this.renewBeforeOpen = false
      this.renewedSinceOpen = true
      token = await refreshAccessToken()
      if (generation !== this.generation) return
      if (!token && currentAccessToken() === null) {
        // The server refused the refresh cookie too and the session is gone;
        // retrying the socket would only be refused again.
        window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT))
        return
      }
    } else {
      token = await ensureAccessToken()
      // close() or connect() may have been called while the refresh was in
      // flight.
      if (generation !== this.generation) return
    }
    token = token ?? this.token
    this.token = token

    const wsBase = wsBaseFrom(getApiBase())
    const params = new URLSearchParams({ token })
    const url = `${wsBase}/api/spaces/${encodeURIComponent(this.spaceId)}/ws?${params.toString()}`

    console.log("[ws] connecting", wsBase)
    this.ws = new WebSocket(url)
    let opened = false

    this.ws.onopen = () => {
      console.log("[ws] connected")
      opened = true
      this.renewedSinceOpen = false
      this.connectedAt = Date.now()
      this.reconnectDelay = RECONNECT_MIN
      this.flushQueue()
      this.onOpen?.()
      this.dispatch(SOCKET_OPEN_EVENT, {})
    }

    this.ws.onmessage = (event) => {
      try {
        const env = JSON.parse(event.data as string) as WsEnvelope
        console.debug("[ws] recv", env.type, env.payload)
        this.dispatch(env.type, env.payload)
      } catch {
        console.warn("[ws] recv unparseable message", event.data)
      }
    }

    this.ws.onclose = (event) => {
      console.log("[ws] closed", { code: event.code, reason: event.reason, wasClean: event.wasClean })
      if (generation !== this.generation) return
      if (event.code === 4001 || event.code === 1008) {
        window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT))
        return
      }
      if (!opened && !this.renewedSinceOpen) {
        this.renewBeforeOpen = true
      }
      if (!this.intentionalClose) {
        this.onClose?.()
        this.scheduleReconnect()
      }
    }

    this.ws.onerror = (err) => {
      console.warn("[ws] error", err)
    }
  }

  send(type: string, payload: unknown): void {
    const msg = JSON.stringify({ type, payload })
    if (this.ws?.readyState === WebSocket.OPEN) {
      console.debug("[ws] send", type, payload)
      this.ws.send(msg)
    } else {
      console.debug("[ws] queued (not open)", type, payload)
      this.sendQueue.push(msg)
    }
  }

  on(type: string, cb: EventHandler): void {
    let set = this.handlers.get(type)
    if (!set) {
      set = new Set()
      this.handlers.set(type, set)
    }
    set.add(cb)
  }

  off(type: string, cb: EventHandler): void {
    this.handlers.get(type)?.delete(cb)
  }

  close(): void {
    console.log("[ws] closing (intentional)")
    this.generation++
    this.intentionalClose = true
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws?.close(1000)
    this.ws = null
    this.sendQueue = []
  }

  get connected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  private dispatch(type: string, payload: unknown): void {
    const set = this.handlers.get(type)
    if (set) {
      for (const cb of set) {
        try {
          cb(payload)
        } catch {
          // handler error; ignore to keep dispatching
        }
      }
    }
  }

  private flushQueue(): void {
    while (this.sendQueue.length > 0 && this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(this.sendQueue.shift()!)
    }
  }

  private scheduleReconnect(): void {
    if (this.intentionalClose) return
    if (this.reconnectTimer) return

    const wasStable = Date.now() - this.connectedAt > STABLE_CONNECTION_MS
    if (wasStable) {
      this.reconnectDelay = RECONNECT_MIN
    }

    console.log("[ws] reconnecting in", this.reconnectDelay, "ms")
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      console.log("[ws] reconnecting now")
      this.openSocket()
    }, this.reconnectDelay)

    this.reconnectDelay = Math.min(this.reconnectDelay * 2, RECONNECT_MAX)
  }
}
