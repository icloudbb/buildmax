import { apiFetch, getApiBase, requestJson, throwIfNotOk } from "../../lib/api/client"
import { authHeaders } from "../../lib/api/common"

/**
 * One live session of the signed-in account: a browser, a CLI, or a Desktop.
 * Metadata to recognise it by; the server never returns a token. See
 * docs/design/enterprise-identity-and-access.md section 14.5.
 */
export interface AccountSession {
  session_id: string
  /** "portal", "cli", or "desktop". */
  platform?: string
  /** The sign-in behind it: "password", "login_code", or "oidc". */
  auth_method?: string
  /** What a native client said it is. Never verified. */
  client_label?: string
  created_at: string
  /** The zero time until the session is used after signing in. */
  last_seen_at: string
  expires_at: string
  /** The session this browser is using. */
  current: boolean
}

export async function listAccountSessions(token: string): Promise<AccountSession[]> {
  const res = await requestJson<{ sessions: AccountSession[] }>(`${getApiBase()}/api/auth/sessions`, {
    headers: authHeaders(token),
  })
  return res.sessions
}

/** End another of the account's sessions. This browser's own ends with the ordinary sign-out. */
export async function revokeAccountSession(token: string, sessionId: string): Promise<void> {
  const res = await apiFetch(`${getApiBase()}/api/auth/sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE",
    headers: authHeaders(token),
  })
  await throwIfNotOk(res)
}
