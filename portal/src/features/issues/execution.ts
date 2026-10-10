import type { Issue, Workflow } from "../../lib/types"
import type { PermissionState } from "../../state/permissionState"

/**
 * What can run an Issue in this Space, as far as the reader may assign it.
 * Space owners and admins create Agents and assign Workflows; `manage` keeps
 * unknown and failed role lookups apart from a refusal.
 */
export interface ExecutorChoices {
  agentCount: number
  publishedWorkflowCount: number
  manage: PermissionState
}

/**
 * The Executor field's hint. A new Space has no Agent and no published
 * Workflow, so the hint has to say what runs an Issue and who can add one;
 * otherwise the field offers only "None" and the Issue is a dead end.
 */
export type ExecutorHint =
  /** Agents and published Workflows can both be assigned. */
  | "agentOrWorkflow"
  /** Only Agents: Workflow assignment needs an owner or admin. */
  | "agentOnly"
  /** Nothing can run it yet, and the reader may create an Agent. */
  | "createAgent"
  /** Nothing can run it yet, and the reader may not create an Agent. */
  | "askForAgent"
  /** Nothing can run it yet, and the reader's role is not known. */
  | "nothingYet"

export function executorHint({ agentCount, publishedWorkflowCount, manage }: ExecutorChoices): ExecutorHint {
  if (manage === "allowed") return agentCount + publishedWorkflowCount > 0 ? "agentOrWorkflow" : "createAgent"
  if (agentCount > 0) return "agentOnly"
  return manage === "denied" ? "askForAgent" : "nothingYet"
}

/**
 * The Issue's run action. Run is offered for the assigned executor, and is
 * disabled with a specific reason until that executor can run; with no
 * executor it names the next step instead of leaving an action-less page.
 */
export type IssueRunAction =
  | { kind: "agent"; blocked: "agentMissing" | null }
  | { kind: "workflow"; blocked: "workflowUnpublished" | null }
  | { kind: "none"; next: "chooseExecutor" | "createAgent" | "askForAgent" | "nothingYet" }

export interface IssueRunInput {
  issue: Pick<Issue, "executorKind" | "executorId">
  /** Whether the assigned Agent is still in the Space. */
  agentExists: boolean
  /** The assigned Workflow's status, when one is assigned and known. */
  workflowStatus?: Workflow["status"]
  choices: ExecutorChoices
}

export function issueRunAction({ issue, agentExists, workflowStatus, choices }: IssueRunInput): IssueRunAction {
  if (issue.executorKind === "agent" && issue.executorId) {
    return { kind: "agent", blocked: agentExists ? null : "agentMissing" }
  }
  if (issue.executorKind === "workflow" && issue.executorId) {
    return { kind: "workflow", blocked: workflowStatus === "published" ? null : "workflowUnpublished" }
  }
  const hint = executorHint(choices)
  return { kind: "none", next: hint === "agentOrWorkflow" || hint === "agentOnly" ? "chooseExecutor" : hint }
}
