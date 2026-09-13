import { apiFetch, getApiBase, requestJson, throwIfNotOk } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import { currentAccessToken, currentRefreshToken } from "../../lib/api/session"
import type { LoginResponse, OtpRequestResponse } from "../../lib/api/types"

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

/** Sign in with a single-use login code: the recovery path. */
export async function login(email: string, otp: string): Promise<LoginResponse> {
  return requestJson<LoginResponse>(`${getApiBase()}/api/auth/login`, {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify({ email, otp, platform: "portal" }),
  })
}

/** Sign in with a password: the everyday path. */
export async function loginWithPassword(
  email: string,
  password: string
): Promise<LoginResponse> {
  return requestJson<LoginResponse>(`${getApiBase()}/api/auth/login`, {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify({ email, password, platform: "portal" }),
  })
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
 * Ask the server to revoke this session.
 *
 * Never throws. Logging out has to work when the server is unreachable, and a
 * signed-out user who is still looking at the app because the call failed is a
 * worse outcome than a session row that outlives its client.
 */
export async function revokeSession(): Promise<void> {
  const refreshToken = currentRefreshToken()
  const accessToken = currentAccessToken()
  if (!refreshToken && !accessToken) return
  try {
    await fetch(`${getApiBase()}/api/auth/logout`, {
      method: "POST",
      headers: accessToken
        ? { ...jsonHeaders, Authorization: `Bearer ${accessToken}` }
        : jsonHeaders,
      body: JSON.stringify({ refresh_token: refreshToken ?? "" }),
      // The tab may be closing. Without this the request is cancelled and the
      // session survives a deliberate sign-out.
      keepalive: true,
    })
  } catch {
    // See above.
  }
}
