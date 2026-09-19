import { getApiBase, requestJson } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import { downloadAuthenticated } from "../../lib/download"
import type {
  ApiAdminLLMCallsResponse,
  ApiAdminLoginCode,
  ApiAdminMe,
  ApiAdminModel,
  ApiAdminModelsResponse,
  ApiAdminSessionsResponse,
  ApiAdminSessionsRevoked,
  ApiAdminSystem,
  ApiAdminSpaceDetail,
  ApiAdminSpacesResponse,
  ApiAdminUser,
  ApiAdminUserAfterDisable,
  ApiAdminUserDetail,
  ApiAdminUsersResponse,
  ApiAuditEventsResponse,
  ApiDeactivationImpact,
  ApiPluginReleasesResponse,
  ApiPluginsResponse,
  ApiSystemGrant,
  ApiSystemGrantsResponse,
} from "../../lib/api/types"

/**
 * Client for /api/admin.
 *
 * Every call here 403s for anyone without a deployment-scoped grant, and that
 * is the expected answer rather than a bug — the same convention the
 * space-scoped audit client already documents. Hiding the navigation is
 * presentation; the server refuses either way.
 */

function adminUrl(path: string, params?: Record<string, string | number | undefined>): string {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params ?? {})) {
    if (value !== undefined && value !== "") query.set(key, String(value))
  }
  const suffix = query.toString()
  return `${getApiBase()}/api/admin${path}${suffix ? `?${suffix}` : ""}`
}

function get<T>(path: string, token: string, params?: Record<string, string | number | undefined>): Promise<T> {
  return requestJson<T>(adminUrl(path, params), { headers: authHeaders(token) })
}

function send<T>(method: string, path: string, token: string, body?: unknown): Promise<T> {
  return requestJson<T>(adminUrl(path), {
    method,
    headers: { ...authHeaders(token), ...jsonHeaders },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

/** Whether the caller may operate this deployment. A rejection means no. */
export function getAdminMe(token: string): Promise<ApiAdminMe> {
  return get<ApiAdminMe>("/me", token)
}

/**
 * Everyone who can operate the deployment. includeRevoked adds the retired
 * grants, which is how the trail of who held authority is read.
 */
export function listAdminGrants(token: string, includeRevoked?: boolean): Promise<ApiSystemGrantsResponse> {
  return get<ApiSystemGrantsResponse>("/grants", token, {
    include_revoked: includeRevoked ? "true" : undefined,
  })
}

/** Grants deployment-administrator authority to an existing account. */
export function createAdminGrant(token: string, userId: string): Promise<ApiSystemGrant> {
  return send<ApiSystemGrant>("POST", "/grants", token, { user_id: userId })
}

/**
 * Revokes an account's authority. The server refuses the last effective holder;
 * that refusal surfaces as the error message it carries.
 */
export function revokeAdminGrant(token: string, userId: string): Promise<void> {
  return send<void>("DELETE", `/grants/${encodeURIComponent(userId)}`, token)
}

export function getAdminSystem(token: string): Promise<ApiAdminSystem> {
  return get<ApiAdminSystem>("/system", token)
}

/** The effective server.yaml, with every credential reduced to whether it is set. */
export function getAdminConfig(token: string): Promise<Record<string, unknown>> {
  return get<Record<string, unknown>>("/config", token)
}

export function listAdminUsers(
  token: string,
  options?: {
    q?: string
    limit?: number
    offset?: number
    status?: string
    has_password?: string
    system_role?: string
    platform?: string
    last_login_after?: string
    last_login_before?: string
  },
): Promise<ApiAdminUsersResponse> {
  return get<ApiAdminUsersResponse>("/users", token, options)
}

export function getAdminUser(token: string, userId: string): Promise<ApiAdminUserDetail> {
  return get<ApiAdminUserDetail>(`/users/${encodeURIComponent(userId)}`, token)
}

export function createAdminUser(token: string, email: string): Promise<ApiAdminUser> {
  return send<ApiAdminUser>("POST", "/users", token, { email })
}

/** Issues a single-use code. It is returned once and is recoverable nowhere. */
export function issueAdminLoginCode(token: string, userId: string): Promise<ApiAdminLoginCode> {
  return send<ApiAdminLoginCode>("POST", `/users/${encodeURIComponent(userId)}/login-code`, token)
}

export function setAdminUserDisabled(
  token: string,
  userId: string,
  disabled: boolean,
  opts?: { retireWebhookKeys?: boolean },
): Promise<ApiAdminUserAfterDisable> {
  return send<ApiAdminUserAfterDisable>("PUT", `/users/${encodeURIComponent(userId)}/state`, token, {
    disabled,
    retire_webhook_keys: opts?.retireWebhookKeys ?? false,
  })
}

/** What disabling this account would stop, as counts and ids only. */
export function getDeactivationImpact(token: string, userId: string): Promise<ApiDeactivationImpact> {
  return get<ApiDeactivationImpact>(`/users/${encodeURIComponent(userId)}/deactivation-impact`, token)
}

/**
 * Recover a shared Space whose owners are all disabled by promoting an enabled
 * member. Refused by the server unless every recorded owner is disabled.
 */
export function recoverSpaceOwner(token: string, spaceId: string, successorId: string): Promise<void> {
  return send<void>("PUT", `/spaces/${encodeURIComponent(spaceId)}/owner`, token, {
    successor_id: successorId,
  })
}

export function revokeAdminUserSessions(token: string, userId: string): Promise<ApiAdminSessionsRevoked> {
  return send<ApiAdminSessionsRevoked>("DELETE", `/users/${encodeURIComponent(userId)}/sessions`, token)
}

/** An account's live login chains — safe metadata to recognise a device by. */
export function listAdminUserSessions(token: string, userId: string): Promise<ApiAdminSessionsResponse> {
  return get<ApiAdminSessionsResponse>(`/users/${encodeURIComponent(userId)}/sessions`, token)
}

/** Revoke one login chain, leaving the account's other sessions live. */
export function revokeAdminUserSession(
  token: string,
  userId: string,
  sessionId: string,
): Promise<ApiAdminSessionsRevoked> {
  return send<ApiAdminSessionsRevoked>(
    "DELETE",
    `/users/${encodeURIComponent(userId)}/sessions/${encodeURIComponent(sessionId)}`,
    token,
  )
}

/**
 * The catalog as an administrator sees it: archived entries included, because
 * hiding a retired entry from the person who retired it leaves no way to
 * restore it.
 */
export function listAdminPlugins(token: string): Promise<ApiPluginsResponse> {
  return get<ApiPluginsResponse>("/plugins", token)
}

/** Every release of one plugin, withdrawn ones included and marked. */
export function listAdminPluginReleases(
  token: string,
  name: string,
): Promise<ApiPluginReleasesResponse> {
  return get<ApiPluginReleasesResponse>(`/plugins/${encodeURIComponent(name)}/releases`, token)
}

/**
 * Withdraws one release from default selection.
 *
 * It deletes nothing: a copy somebody already installed keeps working, and an
 * exact version stays recoverable by someone who acknowledges the state.
 */
export function yankAdminPluginRelease(
  token: string,
  name: string,
  version: string,
  reason: string,
): Promise<void> {
  return send<void>(
    "PUT",
    `/plugins/${encodeURIComponent(name)}/releases/${encodeURIComponent(version)}/state`,
    token,
    { yanked: true, reason },
  )
}

/** Retires or restores a catalog entry. Archiving refuses new releases. */
export function setAdminPluginArchived(
  token: string,
  name: string,
  archived: boolean,
): Promise<void> {
  return send<void>("PUT", `/plugins/${encodeURIComponent(name)}/state`, token, { archived })
}

export function listAdminSpaces(
  token: string,
  options?: { q?: string; limit?: number; offset?: number },
): Promise<ApiAdminSpacesResponse> {
  return get<ApiAdminSpacesResponse>("/spaces", token, options)
}

export function getAdminSpace(token: string, spaceId: string): Promise<ApiAdminSpaceDetail> {
  return get<ApiAdminSpaceDetail>(`/spaces/${encodeURIComponent(spaceId)}`, token)
}

export function searchAdminAuditEvents(
  token: string,
  options?: {
    space_id?: string
    actor_id?: string
    action?: string
    since?: number
    until?: number
    limit?: number
    offset?: number
  },
): Promise<ApiAuditEventsResponse> {
  return get<ApiAuditEventsResponse>("/audit-events", token, options)
}

/**
 * Download the deployment-wide trail as a file, under the same filters as the
 * search above.
 *
 * The server records the export, and records it against the administrator who
 * took it. An export narrowed to one space is recorded in that space's trail
 * too, so its owner can see that the deployment read their record.
 */
export async function exportAdminAuditEvents(
  token: string,
  format: "csv" | "jsonl",
  options?: { space_id?: string; actor_id?: string; action?: string; since?: number; until?: number },
): Promise<void> {
  const url = adminUrl("/audit-events/export", { ...options, format })
  await downloadAuthenticated(url, token, `audit-deployment.${format}`)
}

export function listAdminModels(token: string): Promise<ApiAdminModelsResponse> {
  return get<ApiAdminModelsResponse>("/llm/models", token)
}

/**
 * The managed call ledger across every user and space — what the deployment
 * spent on inference, and on which model. Carries no prompts or generated
 * content: the ledger never held them.
 */
export function searchAdminLLMCalls(
  token: string,
  options?: {
    user_id?: string
    model?: string
    status?: string
    surface?: string
    since?: string
    until?: string
    limit?: number
    offset?: number
  },
): Promise<ApiAdminLLMCallsResponse> {
  return get<ApiAdminLLMCallsResponse>("/llm/calls", token, options)
}

/** Retires or restores a catalog model. */
export function setAdminModelEnabled(
  token: string,
  modelId: string,
  enabled: boolean,
): Promise<ApiAdminModel> {
  return send<ApiAdminModel>("PUT", `/llm/models/${encodeURIComponent(modelId)}/state`, token, {
    enabled,
  })
}

/**
 * The fields a new catalog model is created with, mirroring
 * `buildmax-server model add`. Prices are strings in the model's currency.
 *
 * api_key is write-only: it is sent here and never read back. Only send fields
 * an operator filled; the server applies the same defaults the shell does.
 */
export interface AdminCreateModelInput {
  name: string
  provider_type?: string
  api_url: string
  api_key?: string
  model: string
  context_window?: number
  call_timeout?: number
  max_tokens?: number
  reasoning?: string
  cache_mode?: string
  cache_ttl?: string
  currency?: string
  input_price?: string
  cache_read_price?: string
  cache_write_price?: string
  output_price?: string
  vision?: boolean
  capabilities?: string[]
}

/**
 * Adds a catalog model. The provider key travels in the request body only and
 * is stored encrypted at rest; no read returns it. A deployment with no
 * encryption key configured refuses a model that carries one.
 */
export function createAdminModel(token: string, input: AdminCreateModelInput): Promise<ApiAdminModel> {
  return send<ApiAdminModel>("POST", "/llm/models", token, input)
}
