import type { Fixtures } from "./harness"

/**
 * One small, fixed world every screenshot is rendered from: an account, the
 * Space it owns, and a handful of objects in it. Timestamps sit a little before
 * the harness clock, so relative times read the same on every run.
 *
 * Shapes follow `src/lib/api/types.ts`. When a page starts reading a field
 * that is missing here, its screenshot changes and the reviewer sees why.
 */

export const SPACE = "spc_visual"
const USER = { id: "usr_ada", email: "ada@example.com", name: "Ada Lovelace" }
const space = `/api/spaces/${SPACE}`

const agent = {
  id: "agt_reviewer",
  user_id: USER.id,
  space_id: SPACE,
  name: "Release reviewer",
  description: "Reads a change and reports what a release note should say.",
  instructions: "Summarize the change for the release notes. Name every user-visible effect.",
  model: "default",
  revision: 3,
  created_at: "2026-02-10T10:00:00Z",
}

const definition = JSON.stringify(
  {
    schema_version: 1,
    nodes: [
      { id: "draft", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Draft the release note." } },
      {
        id: "check",
        type: "agent_task",
        needs: ["draft"],
        agent: { id: agent.id },
        input: { instruction: "Check the draft against the merged changes." },
      },
    ],
  },
  null,
  2,
)

const workflow = {
  id: "wfl_release",
  space_id: SPACE,
  name: "Release notes",
  description: "Drafts a release note and checks it against what merged.",
  definition,
  status: "draft",
  revision: 2,
  created_by: USER.id,
  created_at: "2026-02-20T09:00:00Z",
  updated_at: "2026-03-01T11:15:00Z",
}

function issue(n: number, title: string, status: string, extra: Record<string, unknown> = {}) {
  return {
    id: `iss_${n}`,
    user_id: USER.id,
    space_id: SPACE,
    parent_issue_id: null,
    title,
    description: "",
    status,
    owner_id: USER.id,
    executor_kind: null,
    executor_id: null,
    created_by: USER.id,
    created_at: `2026-03-0${Math.min(n, 4)}T08:00:00Z`,
    updated_at: `2026-03-0${Math.min(n, 4)}T09:30:00Z`,
    version: 1,
    comment_count: 0,
    ...extra,
  }
}

const issues = [
  issue(1, "Write the 0.3 release notes", "in_progress", {
    description:
      "Collect every user-visible change merged since 0.2 and describe it for operators.\n\n- Group by surface\n- Link the migration guide",
    executor_kind: "agent",
    executor_id: agent.id,
    comment_count: 2,
    child_count: 2,
    done_child_count: 1,
  }),
  issue(2, "Rotate the staging object-storage key", "todo"),
  issue(3, "Document the sandbox network tiers", "todo", { executor_kind: "workflow", executor_id: workflow.id }),
  issue(4, "Remove the retired export endpoint", "done"),
]

const task = {
  id: "tsk_notes",
  space_id: SPACE,
  session_id: "ses_notes",
  status: "succeeded",
  input: "Draft the release note for 0.3 from the merged pull requests.",
  title: "Draft the 0.3 release note",
  output: "## 0.3\n\n- Portal shows dates in the interface language.\n- Workers refuse to start without a sandbox.",
  created_by: USER.id,
  created_at: "2026-03-04T14:00:00Z",
  started_at: "2026-03-04T14:00:05Z",
  ended_at: "2026-03-04T14:03:40Z",
  error_message: null,
  agent_id: agent.id,
  issue_id: "iss_1",
  origin: { kind: "issue", id: "iss_1", name: "Write the 0.3 release notes" },
  last_run_id: "trn_notes_2",
}

const taskRuns = [
  {
    id: "trn_notes_1",
    task_id: task.id,
    input: task.input,
    created_by: USER.id,
    created_by_type: "user",
    trigger_source: "manual",
    status: "failed",
    error_message: "model provider returned 503",
    failure_class: "model",
    created_at: "2026-03-04T13:50:00Z",
    started_at: "2026-03-04T13:50:02Z",
    ended_at: "2026-03-04T13:51:10Z",
    agent_revision: 3,
  },
  {
    id: "trn_notes_2",
    task_id: task.id,
    input: task.input,
    created_by: USER.id,
    created_by_type: "user",
    trigger_source: "manual",
    status: "succeeded",
    output: task.output,
    created_at: "2026-03-04T14:00:00Z",
    started_at: "2026-03-04T14:00:05Z",
    ended_at: "2026-03-04T14:03:40Z",
    agent_revision: 3,
    retry_of_task_run_id: "trn_notes_1",
  },
]

const usage = { run_count: 42, total_tokens: 1_284_000, tier: "standard", period_days: 30, max_runs_per_period: 500 }

// Administration: a ready deployment whose one Space has running work, a
// request waiting on its members, and failures of each kind an operator
// relays. Every value is one the server sends, so a raw identifier shown as a
// label is visible in the screenshot.
const adminOwner = { user_id: USER.id, email: USER.email, role: "owner", kind: "human" }
const adminWorkflowRuntime = {
  waiting_requests: { input: 1 },
  oldest_waiting_request_at: "2026-03-04T12:00:00Z",
  next_request_expiry_at: "2026-03-05T12:00:00Z",
  workflow_failures: { node_timeout: 1 },
}
const adminSpace = {
  id: SPACE,
  name: "Analytical Engine",
  personal: false,
  quota_tier: "pro",
  member_count: 2,
  created_by: USER.id,
  created_at: "2026-01-12T09:00:00Z",
}
const quotaTiers = [
  { tier_name: "free_trial", max_runs_per_period: 10, max_tokens_per_period: 100_000, max_storage_bytes: 0, period_days: 30 },
  { tier_name: "pro", max_runs_per_period: 1000, max_tokens_per_period: 10_000_000, max_storage_bytes: 0, period_days: 30 },
]

const adminFixtures: Fixtures = {
  "GET /api/admin/me": {
    user_id: USER.id,
    roles: ["system_admin"],
    grants: [{ id: "sgr_ada", user_id: USER.id, role: "system_admin", granted_by: "buildmax-server", granted_at: "2026-01-12T09:00:00Z" }],
  },
  "GET /api/admin/config": { warnings: [] },
  "GET /api/admin/system": {
    version: "v0.3.0",
    schema_migrations: [{ id: "0007_task_run_failure_class", applied_at: "2026-02-01T10:00:00Z" }],
    dependencies: [
      { name: "database", status: "ok" },
      { name: "object_storage", status: "ok" },
    ],
    ready: true,
    worker_run_mode: "k8s_job",
    worker_llm_transport: "direct",
    allow_signup: false,
    task_runs: { SUCCEEDED: 1284, FAILED: 4, CANCELED: 2, RUNNING: 1, SCHEDULED: 1 },
    runtime: {
      oldest_unstarted_at: "2026-03-04T15:22:00Z",
      stale_running: 0,
      stale_after_seconds: 900,
      failures: { space_configuration: 3, model: 1 },
      failure_window_hours: 24,
      ...adminWorkflowRuntime,
    },
    system_admins: 1,
    server_time: "2026-03-04T15:30:00Z",
  },
  "GET /api/admin/runtime/spaces": {
    spaces: [
      {
        space_id: SPACE,
        name: "Analytical Engine",
        personal: false,
        owners: [adminOwner],
        active: { RUNNING: 1, SCHEDULED: 1 },
        oldest_active_at: "2026-03-04T15:22:00Z",
        failures: { space_configuration: 3, model: 1 },
        ...adminWorkflowRuntime,
        oldest_waiting_workflow_run_id: "wfr_signoff",
        latest_failed_workflow_run_id: "wfr_nightly",
      },
    ],
    total: 1,
    failure_window_hours: 24,
  },
  "GET /api/admin/spaces": { spaces: [adminSpace], total: 1 },
  [`GET /api/admin/spaces/${SPACE}`]: {
    ...adminSpace,
    members: [
      adminOwner,
      { user_id: "usr_grace", email: "grace@example.com", role: "member", kind: "human" },
    ],
    usage: { run_count: 258, total_tokens: 1_284_000, tier: "pro", period_days: 30, max_runs_per_period: 1000, max_tokens_per_period: 10_000_000 },
  },
  "GET /api/admin/quota-tiers": { tiers: quotaTiers },
}

export const fixtures: Fixtures = {
  ...adminFixtures,
  // Ada administers the deployment, so the shell shows Administration.
  "POST /api/auth/portal/session": { access_token: "visual-token", expires_in: 3600, user: USER, system_roles: ["system_admin"] },
  // A CLI waiting at the sign-in confirmation page, started four minutes ago.
  "GET /api/auth/native-sign-ins": {
    platform: "cli",
    client_label: "ada-mbp · macOS",
    created_at: "2026-03-04T15:26:00Z",
    expires_at: "2026-03-04T15:36:00Z",
    session_expires_at: "2026-03-05T03:30:00Z",
  },
  // Account → Sessions: this browser, the CLI confirmed above, and a Desktop
  // that signed in with a password and has not been used since.
  "GET /api/auth/sessions": {
    sessions: [
      {
        session_id: "ses_portal",
        platform: "portal",
        auth_method: "oidc",
        created_at: "2026-03-04T09:12:00Z",
        last_seen_at: "2026-03-04T15:29:00Z",
        expires_at: "2026-03-04T21:12:00Z",
        current: true,
      },
      {
        session_id: "ses_cli",
        platform: "cli",
        auth_method: "oidc",
        client_label: "ada-mbp · macOS",
        created_at: "2026-03-04T10:02:00Z",
        last_seen_at: "2026-03-04T14:47:00Z",
        expires_at: "2026-03-04T21:12:00Z",
        current: false,
      },
      {
        session_id: "ses_desktop",
        platform: "desktop",
        auth_method: "password",
        created_at: "2026-02-27T08:40:00Z",
        last_seen_at: "0001-01-01T00:00:00Z",
        expires_at: "2026-05-28T08:40:00Z",
        current: false,
      },
    ],
  },
  "GET /api/spaces": [{ id: SPACE, name: "Analytical Engine", created_at: "2026-01-12T09:00:00Z" }],
  "GET /api/invitations": [],
  "GET /api/usage": usage,
  [`GET ${space}/usage`]: usage,
  [`GET ${space}/members`]: [
    { space_id: SPACE, user_id: USER.id, role: "owner", user_name: USER.name, user_email: USER.email, user_kind: "human", created_at: "2026-01-12T09:00:00Z" },
    { space_id: SPACE, user_id: "usr_grace", role: "member", user_name: "Grace Hopper", user_email: "grace@example.com", user_kind: "human", created_at: "2026-01-20T09:00:00Z" },
  ],
  [`GET ${space}/conversations`]: {
    conversations: [
      { id: "cnv_1", user_id: USER.id, space_id: SPACE, channel: "web", title: "Plan the 0.3 release", created_at: "2026-03-03T10:00:00Z", created_by: USER.id },
    ],
    total: 1,
  },
  [`GET ${space}/agents`]: [agent],
  [`GET ${space}/agents/${agent.id}`]: agent,
  [`GET ${space}/invitations`]: [],
  // An owner's Issue page reads Secret metadata to warn before a run whose
  // grant no longer resolves; none here, so no warning.
  [`GET ${space}/secrets`]: { secrets: [] },
  [`GET ${space}/workflows`]: { workflows: [workflow] },
  [`GET ${space}/agent-instructions`]: {
    instructions: "Write for operators. Prefer the exact command over a description of it.",
    revision: 4,
  },
  [`GET ${space}/issues`]: (url) => {
    // The collection asks for top-level issues; sub-issues are asked for by parent.
    if (url.searchParams.get("parent_id") === "iss_1") {
      return { issues: [issue(5, "Collect merged pull requests", "done", { parent_issue_id: "iss_1" })], total: 1 }
    }
    return { issues, total: issues.length }
  },
  // The Issues page leads with what waits on Ada: one of each group, and a
  // failed-run total above what is listed.
  [`GET ${space}/needs-me`]: {
    attention_issues: {
      items: [{ issue: issues[1], reason: "run_failed" }],
      total: 1,
    },
    failed_tasks: {
      items: [
        {
          ...task,
          id: "tsk_rotate",
          title: "Rotate the staging object-storage key",
          status: "FAILED",
          issue_id: "iss_2",
          origin: { kind: "issue", id: "iss_2", name: "Rotate the staging object-storage key" },
          output: null,
          error_message: "model provider returned 503",
          failure_class: "model",
          ended_at: "2026-03-04T13:10:00Z",
        },
      ],
      total: 3,
    },
    awaiting_answer_tasks: {
      // A direct Agent run: no origin, so its own title names it.
      items: [{ ...task, id: "tsk_ask", title: "Summarize the sandbox tiers", status: "SUCCEEDED", awaiting_answer: true, issue_id: null, origin: null }],
      total: 1,
    },
    workflow_requests: {
      items: [
        {
          id: "wrq_signoff",
          workflow_run_id: "wfr_signoff",
          workflow_id: workflow.id,
          node_run_id: "wnr_signoff",
          node_id: "approve",
          kind: "input",
          prompt: "Approve the draft release note before it is published.",
          status: "pending",
          created_at: "2026-03-04T12:00:00Z",
        },
      ],
      total: 1,
    },
  },
  [`GET ${space}/issues/iss_1/flow`]: {
    issue: issues[0],
    parent: null,
    children: [
      issue(5, "Collect merged pull requests", "done", { parent_issue_id: "iss_1" }),
      issue(6, "Draft the operator summary", "todo", { parent_issue_id: "iss_1" }),
    ],
    workflow: null,
    // The server's task status spelling: the Issue shows this run as succeeded.
    runs: [{ kind: "agent", task: { ...task, status: "SUCCEEDED" } }],
    outputs: [],
    total: 1,
  },
  [`GET ${space}/tasks/${task.id}`]: task,
  [`GET ${space}/tasks/${task.id}/runs`]: { runs: taskRuns },
  [`GET ${space}/workflows/${workflow.id}`]: workflow,
  [`GET ${space}/workflows/${workflow.id}/runs`]: { runs: [], total: 0 },
  [`GET ${space}/workflows/${workflow.id}/revisions`]: {
    revisions: [
      { workflow_id: workflow.id, revision: 2, name: workflow.name, description: workflow.description, definition, status: "draft", created_by: USER.id, created_at: "2026-03-01T11:15:00Z" },
      { workflow_id: workflow.id, revision: 1, name: workflow.name, description: workflow.description, definition, status: "draft", created_by: USER.id, created_at: "2026-02-20T09:00:00Z" },
    ],
    total: 2,
  },
}
