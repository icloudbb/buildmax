import {
  requestJson,
  getApiBase,
} from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import type { ApiIssue, ApiIssueFlowResponse, ApiIssuesListResponse, ApiTask } from "../../lib/api/types"

export interface GetIssuesOptions {
  limit?: number
  offset?: number
  /**
   * "none" lists only top-level issues; an issue id lists that issue's
   * sub-issues. Omitting it lists everything, which is what the endpoint did
   * before sub-issues existed.
   */
  parentId?: string
  status?: "todo" | "in_progress" | "done"
  /** "me" resolves to the caller server-side; anything else is a user id. */
  owner?: string
  /** Narrows only when both are set, matching the server. */
  executorKind?: "agent" | "workflow"
  executorId?: string
}

export async function getIssues(spaceId: string, token: string, options?: GetIssuesOptions): Promise<ApiIssuesListResponse> {
  const params = new URLSearchParams()
  if (options?.limit != null) params.set("limit", String(options.limit))
  if (options?.offset != null) params.set("offset", String(options.offset))
  if (options?.parentId) params.set("parent_id", options.parentId)
  if (options?.status) params.set("status", options.status)
  if (options?.owner === "me") params.set("owner", "me")
  else if (options?.owner) params.set("owner_id", options.owner)
  if (options?.executorKind && options.executorId) {
    params.set("executor_kind", options.executorKind)
    params.set("executor_id", options.executorId)
  }
  const q = params.toString()
  return requestJson<ApiIssuesListResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues${q ? `?${q}` : ""}`, {
    headers: authHeaders(token),
  })
}

export async function getIssue(spaceId: string, issueId: string, token: string): Promise<ApiIssue> {
  return requestJson<ApiIssue>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}`, {
    headers: authHeaders(token),
  })
}

export async function getIssueFlow(spaceId: string, issueId: string, token: string): Promise<ApiIssueFlowResponse> {
  return requestJson<ApiIssueFlowResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}/flow`, {
    headers: authHeaders(token),
  })
}

export async function createIssue(
  spaceId: string,
  body: {
    title: string
    description?: string
    parent_issue_id?: string
    /** Optional, and written with the issue: a refused value creates nothing. */
    status?: "todo" | "in_progress" | "done"
    owner_id?: string
    executor_kind?: "agent" | "workflow" | ""
    executor_id?: string
  },
  token: string,
): Promise<ApiIssue> {
  return requestJson<ApiIssue>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function updateIssue(
  spaceId: string,
  issueId: string,
  body: {
    /**
     * The version this change was built from, required. The server refuses the
     * write with 409 if the issue moved on, rather than overwriting whatever
     * the caller never saw.
     */
    version: number
    title?: string
    description?: string
    status?: "todo" | "in_progress" | "done"
    /** An empty string clears the owner. */
    owner_id?: string
    executor_kind?: "agent" | "workflow" | ""
    /** An empty string clears the executor. */
    executor_id?: string
    /** An empty string clears the parent, matching how owner/executor are cleared. */
    parent_issue_id?: string
  },
  token: string,
): Promise<ApiIssue> {
  return requestJson<ApiIssue>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}`, {
    method: "PATCH",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function runIssueAgent(
  spaceId: string,
  issueId: string,
  token: string,
  input?: string,
): Promise<ApiTask> {
  return requestJson<ApiTask>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}/agent-runs`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(input ? { input } : {}),
  })
}
