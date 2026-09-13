/**
 * In-memory access token, and nothing else.
 *
 * The access token lives in module state, never in storage a script can read:
 * the renewable credential is an HttpOnly cookie the server sets and the Portal
 * cannot see or touch, so there is nothing to persist here. A reload starts with
 * no token and asks the session endpoint for a fresh one against the cookie.
 *
 * This module deliberately makes no network calls. The transport in client.ts
 * needs to read the current token while deciding whether to retry a request,
 * and if the two imported each other that cycle would have to be untangled at
 * the worst possible moment. State lives here; the refresh that changes it
 * lives there.
 */

let accessToken: string | null = null
let expiresAt: number | null = null

export function currentAccessToken(): string | null {
  return accessToken
}

export function accessTokenExpiresAt(): number | null {
  return expiresAt
}

export function setAccessToken(token: string, tokenExpiresAt: number | null): void {
  accessToken = token
  expiresAt = tokenExpiresAt
}

export function clearAccessToken(): void {
  accessToken = null
  expiresAt = null
}

/** expiresIn is the server's seconds-from-now; absent means unknown. */
export function expiresAtFrom(expiresIn: number | undefined): number | null {
  if (typeof expiresIn !== "number" || !Number.isFinite(expiresIn) || expiresIn <= 0) {
    return null
  }
  return Date.now() + expiresIn * 1000
}
