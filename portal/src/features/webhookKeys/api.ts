import {
  getApiBase,
  requestJson,
  apiFetch,
  parseErrorResponse,
} from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import { detectLocale } from "@buildmax/gui"
import { translate } from "../../i18n"

export interface WebhookKeyMeta {
  id: string
  name: string
  created_at: string
}

export interface CreateWebhookKeyResponse {
  key: string
  id: string
}

export interface ListWebhookKeysResponse {
  keys: WebhookKeyMeta[]
}

export async function createWebhookKey(
  body: { name?: string },
  token: string
): Promise<CreateWebhookKeyResponse> {
  return requestJson<CreateWebhookKeyResponse>(`${getApiBase()}/api/webhook-keys`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function listWebhookKeys(
  token: string
): Promise<ListWebhookKeysResponse> {
  return requestJson<ListWebhookKeysResponse>(`${getApiBase()}/api/webhook-keys`, {
    headers: authHeaders(token),
  })
}

export async function revokeWebhookKey(
  keyId: string,
  token: string
): Promise<void> {
  const res = await apiFetch(`${getApiBase()}/api/webhook-keys/${keyId}`, {
    method: "DELETE",
    headers: authHeaders(token),
  })
  if (!res.ok) {
    const msg = await parseErrorResponse(res, translate(detectLocale(), "account.webhook.revokeError"))
    throw new Error(msg)
  }
}
