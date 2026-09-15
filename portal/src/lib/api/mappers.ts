/**
 * Map API DTOs to UI types and format values for display.
 * Imports API types from ./types and UI types from ../types.
 */

import type {
  ApiAgent,
  ApiAgentRevision,
  ApiIssueOutput,
  ApiOutputSource,
  ApiTask,
  ApiConversation,
  ApiIssue,
  ApiWorkflow,
  ApiWorkflowRevision,
  ApiWorkflowRun,
  ApiWorkflowNodeRun,
} from "./types"
import type {
  Agent,
  AgentRevision,
  IssueOutput,
  OutputSource,
  Task,
  Conversation,
  Issue,
  Workflow,
  WorkflowRevision,
  WorkflowRun,
  WorkflowNodeRun,
} from "../types"

/** Format an RFC 3339 instant as "Today HH:MM", "Yesterday HH:MM", or full locale string. */
function formatRelativeTime(rfc3339: string): string {
  const d = new Date(rfc3339)
  const today = new Date()
  if (d.toDateString() === today.toDateString()) {
    return `Today ${d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
  }
  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)
  if (d.toDateString() === yesterday.toDateString()) {
    return `Yesterday ${d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
  }
  return d.toLocaleString()
}

function taskStatusToUI(status: string): Task["status"] {
  switch (status) {
    case "SUCCEEDED":
      return "success"
    case "FAILED":
      return "failed"
    case "CANCELED":
      return "canceled"
    case "PENDING":
      return "pending"
    case "RUNNING":
    default:
      return "running"
  }
}

export function apiAgentToAgent(api: ApiAgent): Agent {
  return {
    id: api.id,
    name: api.name,
    description: api.description,
    instructions: api.instructions,
    model: api.model,
    plugins: api.plugins,
    sandboxNetworkTier: api.sandbox_network_tier,
    sandboxFilesystemTier: api.sandbox_filesystem_tier,
    secretConsumption: api.secret_consumption,
    revision: api.revision,
    createdAt: api.created_at,
  }
}

export function apiAgentRevisionToAgentRevision(api: ApiAgentRevision): AgentRevision {
  return {
    // A revision has no handle of its own: its parent plus its number is what
    // the API addresses it by, and it is what makes a list key unique.
    id: `${api.agent_id}@${api.revision}`,
    agentId: api.agent_id,
    revision: api.revision,
    name: api.name,
    description: api.description,
    instructions: api.instructions,
    model: api.model,
    createdBy: api.created_by,
    createdAt: api.created_at,
    createdLabel: formatRelativeTime(api.created_at),
  }
}

export function apiIssueToIssue(api: ApiIssue): Issue {
  return {
    id: api.id,
    userId: api.user_id,
    parentIssueId: api.parent_issue_id ?? null,
    childCount: api.child_count ?? 0,
    doneChildCount: api.done_child_count ?? 0,
    commentCount: api.comment_count ?? 0,
    title: api.title,
    description: api.description,
    status: api.status as Issue["status"],
    ownerId: api.owner_id ?? null,
    executorKind: (api.executor_kind as Issue["executorKind"]) ?? null,
    executorId: api.executor_id ?? null,
    createdBy: api.created_by,
    createdAt: api.created_at,
    updatedAt: api.updated_at,
    updatedLabel: formatRelativeTime(api.updated_at),
    version: api.version,
  }
}

export function apiWorkflowRevisionToWorkflowRevision(api: ApiWorkflowRevision): WorkflowRevision {
  return {
    id: `${api.workflow_id}@${api.revision}`,
    workflowId: api.workflow_id,
    revision: api.revision,
    name: api.name,
    description: api.description,
    definition: api.definition,
    status: api.status,
    createdBy: api.created_by,
    createdAt: api.created_at,
    createdLabel: formatRelativeTime(api.created_at),
  }
}

export function apiWorkflowToWorkflow(api: ApiWorkflow): Workflow {
  return {
    id: api.id,
    spaceId: api.space_id,
    name: api.name,
    description: api.description,
    definition: api.definition,
    status: api.status as Workflow["status"],
    revision: api.revision,
    createdBy: api.created_by,
    createdAt: api.created_at,
    updatedAt: api.updated_at,
    updatedLabel: formatRelativeTime(api.updated_at),
  }
}

export function apiWorkflowRunToWorkflowRun(api: ApiWorkflowRun): WorkflowRun {
  return {
    id: api.id,
    workflowId: api.workflow_id,
    workflowRevision: api.workflow_revision ?? null,
    issueId: api.issue_id ?? null,
    status: api.status as WorkflowRun["status"],
    createdBy: api.created_by,
    createdAt: api.created_at,
    startedAt: api.started_at ?? null,
    endedAt: api.ended_at ?? null,
    errorMessage: api.error_message ?? null,
    result: api.result ?? null,
    createdLabel: formatRelativeTime(api.created_at),
  }
}

export function apiWorkflowNodeRunToWorkflowNodeRun(api: ApiWorkflowNodeRun): WorkflowNodeRun {
  return {
    id: api.id,
    workflowRunId: api.workflow_run_id,
    nodeId: api.node_id,
    nodeIndex: api.node_index,
    nodeType: api.node_type,
    targetAgentId: api.target_agent_id ?? null,
    agentRevision: api.agent_revision ?? null,
    agentName: api.agent_name ?? null,
    agentDescription: api.agent_description ?? null,
    agentInstructions: api.agent_instructions ?? null,
    prompt: api.prompt,
    status: api.status as WorkflowNodeRun["status"],
    taskId: api.task_id ?? null,
    taskRunId: api.task_run_id ?? null,
    resolvedInput: api.resolved_input ?? null,
    output: api.output ?? null,
    errorMessage: api.error_message ?? null,
    createdAt: api.created_at,
    startedAt: api.started_at ?? null,
    endedAt: api.ended_at ?? null,
  }
}

export function apiTaskToTask(api: ApiTask): Task {
  const title =
    api.title && api.title.trim() !== ""
      ? api.title
      : api.input.length > 80
        ? api.input.slice(0, 77) + "..."
        : api.input
  const summary =
    api.output ?? (api.input.length > 120 ? api.input.slice(0, 117) + "..." : api.input)
  const ts = api.ended_at ?? api.created_at
  return {
    id: api.id,
    conversationId: api.conversation_id,
    sessionId: api.session_id ?? undefined,
    title,
    status: taskStatusToUI(api.status),
    timeLabel: formatRelativeTime(ts),
    summary,
    createdAt: api.created_at,
    agentId: api.agent_id ?? undefined,
    issueId: api.issue_id ?? undefined,
  }
}

export function apiConversationToConversation(api: ApiConversation): Conversation {
  return {
    id: api.id,
    channel: api.channel,
    title: api.title?.trim() ?? "",
    createdAt: api.created_at,
    timeLabel: formatRelativeTime(api.created_at),
  }
}

export function apiOutputSourceToOutputSource(api: ApiOutputSource): OutputSource {
  return {
    sourceType: api.source_type,
    taskId: api.task_id,
    taskRunId: api.task_run_id,
    conversationId: api.conversation_id,
    workflowRunId: api.workflow_run_id ?? null,
    workflowNodeRunId: api.workflow_node_run_id ?? null,
    workflowNodeId: api.workflow_node_id ?? null,
  }
}

export function apiIssueOutputToIssueOutput(api: ApiIssueOutput): IssueOutput {
  return {
    id: api.id,
    title: api.title,
    kind: api.kind,
    artifactId: api.artifact_id,
    filename: api.filename,
    mediaType: api.media_type,
    sizeBytes: api.size_bytes,
    source: apiOutputSourceToOutputSource(api.source),
    createdAt: api.created_at,
  }
}
