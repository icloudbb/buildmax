import { apiFetch, getApiBase, requestJson, throwIfNotOk } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import type { AuthMethods, PortalSessionResponse, OtpRequestResponse } from "../../lib/api/types"

/**
 * Ask which sign-in methods the deployment offers. Unauthenticated: the login
 * page calls it before anyone is signed in, to decide whether to show local
 * inputs, an SSO button, or both.
 */
export async function getAuthMethods(): Promise<AuthMethods> {
  return requestJson<AuthMethods>(`${getApiBase()}/api/auth/methods`, { method: "GET" })
}

/**
 * Create an account, when the deployment allows self-registration.
 *
 * It sends nothing: BuildMax has no mail channel. The account is created and
 * still needs a login code from an operator before anyone can sign in, which is
 * why the Portal offers no sign-up form.
 */
export async function requestOtp(
  email: string,
  intent: "signup" | "login"
): Promise<OtpRequestResponse> {
  return requestJson<OtpRequestResponse>(`${getApiBase()}/api/auth/otp`, {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify({ email, intent }),
  })
}

/**
 * Sign in with a single-use login code: the recovery path.
 *
 * The Portal login endpoint sets the HttpOnly refresh cookie and forces the
 * "portal" platform, so `credentials: "include"` is required and no platform is
 * sent.
 */
export async function login(email: string, otp: string): Promise<PortalSessionResponse> {
  return requestJson<PortalSessionResponse>(`${getApiBase()}/api/auth/portal/login`, {
    method: "POST",
    headers: jsonHeaders,
    credentials: "include",
    body: JSON.stringify({ email, otp }),
  })
}

/** Sign in with a password: the everyday path. */
export async function loginWithPassword(
  email: string,
  password: string
): Promise<PortalSessionResponse> {
  return requestJson<PortalSessionResponse>(`${getApiBase()}/api/auth/portal/login`, {
    method: "POST",
    headers: jsonHeaders,
    credentials: "include",
    body: JSON.stringify({ email, password }),
  })
}

/**
 * Trade the refresh cookie for a fresh session on load. Returns null on any
 * non-200 — no cookie, a dead session, or any error — rather than throwing, so
 * a cold start with no session simply lands on the login form.
 */
export async function restoreSession(): Promise<PortalSessionResponse | null> {
  let res: Response
  try {
    res = await fetch(`${getApiBase()}/api/auth/portal/session`, {
      method: "POST",
      credentials: "include",
    })
  } catch {
    return null
  }
  if (!res.ok) return null
  try {
    return (await res.json()) as PortalSessionResponse
  } catch {
    return null
  }
}

/**
 * Set or change the signed-in account's password.
 *
 * currentPassword is required when the account already has one — a session by
 * itself must not be enough to change it, or a stolen token would become a
 * permanent takeover. Setting the first password after signing in with a login
 * code needs no current password, because there is none.
 */
export async function setPassword(
  token: string,
  newPassword: string,
  currentPassword?: string
): Promise<void> {
  const res = await apiFetch(`${getApiBase()}/api/auth/password`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify({
      new_password: newPassword,
      current_password: currentPassword ?? "",
    }),
  })
  await throwIfNotOk(res)
}

/**
 * Ask the server to revoke this session and clear the refresh cookie.
 *
 * Needs no access token: the cookie the browser sends with
 * `credentials: "include"` is what names the session. Never throws — logging
 * out has to work when the server is unreachable, and a signed-out user still
 * looking at the app because the call failed is a worse outcome than a session
 * row that outlives its client.
 */
export async function revokeSession(): Promise<void> {
  try {
    await fetch(`${getApiBase()}/api/auth/portal/logout`, {
      method: "POST",
      credentials: "include",
      // The tab may be closing. Without this the request is cancelled and the
      // session survives a deliberate sign-out.
      keepalive: true,
    })
  } catch {
    // See above.
  }
}
