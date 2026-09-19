// --- Entity types ---

/** Space-owned Agent execution thread. Backend: Task. */
export interface Task {
  id: string
  conversationId?: string
  sessionId?: string
  title: string
  status: "pending" | "running" | "success" | "failed" | "canceled"
  timeLabel: string
  summary: string
  createdAt: string
  /** Set when the task was started from an agent. */
  agentId?: string
  /** Set when the task was started from an issue. */
  issueId?: string
}

/** Tier 1 conversation (user-facing dialogue). */
export interface Conversation {
  id: string
  channel: string
  title: string
  createdAt: string
  timeLabel: string
}

// --- Route types ---

// Every Space-owned route carries the Space's public id: the URL is
// authoritative for Space context, never a previously-selected Space held in
// local state. `artifact` is the one ID-resolved exception (see
// docs/design/portal-navigation-and-space-context.md) -- Portal looks it up
// by id alone and then reconciles the shell to the artifact's own Space.
export type Route =
  | { name: "login" }
  // Folds the former home/conversations/conversation routes: the composer
  // and its list live at the collection form, one open conversation at the
  // detail form.
  | { name: "chat"; spaceId: string; conversationId?: string }
  | { name: "task"; spaceId: string; taskId: string }
  | { name: "explore"; spaceId: string }
  | { name: "agents"; spaceId: string }
  | { name: "agent"; spaceId: string; agentId: string }
  | {
      name: "account"
      section?: "general" | "usage" | "webhook" | "invitations"
    }
  | {
      name: "space"
      spaceId: string
      section?: "overview" | "members" | "plugins" | "security" | "secrets" | "audit" | "memberNew"
    }
  | {
      name: "admin"
      section?:
        | "overview"
        | "administrators"
        | "accounts"
        | "spaces"
        | "models"
        | "calls"
        | "plugins"
        | "audit"
      // The account whose detail is open, so the panel survives a reload and can
      // be linked. Only meaningful for the accounts section.
      userId?: string
    }
  | { name: "workflows"; spaceId: string }
  | { name: "workflow"; spaceId: string; workflowId: string }
  | { name: "workflowRun"; spaceId: string; workflowRunId: string }
  | { name: "schedules"; spaceId: string }
  | { name: "issues"; spaceId: string }
  | { name: "issue"; spaceId: string; issueId: string }
  | { name: "artifacts"; spaceId: string }
  | { name: "artifact"; artifactId: string }
  | { name: "marketplace" }
  | { name: "help"; slug?: string }
  // An unrecognized hash, or a route name AppRouter has no case for -- never
  // silently falls through to Chat (docs/design/portal-navigation-and-space-context.md).
  | { name: "notFound" }

/** Every `Route["name"]` that carries a `spaceId` -- i.e. every Space-owned route. */
export type SpaceScopedRouteName = Extract<Route, { spaceId: string }>["name"]

/** One breadcrumb segment: a label and the route it links to. */
export interface BreadcrumbCrumb {
  label: string
  route: Route
}

// --- Agent (user-owned persona) ---

export interface Agent {
  id: string
  name: string
  description?: string
  instructions?: string
  model?: string
  plugins?: string[]
  sandboxNetworkTier?: string
  sandboxFilesystemTier?: string
  secretConsumption?: import("./api/types").ApiSecretConsumption
  revision: number
  createdAt: string
}

export interface AgentRevision {
  id: string
  agentId: string
  revision: number
  name: string
  description: string
  instructions: string
  model?: string
  createdBy: string
  createdAt: string
  createdLabel: string
}

export interface Issue {
  id: string
  userId: string
  parentIssueId?: string | null
  title: string
  description: string
  status: "todo" | "in_progress" | "done"
  /** The accountable person for this Issue, independent of who or what
   *  executes it. */
  ownerId?: string | null
  /** What is selected to perform the work -- an Agent or a Workflow, never a
   *  person. Both null means no executor is selected. */
  executorKind?: "agent" | "workflow" | null
  executorId?: string | null
  createdBy: string
  createdAt: string
  updatedAt: string
  updatedLabel: string
  /** Optimistic-concurrency token; an update must send the version it read. */
  version: number
  /** Derived server-side per response, never stored. */
  childCount: number
  doneChildCount: number
  commentCount: number
}

export interface Workflow {
  id: string
  spaceId: string
  name: string
  description: string
  definition: string
  status: "draft" | "published" | "archived"
  revision: number
  createdBy: string
  createdAt: string
  updatedAt: string
  updatedLabel: string
}

export interface WorkflowRevision {
  id: string
  workflowId: string
  revision: number
  name: string
  description: string
  definition: string
  status: string
  createdBy: string
  createdAt: string
  createdLabel: string
}

export interface WorkflowRun {
  id: string
  workflowId: string
  workflowRevision?: number | null
  issueId?: string | null
  status: "pending" | "running" | "succeeded" | "failed" | "canceled"
  createdBy: string
  createdAt: string
  startedAt?: string | null
  endedAt?: string | null
  errorMessage?: string | null
  result?: unknown | null
  createdLabel: string
}

export interface WorkflowNodeRun {
  id: string
  workflowRunId: string
  nodeId: string
  nodeIndex: number
  nodeType: string
  needs?: string[] | null
  issueAccess?: string | null
  targetAgentId?: string | null
  agentRevision?: number | null
  agentName?: string | null
  agentDescription?: string | null
  agentInstructions?: string | null
  prompt: string
  status: "pending" | "running" | "succeeded" | "failed" | "canceled" | "blocked"
  taskId?: string | null
  taskRunId?: string | null
  resolvedInput?: string | null
  output?: string | null
  errorMessage?: string | null
  createdAt: string
  startedAt?: string | null
  endedAt?: string | null
}

export interface IssueFlowRun {
  run: WorkflowRun
  steps: WorkflowNodeRun[]
}

export interface OutputSource {
  sourceType: string
  taskId?: string
  taskRunId?: string
  conversationId?: string
  workflowRunId?: string | null
  workflowNodeRunId?: string | null
  workflowNodeId?: string | null
}

export interface IssueOutput {
  id: string
  title: string
  kind: string
  /** The artifact's whole address, no run needed. */
  artifactId: string
  filename?: string
  mediaType?: string
  sizeBytes?: number
  source: OutputSource
  createdAt: string
}

export interface IssueFlow {
  issue: Issue
  /** Set on a sub-issue; children is set on a parent. Never both. */
  parent: Issue | null
  children: Issue[]
  workflow?: Workflow | null
  runs: IssueFlowRun[]
  agentTasks: Task[]
  latestResult: IssueOutput | null
  outputs: IssueOutput[]
  total: number
}

// --- Explore (user file structure) ---

export type ExploreNode =
  | { id: string; name: string; type: "folder"; children: ExploreNode[] }
  | { id: string; name: string; type: "file"; content?: string }
