import { apiFetch, getApiBase, requestJson, throwIfNotOk } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"

/**
 * A CLI or Desktop sign-in request, as the Portal shows it before a person
 * decides. Only a Portal session can read or decide one; the server refuses
 * any other. See docs/design/enterprise-identity-and-access.md section 14.
 */
export interface NativeSignInRequest {
  /** "cli" or "desktop". */
  platform: string
  /** What the client says it is. Never verified. */
  client_label?: string
  created_at: string
  expires_at: string
  /** When the session a confirmation leads to would end: this Portal session's own end. */
  session_expires_at: string
}

/** Look a request up by its code. A wrong, expired, or decided code is one 404. */
export async function lookUpNativeSignIn(code: string, token: string): Promise<NativeSignInRequest> {
  const query = new URLSearchParams({ code })
  return requestJson<NativeSignInRequest>(`${getApiBase()}/api/auth/native-sign-ins?${query}`, {
    headers: authHeaders(token),
  })
}

async function decide(path: "confirm" | "deny", code: string, token: string): Promise<void> {
  const res = await apiFetch(`${getApiBase()}/api/auth/native-sign-ins/${path}`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify({ code }),
  })
  await throwIfNotOk(res)
}

/** Let the client's next poll create its session. */
export function confirmNativeSignIn(code: string, token: string): Promise<void> {
  return decide("confirm", code, token)
}

/** End the request; the client's next poll is told it was not allowed. */
export function denyNativeSignIn(code: string, token: string): Promise<void> {
  return decide("deny", code, token)
}
