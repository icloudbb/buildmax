import { apiFetch, requestJson, throwIfNotOk, getApiBase } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import type { ApiIssueComment, ApiIssueCommentsResponse } from "../../lib/api/types"

function commentsBase(spaceId: string, issueId: string): string {
  return `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}/comments`
}

export async function getIssueComments(
  spaceId: string,
  issueId: string,
  token: string,
  options?: { limit?: number; offset?: number },
): Promise<ApiIssueCommentsResponse> {
  const params = new URLSearchParams()
  if (options?.limit != null) params.set("limit", String(options.limit))
  if (options?.offset != null) params.set("offset", String(options.offset))
  const q = params.toString()
  return requestJson<ApiIssueCommentsResponse>(`${commentsBase(spaceId, issueId)}${q ? `?${q}` : ""}`, {
    headers: authHeaders(token),
  })
}

export async function createIssueComment(
  spaceId: string,
  issueId: string,
  body: string,
  token: string,
): Promise<ApiIssueComment> {
  return requestJson<ApiIssueComment>(commentsBase(spaceId, issueId), {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify({ body }),
  })
}

export async function updateIssueComment(
  spaceId: string,
  issueId: string,
  commentId: string,
  body: string,
  token: string,
): Promise<ApiIssueComment> {
  return requestJson<ApiIssueComment>(`${commentsBase(spaceId, issueId)}/${encodeURIComponent(commentId)}`, {
    method: "PATCH",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify({ body }),
  })
}

export async function deleteIssueComment(
  spaceId: string,
  issueId: string,
  commentId: string,
  token: string,
): Promise<void> {
  const res = await apiFetch(`${commentsBase(spaceId, issueId)}/${encodeURIComponent(commentId)}`, {
    method: "DELETE",
    headers: authHeaders(token),
  })
  await throwIfNotOk(res)
}

/** Sends text to an escalated Issue's requester through the Assistant's bot.
 *  The server records it on the Issue and returns that comment. */
export async function replyToRequester(
  spaceId: string,
  issueId: string,
  text: string,
  token: string,
): Promise<ApiIssueComment> {
  return requestJson<ApiIssueComment>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}/requester-replies`,
    {
      method: "POST",
      headers: { ...jsonHeaders, ...authHeaders(token) },
      body: JSON.stringify({ text }),
    },
  )
}
