/**
 * API transport: base URL, 401 handling, and response helpers.
 * No DTOs or UI types.
 */

import {
  accessTokenExpiresAt,
  clearAccessToken,
  currentAccessToken,
  expiresAtFrom,
  setAccessToken,
} from "./session"

const defaultApiBase = "http://localhost:5678"

declare global {
  interface Window {
    /**
     * Runtime configuration. `public/config.js` ships an empty default and the
     * container entrypoint overwrites it from BUILDMAX_API_BASE before nginx
     * starts, which is what lets one published image serve every deployment:
     * the API URL is not knowable when the bundle is built.
     */
    __BUILDMAX_CONFIG__?: { apiBase?: string }
  }
}

/** Event dispatched when a call is unauthorized and refreshing did not help. Listeners should clear auth and show login. */
export const UNAUTHORIZED_EVENT = "buildmax:unauthorized"

/** Event dispatched after a successful refresh, carrying the new access token. */
export const TOKEN_REFRESHED_EVENT = "buildmax:token-refreshed"

export function checkUnauthorized(res: Response): void {
  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT))
  }
}

/**
 * Refresh in flight, shared by every caller.
 *
 * An access token expiring is not one request's problem: a dashboard mounts a
 * dozen at once, and they all get 401 within the same tick. Without this they
 * would each hit the session endpoint, and the server — which rotates the
 * refresh cookie on every exchange — would read the second one as a replayed
 * credential and revoke the session. One refresh, one rotation, everyone waits
 * for it.
 */
let refreshInFlight: Promise<string | null> | null = null

/** Refresh the access token, sharing one exchange between concurrent callers. */
export function refreshAccessToken(): Promise<string | null> {
  if (refreshInFlight) return refreshInFlight
  refreshInFlight = exchangePortalSession().finally(() => {
    refreshInFlight = null
  })
  return refreshInFlight
}

/**
 * Trade the refresh cookie for a fresh access token at the Portal session
 * endpoint. No body: the credential is the HttpOnly cookie the browser sends
 * with `credentials: "include"`, which the server rotates on each call.
 */
async function exchangePortalSession(): Promise<string | null> {
  let res: Response
  try {
    res = await fetch(`${getApiBase()}/api/auth/portal/session`, {
      method: "POST",
      credentials: "include",
    })
  } catch {
    // The network is down, not the session. The cookie the server holds is
    // untouched, so coming back online resumes where it left off instead of at
    // the login form — which, on this deployment, would mean asking for a
    // login code.
    return null
  }

  if (!res.ok) {
    // 401 is the server saying the session is dead — spent, revoked, or gone —
    // and it has already cleared the cookie. Any other status is the server's
    // problem, not the session's, but there is no token to hand back either
    // way.
    if (res.status === 401) clearAccessToken()
    return null
  }

  const body = (await res.json()) as {
    access_token?: string
    expires_in?: number
  }
  if (!body.access_token) return null
  setAccessToken(body.access_token, expiresAtFrom(body.expires_in))
  window.dispatchEvent(
    new CustomEvent(TOKEN_REFRESHED_EVENT, { detail: { accessToken: body.access_token } })
  )
  return body.access_token
}

/**
 * The access token to use now, refreshed first if it is at or near expiry.
 *
 * HTTP calls do not need this — they discover expiry from a 401 and retry. A
 * WebSocket cannot: the server checks the token once, at the upgrade, and a
 * rejected upgrade arrives as a close event with nothing to read. So the
 * socket asks in advance.
 */
export async function ensureAccessToken(): Promise<string | null> {
  const token = currentAccessToken()
  const expiresAt = accessTokenExpiresAt()
  if (!token) return null
  // Unknown expiry means a session stored before this existed. Use it and let
  // a 401 sort it out.
  if (expiresAt === null) return token
  if (Date.now() < expiresAt - EXPIRY_SKEW_MS) return token
  return (await refreshAccessToken()) ?? null
}

/** Refresh this long before the deadline, so a request in flight does not cross it. */
const EXPIRY_SKEW_MS = 60_000

function authorizationOf(init: RequestInit | undefined): string | null {
  const headers = new Headers(init?.headers)
  return headers.get("Authorization")
}

function withBearer(init: RequestInit | undefined, token: string): RequestInit {
  const headers = new Headers(init?.headers)
  headers.set("Authorization", `Bearer ${token}`)
  return { ...init, headers }
}

/**
 * Fetch, and on 401 refresh once and replay.
 *
 * The Authorization header is rewritten from the in-memory token rather than
 * used as passed. Callers take a token as an argument and hold it in React
 * state, which goes stale the moment any other call refreshes; reading the
 * current one here means that staleness never reaches the server.
 *
 * A request with no Authorization header is left alone — an unauthenticated
 * endpoint answering 401 is not a session problem.
 */
export async function apiFetch(url: string, init?: RequestInit): Promise<Response> {
  // The in-memory token wins, but only when there is one — before hydration
  // there is none, and a caller's header is left as passed rather than dropped.
  const current = currentAccessToken()
  const sent = authorizationOf(init) && current ? withBearer(init, current) : init
  const res = await fetch(url, sent)
  if (res.status !== 401 || !authorizationOf(sent)) {
    if (res.status === 401) checkUnauthorized(res)
    return res
  }

  const refreshed = await refreshAccessToken()
  if (!refreshed) {
    checkUnauthorized(res)
    return res
  }
  const retried = await fetch(url, withBearer(init, refreshed))
  checkUnauthorized(retried)
  return retried
}

/** Parse response body to an error message. Uses JSON .error if present, else body text or default. */
export async function parseErrorResponse(res: Response, defaultMessage: string): Promise<string> {
  const text = await res.text()
  try {
    const j = JSON.parse(text) as { error?: string }
    return j.error ?? (text || defaultMessage)
  } catch {
    return text || defaultMessage
  }
}

/**
 * An error carrying the status that produced it.
 *
 * Still an Error, so every existing `getErrorMessage(err, ...)` caller is
 * unaffected; the status is there for the few cases where the response code
 * changes what the UI should do rather than only what it says.
 */
export class ApiRequestError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = "ApiRequestError"
    this.status = status
  }
}

/** If res is not ok, read body, parse error message, and throw. Call after checkUnauthorized. */
export async function throwIfNotOk(res: Response): Promise<void> {
  if (res.ok) return
  const msg = await parseErrorResponse(res, res.statusText)
  throw new ApiRequestError(msg, res.status)
}

/** Fetch URL, refresh and retry on 401, throw if not ok, return JSON. Use for standard API calls. */
export async function requestJson<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await apiFetch(url, init)
  await throwIfNotOk(res)
  return res.json() as Promise<T>
}

/** Fetch URL, refresh and retry on 401, throw if not ok, return text. */
export async function requestText(url: string, init?: RequestInit): Promise<string> {
  const res = await apiFetch(url, init)
  await throwIfNotOk(res)
  return res.text()
}

/**
 * API base URL, most specific source first:
 *
 *  1. `window.__BUILDMAX_CONFIG__.apiBase` — written at container start from
 *     BUILDMAX_API_BASE. The only source available to a prebuilt image.
 *  2. `VITE_API_BASE` — baked in at build time, for `npm run dev` and for
 *     anyone building the bundle themselves.
 *  3. `http://localhost:5678` — the default a local server listens on.
 *
 * A trailing slash is trimmed, so `BUILDMAX_API_BASE=/` yields "" and every
 * request goes to the origin serving the Portal — the setup to use when a
 * reverse proxy puts the Portal and the server behind one hostname.
 */
export function getApiBase(): string {
  const runtime = typeof window !== "undefined" ? window.__BUILDMAX_CONFIG__?.apiBase : undefined
  if (typeof runtime === "string" && runtime !== "") {
    return trimTrailingSlash(runtime)
  }
  const build = import.meta.env.VITE_API_BASE
  if (typeof build === "string" && build !== "") {
    return trimTrailingSlash(build)
  }
  return defaultApiBase
}

function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, "")
}
