import { detectLocale } from "@buildmax/gui"
import { ApiRequestError, apiFetch, getApiBase, parseErrorResponse, throwIfNotOk } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import type { ApiAssistant, ApiAssistantDefinition, ApiAssistantRequester, ApiAssistantStatement } from "../../lib/api/types"
import { translate } from "../../i18n"

/**
 * The server refused a change because it alters what the Assistant discloses
 * and the owner has not confirmed this statement. The caller shows it and
 * resends with its digest. See docs/design/space-assistants.md §8.
 */
export class StatementRequiredError extends ApiRequestError {
  readonly statement: ApiAssistantStatement

  constructor(message: string, statement: ApiAssistantStatement) {
    super(message, 409)
    this.name = "StatementRequiredError"
    this.statement = statement
  }
}

function base(spaceId: string): string {
  return `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/assistants`
}

function one(spaceId: string, assistantId: string): string {
  return `${base(spaceId)}/${encodeURIComponent(assistantId)}`
}

/**
 * Every Assistant write can answer 409 with a statement to confirm, so the body
 * of a 409 is read before the generic error path consumes it.
 */
async function send<T>(url: string, init: RequestInit): Promise<T> {
  const res = await apiFetch(url, init)
  if (res.status === 409) {
    const text = await res.text()
    let body: { error?: string; statement?: ApiAssistantStatement } = {}
    try {
      body = JSON.parse(text) as typeof body
    } catch {
      // Not the statement shape; fall through to a plain conflict.
    }
    if (body.statement?.digest) {
      throw new StatementRequiredError(body.error ?? translate(detectLocale(), "assistants.error.confirmStatement"), body.statement)
    }
    throw new ApiRequestError(body.error ?? (text || res.statusText), 409)
  }
  await throwIfNotOk(res)
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

function write(method: string, token: string, body?: unknown): RequestInit {
  return {
    method,
    headers: { ...jsonHeaders, ...authHeaders(token) },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  }
}

export async function listAssistants(spaceId: string, token: string): Promise<ApiAssistant[]> {
  return send<ApiAssistant[]>(base(spaceId), { headers: authHeaders(token) })
}

export async function getAssistant(spaceId: string, assistantId: string, token: string): Promise<ApiAssistant> {
  return send<ApiAssistant>(one(spaceId, assistantId), { headers: authHeaders(token) })
}

/** Creates a paused Assistant; omitting service_account_id makes a same-named one. */
export async function createAssistant(
  spaceId: string,
  definition: ApiAssistantDefinition,
  token: string
): Promise<ApiAssistant> {
  return send<ApiAssistant>(base(spaceId), write("POST", token, definition))
}

/**
 * Replaces the definition, changes the sponsor, or both. Widening an active
 * Assistant throws StatementRequiredError until its digest is confirmed.
 */
export async function updateAssistant(
  spaceId: string,
  assistantId: string,
  body: { definition?: ApiAssistantDefinition; sponsor_user_id?: string; confirm_statement?: string },
  token: string
): Promise<ApiAssistant> {
  return send<ApiAssistant>(one(spaceId, assistantId), write("PATCH", token, body))
}

/** Publishes or pauses. Publishing throws StatementRequiredError until confirmed. */
export async function setAssistantState(
  spaceId: string,
  assistantId: string,
  state: "active" | "paused",
  token: string,
  confirmStatement?: string
): Promise<ApiAssistant> {
  return send<ApiAssistant>(
    `${one(spaceId, assistantId)}/state`,
    write("PUT", token, { state, ...(confirmStatement ? { confirm_statement: confirmStatement } : {}) })
  )
}

export async function deleteAssistant(spaceId: string, assistantId: string, token: string): Promise<void> {
  const res = await apiFetch(one(spaceId, assistantId), { method: "DELETE", headers: authHeaders(token) })
  if (!res.ok) {
    throw new ApiRequestError(await parseErrorResponse(res, translate(detectLocale(), "assistants.error.delete")), res.status)
  }
}

/** Binds a bot. The token is checked with the platform, sealed, and never returned. */
export async function bindAssistant(
  spaceId: string,
  assistantId: string,
  body: { platform: "telegram"; token: string },
  token: string
): Promise<ApiAssistant> {
  return send<ApiAssistant>(`${one(spaceId, assistantId)}/binding`, write("PUT", token, body))
}

export async function unbindAssistant(spaceId: string, assistantId: string, token: string): Promise<ApiAssistant> {
  return send<ApiAssistant>(`${one(spaceId, assistantId)}/binding`, { method: "DELETE", headers: authHeaders(token) })
}

/** The people an Assistant's bot may message, most recent first. Owners and admins only. */
export async function listAssistantRequesters(spaceId: string, assistantId: string, token: string): Promise<ApiAssistantRequester[]> {
  const res = await send<{ requesters: ApiAssistantRequester[] }>(`${one(spaceId, assistantId)}/requesters`, { headers: authHeaders(token) })
  return res.requesters
}
