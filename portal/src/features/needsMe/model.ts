import type { ApiAttentionReason, ApiNeedsMeGroup, ApiNeedsMeResponse } from "../../lib/api/types"
import { apiIssueToIssue, apiTaskToTask, apiWorkflowRequestToWorkflowRequest } from "../../lib/api/mappers"
import type { Issue, Task, WorkflowRequest } from "../../lib/types"

/** Up to ten items, and how many matched: more exist when total exceeds them. */
export interface NeedsMeGroup<T> {
  items: T[]
  total: number
}

export interface AttentionIssue {
  issue: Issue
  reason: ApiAttentionReason
}

/** The four groups, in the order the section shows them. */
export interface NeedsMe {
  issues: NeedsMeGroup<AttentionIssue>
  awaiting: NeedsMeGroup<Task>
  requests: NeedsMeGroup<WorkflowRequest>
  failed: NeedsMeGroup<Task>
}

function group<A, T>(api: ApiNeedsMeGroup<A> | undefined, map: (item: A) => T): NeedsMeGroup<T> {
  return { items: (api?.items ?? []).map(map), total: api?.total ?? 0 }
}

export function toNeedsMe(api: ApiNeedsMeResponse): NeedsMe {
  return {
    issues: group(api.attention_issues, (item) => ({ issue: apiIssueToIssue(item.issue), reason: item.reason })),
    awaiting: group(api.awaiting_answer_tasks, apiTaskToTask),
    requests: group(api.workflow_requests, apiWorkflowRequestToWorkflowRequest),
    failed: group(api.failed_tasks, apiTaskToTask),
  }
}

/** How many things wait, across every group. */
export function waitingCount(needs: NeedsMe): number {
  return needs.issues.total + needs.awaiting.total + needs.requests.total + needs.failed.total
}

const SUMMARY_LIMIT = 140

/**
 * What a request asks, in one line: a human_input step's prompt, or the first
 * question a step's Agent asked. Empty when it carries neither.
 */
export function requestSummary(request: Pick<WorkflowRequest, "prompt" | "questions">): string {
  const text = (request.prompt.trim() || request.questions[0]?.question || "").trim()
  const line = text.split("\n").find((part) => part.trim() !== "")?.trim() ?? ""
  return line.length > SUMMARY_LIMIT ? `${line.slice(0, SUMMARY_LIMIT - 1)}…` : line
}
