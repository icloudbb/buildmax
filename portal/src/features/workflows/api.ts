import { requestJson, getApiBase } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import type {
  ApiWorkflow,
  ApiWorkflowListResponse,
  ApiWorkflowRevisionListResponse,
  ApiWorkflowRunDetailResponse,
  ApiWorkflowRunListResponse,
} from "../../lib/api/types"

export async function getWorkflows(spaceId: string, token: string): Promise<ApiWorkflowListResponse> {
  return requestJson<ApiWorkflowListResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows`, {
    headers: authHeaders(token),
  })
}

export async function getWorkflow(spaceId: string, workflowId: string, token: string): Promise<ApiWorkflow> {
  return requestJson<ApiWorkflow>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows/${encodeURIComponent(workflowId)}`, {
    headers: authHeaders(token),
  })
}

export async function createWorkflow(
  spaceId: string,
  body: { name: string; description?: string; definition: string },
  token: string,
): Promise<ApiWorkflow> {
  return requestJson<ApiWorkflow>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function updateWorkflow(
  spaceId: string,
  workflowId: string,
  body: { name?: string; description?: string; definition?: string; status?: string },
  token: string,
): Promise<ApiWorkflow> {
  return requestJson<ApiWorkflow>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows/${encodeURIComponent(workflowId)}`, {
    method: "PATCH",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function getWorkflowRuns(
  spaceId: string,
  workflowId: string,
  token: string,
): Promise<ApiWorkflowRunListResponse> {
  return requestJson<ApiWorkflowRunListResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows/${encodeURIComponent(workflowId)}/runs`, {
    headers: authHeaders(token),
  })
}

export async function getWorkflowRunDetail(
  spaceId: string,
  workflowRunId: string,
  token: string,
): Promise<ApiWorkflowRunDetailResponse> {
  return requestJson<ApiWorkflowRunDetailResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflow-runs/${encodeURIComponent(workflowRunId)}`, {
    headers: authHeaders(token),
  })
}

export async function runWorkflow(
  spaceId: string,
  workflowId: string,
  token: string,
  issueId?: string,
  input?: unknown,
): Promise<ApiWorkflowRunDetailResponse> {
  const body: Record<string, unknown> = {}
  if (issueId) body.issue_id = issueId
  if (input !== undefined) body.input = input
  return requestJson<ApiWorkflowRunDetailResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows/${encodeURIComponent(workflowId)}/runs`, {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function runIssueWorkflow(
  spaceId: string,
  issueId: string,
  token: string,
): Promise<ApiWorkflowRunDetailResponse> {
  return requestJson<ApiWorkflowRunDetailResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/issues/${encodeURIComponent(issueId)}/workflow-runs`, {
    method: "POST",
    headers: authHeaders(token),
  })
}

export async function getWorkflowRevisions(
  spaceId: string,
  workflowId: string,
  token: string,
): Promise<ApiWorkflowRevisionListResponse> {
  return requestJson<ApiWorkflowRevisionListResponse>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows/${encodeURIComponent(workflowId)}/revisions`,
    { headers: authHeaders(token) },
  )
}

export async function restoreWorkflowRevision(
  spaceId: string,
  workflowId: string,
  revision: number,
  token: string,
): Promise<ApiWorkflow> {
  return requestJson<ApiWorkflow>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/workflows/${encodeURIComponent(workflowId)}/revisions/${revision}/restore`,
    { method: "POST", headers: authHeaders(token) },
  )
}
