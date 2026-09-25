import { getApiBase, requestJson, apiFetch, parseErrorResponse } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"

/** A chat platform this deployment has a bot on. */
export interface ChannelPlatform {
  platform: string
  name: string
  bot_handle?: string
  bot_url?: string
}

/** A link from a chat-platform account to the signed-in account. */
export interface ChannelLink {
  id: string
  platform: string
  handle?: string
  created_at: string
}

export interface ListChannelLinksResponse {
  platforms: ChannelPlatform[]
  links: ChannelLink[]
}

/** The chat account a link code would link, shown before confirming. */
export interface ChannelPairing {
  platform: string
  handle?: string
  expires_at: string
}

export async function listChannelLinks(token: string): Promise<ListChannelLinksResponse> {
  return requestJson<ListChannelLinksResponse>(`${getApiBase()}/api/channel-links`, {
    headers: authHeaders(token),
  })
}

export async function getChannelPairing(code: string, token: string): Promise<ChannelPairing> {
  const query = new URLSearchParams({ code })
  return requestJson<ChannelPairing>(`${getApiBase()}/api/channel-link-pairings?${query}`, {
    headers: authHeaders(token),
  })
}

export async function createChannelLink(code: string, token: string): Promise<ChannelLink> {
  return requestJson<ChannelLink>(`${getApiBase()}/api/channel-links`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify({ code }),
  })
}

export async function deleteChannelLink(linkId: string, token: string): Promise<void> {
  const res = await apiFetch(`${getApiBase()}/api/channel-links/${encodeURIComponent(linkId)}`, {
    method: "DELETE",
    headers: authHeaders(token),
  })
  if (!res.ok) {
    throw new Error(await parseErrorResponse(res, "Failed to unlink"))
  }
}
