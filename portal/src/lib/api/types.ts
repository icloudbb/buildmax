/**
 * API request/response types (snake_case as returned by the server).
 * Single source of truth for DTOs; see design/archive/004-portal-api-contract.md for historical contract context.
 */

export interface LoginUser {
  id: string
  email: string
  name: string
}

/**
 * What the Portal auth endpoints (login and session) return.
 *
 * The renewable credential never appears here: the server keeps it in an
 * HttpOnly refresh cookie the script cannot read. Only the short-lived access
 * token, its lifetime, and the account come back in the body.
 */
export interface PortalSessionResponse {
  access_token: string
  /** Access token lifetime in seconds. */
  expires_in?: number
  user: LoginUser
}

export interface OtpRequestResponse {
  message: string
}

/**
 * Which ways in the deployment offers, from GET /api/auth/methods. It carries
 * no issuer, client, or policy: only what the sign-in page needs to decide
 * whether to show local inputs, an SSO button, or both.
 */
export interface AuthMethods {
  /** "all", "system_admins", or "off". */
  local_login: string
  oidc: {
    enabled: boolean
    display_name?: string
  }
}

/** Legacy: user is the top-level owner. Kept for API compatibility. */
export interface ApiWorkspace {
  id: string
  name: string
  owner_user_id?: string
  created_at?: string
}

/** Agent as returned by space-scoped agent endpoints. */
export interface ApiAgent {
  id: string
  user_id: string
  space_id: string
  name: string
  description: string
  instructions: string
  /**
   * Catalog model name this agent's runs call, or empty for the deployment
   * default. Takes effect on the managed worker transport.
   */
  model?: string
  /**
   * Catalog plugin names this agent loads for a background run. Nothing is
   * inherited from the space's activations: an agent that names none loads none.
   */
  plugins?: string[]
  /**
   * config.SandboxNetworkTier / config.SandboxFilesystemTier this agent
   * declares. Empty inherits the space's default, then the strictest
   * baseline. See docs/design/agent-sandbox-policy.md.
   */
  sandbox_network_tier?: string
  sandbox_filesystem_tier?: string
  /** How this agent consumes Space Secrets. See docs/design/space-secrets.md §6. */
  secret_consumption?: ApiSecretConsumption
  revision: number
  created_at: string
}

export interface ApiAgentRevision {
  agent_id: string
  revision: number
  name: string
  description: string
  instructions: string
  model?: string
  created_by: string
  created_at: string
}

export interface ApiAgentRevisionListResponse {
  revisions: ApiAgentRevision[]
  total: number
}

export interface ApiIssue {
  id: string
  user_id: string
  space_id: string
  parent_issue_id?: string | null
  title: string
  description: string
  status: string
  owner_id?: string | null
  executor_kind?: string | null
  executor_id?: string | null
  created_by: string
  created_at: string
  updated_at: string
  /** Optimistic-concurrency token. Send it back on any update of this issue. */
  version: number
  /** Derived per response, never stored. Absent on older servers. */
  child_count?: number
  done_child_count?: number
  comment_count?: number
}

export interface ApiIssueComment {
  id: string
  issue_id: string
  /**
   * "agent" is a run the deployment scheduled and recorded. "local_agent" is an
   * agent on someone's own machine, reported by them over their session — a
   * claim, not something the deployment observed. author_id is that person.
   */
  author_kind: "user" | "agent" | "local_agent" | "system"
  author_id: string
  body: string
  source_task_id?: string | null
  source_task_run_id?: string | null
  created_at: string
  /** Absent until the comment is edited. */
  edited_at?: string | null
}

export interface ApiIssueCommentsResponse {
  comments: ApiIssueComment[]
  total: number
}

export interface ApiWorkflow {
  id: string
  space_id: string
  name: string
  description: string
  definition: string
  status: string
  revision: number
  created_by: string
  created_at: string
  updated_at: string
}

export interface ApiWorkflowRevision {
  workflow_id: string
  revision: number
  name: string
  description: string
  definition: string
  status: string
  created_by: string
  created_at: string
}

export interface ApiWorkflowRevisionListResponse {
  revisions: ApiWorkflowRevision[]
  total: number
}

export interface ApiWorkflowListResponse {
  workflows: ApiWorkflow[]
}

export interface ApiWorkflowRun {
  id: string
  workflow_id: string
  workflow_revision?: number | null
  issue_id?: string | null
  schedule_id?: string | null
  status: string
  created_by: string
  created_at: string
  started_at?: string | null
  ended_at?: string | null
  error_message?: string | null
  result?: unknown
}

export interface ApiWorkflowNodeRun {
  id: string
  workflow_run_id: string
  node_id: string
  node_index: number
  node_type: string
  needs?: string[] | null
  issue_access?: string | null
  target_agent_id?: string | null
  agent_name?: string | null
  agent_description?: string | null
  agent_instructions?: string | null
  agent_revision?: number | null
  prompt: string
  status: string
  task_id?: string | null
  task_run_id?: string | null
  resolved_input?: string | null
  output?: string | null
  error_message?: string | null
  created_at: string
  started_at?: string | null
  ended_at?: string | null
}

export interface ApiWorkflowRunListResponse {
  runs: ApiWorkflowRun[]
  total: number
}

export interface ApiWorkflowRunDetailResponse {
  run: ApiWorkflowRun
  steps: ApiWorkflowNodeRun[]
}

export interface ApiIssueFlowRun {
  run: ApiWorkflowRun
  steps: ApiWorkflowNodeRun[]
}

export interface ApiOutputSource {
  source_type: string
  task_id?: string
  task_run_id?: string
  conversation_id?: string
  workflow_run_id?: string | null
  workflow_node_run_id?: string | null
  workflow_node_id?: string | null
}

export interface ApiIssueOutput {
  id: string
  title: string
  kind: string
  /** The artifact's whole address, no run needed. */
  artifact_id: string
  filename?: string
  media_type?: string
  size_bytes?: number
  source: ApiOutputSource
  created_at: string
}

export interface ApiIssueFlowResponse {
  issue: ApiIssue
  /** Set on a sub-issue; children is set on a parent. Never both. */
  parent?: ApiIssue | null
  children: ApiIssue[]
  workflow?: ApiWorkflow | null
  runs: ApiIssueFlowRun[]
  agent_tasks: ApiTask[]
  latest_result?: ApiIssueOutput | null
  outputs: ApiIssueOutput[]
  total: number
}

export interface ApiIssuesListResponse {
  issues: ApiIssue[]
  total: number
}

/** Paginated tasks response when using limit/offset/executed_only (if backend supports it). */
export interface ApiTasksListResponse {
  tasks: ApiTask[]
  total: number
}

/** A recurring schedule as returned by the schedule endpoints. */
export interface ApiSchedule {
  id: string
  space_id: string
  /** "agent" or "workflow": what the schedule fires. */
  executor_kind: string
  executor_id: string
  created_by: string
  name?: string
  input: string
  cron_expr: string
  timezone: string
  enabled: boolean
  pause_reason?: string
  next_fire_at: string
  last_fire_at?: string | null
  /** The task (agent) or workflow run (workflow) the last firing produced. */
  last_fire_ref?: string | null
  consecutive_failures: number
  created_at: string
  updated_at: string
}

export interface ApiScheduleListResponse {
  schedules: ApiSchedule[]
  total: number
}

/** Task as returned by space-scoped task endpoints. */
export interface ApiTask {
  id: string
  space_id: string
  conversation_id?: string
  session_id: string | null
  status: string
  input: string
  title?: string
  output: string | null
  created_by: string
  created_at: string
  started_at: string | null
  ended_at: string | null
  error_message: string | null
  agent_id?: string | null
  issue_id?: string | null
  /** The run behind the current status. Keys the trace route. */
  last_run_id?: string | null
  /** Set only when the task has neither issue_id nor conversation_id: the
   *  workflow run that dispatched it. */
  workflow_run_id?: string | null
}

export interface ApiTaskRun {
  id: string
  task_id: string
  /** The immutable predecessor whose session bundle this run restores. */
  previous_task_run_id?: string | null
  input: string
  created_by?: string
  created_by_type?: string
  trigger_source?: string
  status: string
  output?: string | null
  error_message?: string | null
  created_at: string
  started_at?: string | null
  ended_at?: string | null
  agent_revision?: number | null
  space_agent_instructions_revision?: number | null
  retry_of_task_run_id?: string | null
}

export interface ApiTaskRunsResponse {
  runs: ApiTaskRun[]
}

/** Where one task run came from, as returned by the run provenance endpoint. */
export interface ApiRunProvenance {
  task_run_id: string
  task_id: string
  status: string
  /** What the worker was given. Compare with source_message. */
  input: string
  created_by?: string
  created_by_type?: string
  trigger_source?: string
  retry_of_task_run_id?: string | null
  created_at: string
  source_message?: ApiRunSourceMessage | null
  agent?: ApiRunAgent | null
  space_instructions?: ApiRunSpaceInstructions | null
  /** The releases this run actually resolved, fixed at dispatch. */
  plugin_pins?: ApiRunPluginPin[]
  /** What this run published, looked up by its own id, not owned by it. */
  artifacts?: ApiRunArtifact[]
}

/** One release a run was given. Not what the agent currently names — see
 * docs/design/portal-data-and-plugin-surfaces.md. */
export interface ApiRunPluginPin {
  plugin_name: string
  version: string
  digest: string
}

/** An artifact this run published, enough to recognise and open it. */
export interface ApiRunArtifact {
  id: string
  title?: string
  filename: string
  media_type?: string
  size_bytes?: number
  created_at: string
}

/** The Space-wide instruction layer a run received. */
export interface ApiRunSpaceInstructions {
  /** The revision handed to the worker; 0 means no instructions were configured. */
  revision: number
  /** The Space's current revision, for detecting edits after the run. */
  current_revision?: number
}

/** The agent definition a run executed under. */
export interface ApiRunAgent {
  id: string
  name?: string
  /** The revision the run was handed. 0 when it was not recorded. */
  revision?: number
  /** What the definition says now; ahead of revision means it was edited since. */
  current_revision?: number
  deleted?: boolean
}

/** What the person actually said, quoted for comparison with the run input. */
export interface ApiRunSourceMessage {
  id: string
  content: string
  truncated: boolean
  created_at: string
}

/**
 * Response from the space-scoped cancel endpoint.
 *
 * `cancel_requested` is the difference that matters to the UI: false means the
 * run is already over, true means it is still executing and its worker has been
 * asked to stop.
 */
export interface CancelTaskResponse {
  task_id: string
  task_run_id: string
  status: string
  cancel_requested: boolean
}

/**
 * The run a retry created, and the one it repeats.
 *
 * `task_run_id` is the new run; `retry_of_task_run_id` is the finished run whose
 * input it carries.
 */
export interface RetryTaskResponse {
  task_id: string
  task_run_id: string
  retry_of_task_run_id: string
  status: string
}

/**
 * A durable file the space keeps, addressed by its own opaque id.
 *
 * Not a run output: those are paths inside one run's directory and come from
 * the task-run routes instead. See docs/design/unified-artifacts.md section 5.3.
 */
export interface ApiArtifact {
  id: string
  space_id: string
  filename: string
  media_type: string
  size_bytes: number
  sha256: string
  title?: string
  created_by_type: string
  created_by_id?: string
  source_type: string
  source_id?: string
  /**
   * How the server will show this content: "inline" to render directly,
   * "sandbox" for an active document (HTML) that runs only in an opaque-origin
   * frame, or "none" for download-only.
   */
  preview: "inline" | "sandbox" | "none"
  expires_at?: string
  created_at: string
}

export interface ApiArtifactList {
  items: ApiArtifact[]
  total: number
}

/**
 * A public share link. `url`, `download_url`, and `token` are present only in
 * the create response — a hashed token cannot be reconstructed, so a later
 * listing shows the link's metadata but never the link itself.
 */
export interface ApiArtifactShare {
  share_id: string
  artifact_id: string
  url?: string
  download_url?: string
  token?: string
  created_by_type: string
  created_by_id?: string
  expires_at?: string
  revoked_at?: string
  retrieval_count: number
  last_retrieved_at?: string
  created_at: string
}

export interface ApiArtifactShareList {
  items: ApiArtifactShare[]
}

/** Public metadata for a shared artifact, served without a session. */
export interface ApiSharedMeta {
  filename: string
  media_type: string
  size_bytes: number
  title?: string
  preview: "inline" | "sandbox" | "none"
  created_at: string
}

/** The execution boundary a run actually ran under. */
export interface ApiTraceBoundary {
  /** False means nothing confined the run's shell commands. Never assume true. */
  sandboxed: boolean
  mode?: string
  backend?: string
  /** The layer chain that decided the boundary, e.g. ["default:worker", "policy"]. */
  sources?: string[]
  downgraded?: boolean
}

/**
 * How a run's MCP transports were treated. Absent means the trace predates this
 * record — unknown, not stdio-allowed and not confined.
 */
export interface ApiTraceMCP {
  /** The unattended-worker profile refused stdio MCP for this run. */
  stdio_disabled: boolean
  /** Resolved remote transport kinds active for the run, a subset of http/sse. */
  remote_transports?: string[]
}

/** One tool call in a run. */
export interface ApiTraceToolCall {
  name: string
  duration_ms?: number
  /** The call's file_path argument, when it had one. */
  path?: string
  denied?: boolean
  deny_reason?: string
}

/**
 * A task run's trace summary. The server deliberately omits model output, tool
 * arguments, and tool results — this describes the shape of a run, not its
 * content.
 */
export interface ApiTaskRunTrace {
  task_run_id: string
  run_id?: string
  model?: string
  started_at?: string
  ended_at?: string
  boundary?: ApiTraceBoundary
  /** How the run's MCP transports were treated. Absent means unknown. */
  mcp?: ApiTraceMCP
  llm_calls: number
  tool_calls: number
  compactions: number
  prompt_tokens: number
  completion_tokens: number
  tools?: ApiTraceToolCall[]
  /** The tools list was bounded; tool_calls still counts them all. */
  tools_truncated?: boolean
  files_changed?: string[]
  /** What happened to this run's workspace checkpoint. */
  workspace?: ApiTraceWorkspace
  /** Terminal error; empty when the run succeeded. */
  error?: string
  /** False means the run wrote no terminal record. Do not read it as success. */
  complete: boolean
}

/**
 * A run's workspace-checkpoint state: whether it restored a base and whether it
 * committed a result. Read-only status the run recorded; a field is empty when
 * the step did not apply (a first run restores nothing, a reply-only run
 * captures nothing).
 */
export interface ApiTraceWorkspace {
  /** "restored", "failed", or empty when no base was restored. */
  restore_status?: string
  restore_error?: string
  /** "committed", "failed", or empty when none was captured. */
  checkpoint_status?: string
  checkpoint_error?: string
}

/**
 * One managed model call a run made, as the governance ledger recorded it.
 *
 * This is a different record from the trace, and the difference matters when
 * reading a run: the trace is what the agent did, written by the run itself,
 * while this is what the deployment was asked to serve and account for. Only
 * calls that went through the managed gateway appear here — a deployment
 * running in direct mode records none, because the worker called the provider
 * itself and the server never saw it.
 *
 * It carries no prompts, tool payloads, or generated content, and omits the
 * catalog entry the model name resolved to: that is the operator's routing, not
 * the caller's.
 */
export interface ApiTaskRunLLMCall {
  id: string
  user_id?: string
  task_id?: string
  surface?: string
  session_id?: string
  /** The catalog model the run named. */
  model?: string
  streaming: boolean
  accepted_at: string
  first_delta_at?: string
  completed_at?: string
  status: string
  error_class?: string
  attempts?: number
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  /**
   * The cached parts of `prompt_tokens`, not tokens on top of it. A reader that
   * adds them to the prompt count counts the same tokens twice.
   */
  cache_read_tokens?: number
  cache_write_tokens?: number
  /**
   * Separates a provider that reported nothing from one that reported zero.
   * Without it an absent count reads as a free call.
   */
  usage_source?: string
  /**
   * What the call is estimated to have cost, priced at the rates recorded when
   * it ran rather than at whatever the catalog says now. Absent when the model
   * was unpriced or the provider reported no usage — an unpriced call is an
   * unknown, and a zero would read as a free one.
   */
  cost?: ApiLLMCallCost
}

/**
 * One call's estimated spend, in nano-units of `currency`: one currency unit is
 * 1e9 of them. Integers so a run sums exactly instead of accumulating float
 * error across hundreds of calls.
 *
 * `baseline` is what the same tokens would have cost with no caching at all.
 * It is the only honest way to say whether caching helped: comparing `total`
 * against zero would report a saving on a call that only ever wrote.
 */
export interface ApiLLMCallCost {
  currency: string
  uncached: number
  cache_read: number
  cache_write: number
  output: number
  total: number
  baseline: number
}

/**
 * One recorded action. The server records that something happened and who did
 * it — never prompts, generated content, tool output, or credentials.
 */
export interface ApiAuditEvent {
  id: string
  space_id?: string
  actor_type: string
  actor_id: string
  action: string
  target_type?: string
  target_id?: string
  /** The task run this action was taken on behalf of, when there is one. */
  task_run_id?: string
  /** A short non-sensitive note — a role name, a model alias. */
  detail?: string
  created_at: string
}

export interface ApiAuditEventsResponse {
  events: ApiAuditEvent[]
  total: number
}

// --- Deployment administration ---
//
// These come from /api/admin, which is deployment-scoped rather than
// space-scoped. Nothing here carries space content: an administrator learns that
// an account or a space exists, never what is in it.

/** One deployment-scoped grant. */
export interface ApiSystemGrant {
  id: string
  user_id: string
  role: string
  granted_by: string
  granted_at: string
  revoked_at?: string
  /** Resolved on the grants list so a reader does not see only ids. */
  email?: string
}

/** The caller's own deployment authority, from GET /api/admin/me. */
export interface ApiAdminMe {
  user_id: string
  roles: string[]
  grants: ApiSystemGrant[]
}

/** Everyone who can operate the deployment, from GET /api/admin/grants. */
export interface ApiSystemGrantsResponse {
  grants: ApiSystemGrant[]
}

/** One account as an administrator sees it. Never a hash, never a token. */
export interface ApiAdminUser {
  id: string
  email: string
  name?: string
  quota_tier?: string
  has_password: boolean
  /** Non-null means every credential this account holds is refused. */
  disabled_at?: string
  last_login_at?: string
  last_login_platform?: string
  created_at: string
}

export interface ApiAdminUsersResponse {
  users: ApiAdminUser[]
  total: number
}

/** One live login chain, safe metadata only — never a token or its hash. */
export interface ApiAdminSession {
  session_id: string
  platform?: string
  created_at: string
  last_rotated_at: string
  expires_at: string
}

export interface ApiAdminSessionsResponse {
  sessions: ApiAdminSession[]
}

export interface ApiAdminUserSpace {
  space_id: string
  name: string
  role: string
}

export interface ApiAdminUserDetail extends ApiAdminUser {
  spaces: ApiAdminUserSpace[]
  /** Live login chains, not tokens. */
  session_count: number
  system_roles: string[]
}

export interface ApiAdminLoginCode {
  code: string
  expires_at: string
}

export interface ApiAdminSessionsRevoked {
  revoked: number
}

/** An account plus what a disable's orchestrated cleanup did. */
export interface ApiAdminUserAfterDisable extends ApiAdminUser {
  sessions_revoked: number
  webhook_keys_retired?: number
  schedules_paused?: number
  runs_canceled?: number
}

/** One Space an account belongs to and the role it holds there. */
export interface ApiDeactivationMembership {
  space_id: string
  role: string
}

/**
 * What disabling an account would stop, as counts and ids only — never Space
 * content. Read before committing the change.
 */
export interface ApiDeactivationImpact {
  live_sessions: number
  webhook_keys: number
  memberships: ApiDeactivationMembership[]
  sole_owned_space_ids: string[]
  enabled_schedules: number
  active_runs_by_status: Record<string, number>
  cancellation_bound: string
}

export interface ApiAdminDependency {
  name: string
  /** "ok" or "failed". The reason is deliberately not reported. */
  status: string
}

export interface ApiAdminSchemaMigration {
  id: string
  applied_at: string
}

export interface ApiAdminSystem {
  version: string
  schema_migrations: ApiAdminSchemaMigration[]
  dependencies: ApiAdminDependency[]
  ready: boolean
  worker_run_mode?: string
  worker_llm_transport?: string
  /** Empty when no worker path passes one, which is every deployment today. */
  sandbox_surface?: string
  allow_signup: boolean
  task_runs: Record<string, number>
  system_admins: number
  server_time: number
}

/**
 * One catalog model. There is no credential field, and there is none in the
 * server's record either — the key leaves the store only for the component that
 * opens a provider connection.
 */
export interface ApiAdminModel {
  id: string
  name: string
  provider_type: string
  api_url: string
  model: string
  context_window?: number
  call_timeout?: number
  max_tokens?: number
  reasoning?: string
  prompt_cache?: boolean
  vision?: boolean
  capabilities?: string[]
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface ApiAdminModelsResponse {
  models: ApiAdminModel[]
  /** Model name callers get when they name none. Empty means the first enabled one. */
  default_model?: string
}

/**
 * One call's estimated spend, in nano-units of `currency` — one currency unit is
 * 1e9 of them — so a client sums them exactly. Absent when the model was unpriced
 * or the provider reported no usage; a zero would read as a free call.
 */
export interface ApiAdminLLMCallCost {
  currency: string
  uncached: number
  cache_read: number
  cache_write: number
  output: number
  total: number
  /** What the same tokens would have cost with no caching, to judge whether it helped. */
  baseline: number
}

/**
 * One managed LLM call as a deployment administrator sees it: accounting and
 * routing metadata, never prompts, tool arguments, or generated content.
 */
export interface ApiAdminLLMCall {
  id: string
  user_id?: string
  task_id?: string
  task_run_id?: string
  surface?: string
  session_id?: string
  /** What the caller asked for; the three that follow are how it was served. */
  model?: string
  target_id?: string
  provider_type?: string
  upstream_model?: string
  streaming: boolean
  accepted_at: string
  first_delta_at?: string
  completed_at?: string
  status: string
  error_class?: string
  attempts?: number
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  /** The cached parts of prompt_tokens, not tokens on top of it. */
  cache_read_tokens?: number
  cache_write_tokens?: number
  /** Separates a provider that reported nothing from one that reported zero. */
  usage_source?: string
  cost?: ApiAdminLLMCallCost
}

export interface ApiAdminLLMCallsResponse {
  calls: ApiAdminLLMCall[]
  total: number
}

/** One catalog entry in the private plugin Marketplace. */
export interface ApiPlugin {
  name: string
  display_name?: string
  description?: string
  /** Non-zero means retired: out of the default catalog, and no new releases. */
  archived_at?: string
  created_by: string
  created_at: string
  updated_at: string
}

/**
 * What a release says it contributes.
 *
 * Names, transports, executables, and hosts — never arguments, header values,
 * environment values, prompts, or file contents. The server's inspection is
 * what decides that, and this shape can only carry what it kept.
 */
export interface ApiPluginInspection {
  skills?: string[]
  subagents?: { name: string; tools?: string[]; model?: string }[]
  mcp?: { id: string; transport: string; executable?: string; host?: string }[]
  hooks?: {
    event: string
    type: string
    matcher?: string
    executable?: string
    host?: string
    mcp_server?: string
    mcp_tool?: string
  }[]
  env_refs?: string[]
  plugin_paths?: string[]
  warnings?: string[]
}

/**
 * The publisher's claim about the checkout the bytes came from.
 *
 * Unlike the digest, the server cannot verify any of it, so it is shown as a
 * claim rather than as proof.
 */
/** Who fills a space's plugin activation list. */
export type ApiPluginCuration = "open" | "curated"

/** How an activation came to exist. */
export type ApiPluginActivationOrigin = "curated" | "automatic"

/**
 * One space's pinned use of one catalog plugin.
 *
 * The pin is the point: a release published after this row was written cannot
 * change what a run loads until somebody moves it.
 */
export interface ApiPluginActivation {
  id: string
  space_id: string
  plugin_name: string
  version: string
  digest: string
  enabled: boolean
  origin: ApiPluginActivationOrigin
  activated_by: string
  activated_at: string
  updated_by?: string
  updated_at: string
}

/**
 * What a space has activated, and who fills the list.
 *
 * The mode travels with the activations because reading one without the other
 * misleads: an empty list means "nothing activated yet" when open and "nothing
 * may be named" when curated.
 */
export interface ApiPluginActivationsResponse {
  curation: ApiPluginCuration
  activations: ApiPluginActivation[]
}

export interface ApiPluginReleaseSource {
  remote_url?: string
  commit?: string
  branch?: string
  dirty?: boolean
}

/** One immutable published version. */
export interface ApiPluginRelease {
  plugin_name: string
  version: string
  min_buildmax_version?: string
  digest: string
  object_key: string
  size_bytes: number
  inspection: ApiPluginInspection
  source: ApiPluginReleaseSource
  published_by: string
  published_at: string
  /** Non-zero means withdrawn from default selection. Nothing is deleted. */
  yanked_at?: string
  yanked_by?: string
  yanked_reason?: string
}

export interface ApiPluginsResponse {
  plugins: ApiPlugin[]
}

export interface ApiPluginReleasesResponse {
  releases: ApiPluginRelease[]
}

/** One entry and everything published under it. */
export interface ApiPluginResponse {
  plugin: ApiPlugin
  releases: ApiPluginRelease[]
}

export interface ApiAdminSpace {
  id: string
  name: string
  personal: boolean
  quota_tier?: string
  member_count: number
  created_by?: string
  created_at: string
}

export interface ApiAdminSpacesResponse {
  spaces: ApiAdminSpace[]
  total: number
}

export interface ApiAdminSpaceMember {
  user_id: string
  email?: string
  role: string
}

export interface ApiAdminSpaceDetail extends ApiAdminSpace {
  members: ApiAdminSpaceMember[]
  usage?: ApiUsage
}

/** Upload response from the space-scoped upload endpoint. */
export interface UploadResponse {
  uploaded: string[]
}

/** Usage as returned by space usage endpoints (and the legacy personal alias). */
export interface ApiUsage {
  run_count: number
  total_tokens: number
  tier: string
  period_days: number
  max_runs_per_period?: number
  max_tokens_per_period?: number
  /**
   * What the space's artifacts hold now. A stock, not a windowed total:
   * period_days does not apply to it, and it falls only when an artifact is
   * deleted or expires. Absent on a deployment with no artifact storage.
   */
  storage_bytes?: number
  max_storage_bytes?: number
  /**
   * The caller's own managed calls from signed-in CLI and Desktop sessions
   * over the same period. They belong to no space, so the figures above do
   * not include them. Absent on a deployment without a call ledger.
   */
  managed_calls?: ApiManagedCallTotals
}

export interface ApiManagedCallTotals {
  call_count: number
  total_tokens: number
  /** One entry per currency; amounts in nano-units. Never summed across currencies. */
  costs: ApiLLMCallCost[]
  /** Calls against a model that had no price when they ran. */
  unpriced_calls: number
}

/** Tier 1 conversation as returned by space-scoped conversation endpoints. */
export interface ApiConversation {
  id: string
  user_id: string
  space_id: string
  channel: string
  title?: string
  created_at: string
  created_by: string
}

export interface ApiSpace {
  id: string
  name: string
  personal_for_user_id?: string | null
  created_at?: string
}

export interface ApiSpaceAgentInstructions {
  instructions: string
  revision: number
}

/**
 * The tiers an agent that declares neither inherits. See
 * docs/design/agent-sandbox-policy.md §9 M3.
 */
export interface ApiSpaceSandboxDefaults {
  sandbox_network_tier?: string
  sandbox_filesystem_tier?: string
}

/**
 * A Space Secret: a group of named items, sealed. The API never returns an item
 * value -- item_names lists the keys present, and there is no reveal route. See
 * docs/design/space-secrets.md.
 */
export interface ApiSecret {
  id: string
  space_id: string
  name: string
  description: string
  provider: string
  state: "active" | "disabled" | "destroyed"
  item_names: string[]
  created_by: string
  created_at: string
  updated_at: string
}

export interface ApiSecretListResponse {
  secrets: ApiSecret[]
}

export interface ApiCreateSecretRequest {
  name: string
  description?: string
  items: Record<string, string>
}

/**
 * One of two shapes over the edit route: items replaces the whole map (a
 * raw-JSON editor), set/remove patch named keys (a row editor). Sending both is
 * refused.
 */
export interface ApiEditSecretRequest {
  items?: Record<string, string>
  set?: Record<string, string>
  remove?: string[]
}

export interface ApiSetSecretStateRequest {
  state: "active" | "disabled" | "destroyed"
}

/**
 * How an agent consumes a Space Secret's item as an environment variable: a
 * selected item under a chosen name, or -- when item is empty -- the whole
 * group under each item's own name with an optional prefix.
 */
export interface ApiSecretEnvGrant {
  secret: string
  item?: string
  env_name?: string
  prefix?: string
  optional?: boolean
}

export interface ApiSecretConsumption {
  env?: ApiSecretEnvGrant[]
}

export interface ApiSpaceMember {
  space_id: string
  user_id: string
  role: string
  created_at?: string
  user_name?: string
  user_email?: string
}

/**
 * A pending offer of space membership against an account that already
 * exists. Never carries a code -- see docs/design/space-membership-lifecycle.md.
 */
export interface ApiInvitation {
  id: string
  space_id: string
  user_id: string
  role: string
  invited_by: string
  expires_at: string
  created_at: string
}

export interface ApiMemberRole {
  space_id: string
  user_id: string
  role: string
}

export interface ApiMemberLoginCode {
  code: string
  expires_at: string
}

/** Response from the space-scoped list conversations endpoint. */
export interface ApiConversationsListResponse {
  conversations: ApiConversation[]
  total: number
}

/** Response from the space-scoped create conversation endpoint. */
export interface CreateConversationResponse {
  conversation_id: string
  reply?: string
}

/** Message as returned by the space-scoped list conversation messages endpoint. */
export interface ApiConversationMessage {
  id: string
  role: string
  content: string
  channel?: string | null
  created_at: string
}

/** Response from the space-scoped list conversation messages endpoint. */
export interface ApiConversationMessagesResponse {
  messages: ApiConversationMessage[]
}

/** Response from the space-scoped add conversation message endpoint. */
export interface AddConversationMessageResponse {
  reply: string
}
