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
        depends_on: ["draft"],
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

export const fixtures: Fixtures = {
  "POST /api/auth/portal/session": { access_token: "visual-token", expires_in: 3600, user: USER },
  "GET /api/admin/me": { user_id: USER.id, roles: [], grants: [] },
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
