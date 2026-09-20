import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, ButtonLink } from "@buildmax/gui"
import type { Agent, Issue, IssueFlow, IssueFlowRun, Workflow } from "../../lib/types"
import type { ApiIssueComment, ApiIssueFlowResponse, ApiSpaceMember } from "../../lib/api/types"
import { buildHash, navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { statusLabel } from "../../lib/statusLabels"
import { ApiRequestError } from "../../lib/api/client"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"
import { taskIsRetryable, taskIsStoppable } from "../../lib/taskStatus"
import {
  apiAgentToAgent,
  apiIssueOutputToIssueOutput,
  apiIssueToIssue,
  apiTaskToTask,
  apiWorkflowRunToWorkflowRun,
  apiWorkflowNodeRunToWorkflowNodeRun,
  apiWorkflowToWorkflow,
} from "../../lib/api/mappers"
import { getAgents } from "../../features/agents"
import { cancelTask, retryTask } from "../../features/tasks"
import {
  createIssue,
  getIssueFlow,
  IssueDiscussion,
  OutputsList,
  runIssueAgent,
  updateIssue,
} from "../../features/issues"
import { RunTraceModal } from "../../features/runs"
import { getSpaceMembers } from "../../features/spaces/api"
import { getWorkflows, runIssueWorkflow } from "../../features/workflows"
import { useSpace } from "../../contexts/SpaceContext"
import { useApp } from "../../contexts/AppContext"

interface IssueDetailProps {
  token: string | null
  spaceId: string
  issueId: string
  userId?: string
}

// The four user-level areas an Issue is read through: what is being done and
// who or what is on it (Overview), the conversation about it (Discussion),
// what it produced (Results), and its full execution history (Runs). Each is
// independent -- nothing here duplicates another tab's content.
type IssueTab = "overview" | "discussion" | "results" | "runs"

const ISSUE_TABS: { id: IssueTab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "discussion", label: "Discussion" },
  { id: "results", label: "Results" },
  { id: "runs", label: "Runs" },
]

function mapIssueFlow(api: ApiIssueFlowResponse): IssueFlow {
  return {
    issue: apiIssueToIssue(api.issue),
    parent: api.parent ? apiIssueToIssue(api.parent) : null,
    children: (api.children ?? []).map(apiIssueToIssue),
    workflow: api.workflow ? apiWorkflowToWorkflow(api.workflow) : null,
    runs: api.runs.map((item) => ({
      run: apiWorkflowRunToWorkflowRun(item.run),
      steps: item.steps.map(apiWorkflowNodeRunToWorkflowNodeRun),
    })),
    agentTasks: api.agent_tasks.map(apiTaskToTask),
    latestResult: api.latest_result ? apiIssueOutputToIssueOutput(api.latest_result) : null,
    outputs: (api.outputs ?? []).map(apiIssueOutputToIssueOutput),
    total: api.total,
  }
}

function formatTimestamp(rfc3339: string): string {
  return new Date(rfc3339).toLocaleString()
}

function latestRun(flow: IssueFlow | null): IssueFlowRun | null {
  return flow?.runs[0] ?? null
}

export function IssueDetail({ token, spaceId, issueId, userId }: IssueDetailProps) {
  const { currentUserRole } = useSpace()
  const { setEntityLabel } = useApp()
  const [tab, setTab] = useState<IssueTab>("overview")
  const [flow, setFlow] = useState<IssueFlow | null>(null)
  const [traceRunId, setTraceRunId] = useState<string | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [members, setMembers] = useState<ApiSpaceMember[]>([])
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [status, setStatus] = useState<Issue["status"]>("todo")
  const [ownerValue, setOwnerValue] = useState("")
  const [executorValue, setExecutorValue] = useState("")
  const [editing, setEditing] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [runningWorkflow, setRunningWorkflow] = useState(false)
  const [runningAgent, setRunningAgent] = useState(false)
  const [cancelingTaskId, setCancelingTaskId] = useState<string | null>(null)
  const [retryingTaskId, setRetryingTaskId] = useState<string | null>(null)
  // Save persists; Run schedules work and spends quota. They are different
  // user intentions with different failure modes, so each gets its own error
  // and success feedback rather than one shared banner that leaves it unclear
  // which action actually failed.
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saveMessage, setSaveMessage] = useState<string | null>(null)
  const [runError, setRunError] = useState<string | null>(null)
  const [unavailable, setUnavailable] = useState<ResourceUnavailableKind | null>(null)
  const [subIssueTitle, setSubIssueTitle] = useState("")
  const [addingSubIssue, setAddingSubIssue] = useState(false)
  const [subIssueError, setSubIssueError] = useState<string | null>(null)
  // Owned by the Discussion panel's fetch and mirrored here for the tab's
  // comment count badge.
  const [comments, setComments] = useState<ApiIssueComment[]>([])
  const canAssignWorkflow = currentUserRole === "owner" || currentUserRole === "admin"

  const load = useCallback(async () => {
    if (!token || !spaceId) {
      setFlow(null)
      setAgents([])
      setWorkflows([])
      setMembers([])
      setLoading(false)
      return
    }
    setLoading(true)
    setLoadError(null)
    setUnavailable(null)
    try {
      const [flowApi, agentsApi, membersApi, workflowsApi] = await Promise.all([
        getIssueFlow(spaceId, issueId, token),
        getAgents(spaceId, token),
        getSpaceMembers(spaceId, token),
        getWorkflows(spaceId, token),
      ])
      const mapped = mapIssueFlow(flowApi)
      setFlow(mapped)
      setAgents(agentsApi.map(apiAgentToAgent))
      setMembers(membersApi)
      setWorkflows(workflowsApi.workflows.map(apiWorkflowToWorkflow))
      setTitle(mapped.issue.title)
      setDescription(mapped.issue.description)
      setStatus(mapped.issue.status)
      setOwnerValue(mapped.issue.ownerId ?? "")
      setExecutorValue(
        mapped.issue.executorKind && mapped.issue.executorId
          ? `${mapped.issue.executorKind}:${mapped.issue.executorId}`
          : "",
      )
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 404) {
        setUnavailable("notFound")
      } else if (err instanceof ApiRequestError && err.status === 403) {
        setUnavailable("forbidden")
      } else {
        setUnavailable("error")
      }
      setFlow(null)
      setLoadError(getErrorMessage(err, "Failed to load issue detail"))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, issueId])

  useEffect(() => {
    void load()
  }, [load])

  // Publish the loaded title so the breadcrumb reads "Issues / <title>"
  // instead of the opaque id, and updates in place after a rename.
  useEffect(() => {
    if (flow) setEntityLabel(flow.issue.id, flow.issue.title)
  }, [flow, setEntityLabel])

  const currentRun = latestRun(flow)
  const currentRunLatestTaskId =
    [...(currentRun?.steps ?? [])].reverse().find((step) => step.taskId)?.taskId ?? null
  const latestAgentTask = flow?.agentTasks[0] ?? null
  const isWorkflowAssigned = flow?.issue.executorKind === "workflow" && Boolean(flow.issue.executorId)
  const isAgentAssigned = flow?.issue.executorKind === "agent" && Boolean(flow.issue.executorId)
  const assignedWorkflowStatus =
    flow?.workflow?.status ??
    workflows.find((workflow) => workflow.id === flow?.issue.executorId)?.status
  const publishedAssignableWorkflows = workflows.filter(
    (workflow) => workflow.status === "published" || workflow.id === flow?.issue.executorId,
  )

  const openChildCount = (flow?.issue.childCount ?? 0) - (flow?.issue.doneChildCount ?? 0)

  const agentNames = useMemo(() => {
    const out: Record<string, string> = {}
    for (const agent of agents) out[agent.id] = agent.name
    return out
  }, [agents])

  // Owner and Executor are independent: an Issue can have one, the other,
  // both, or neither, which one combined field could never say at once.
  function ownerLabel(issue: Issue): string | null {
    if (!issue.ownerId) return null
    if (issue.ownerId === userId) return "Me"
    const member = members.find((item) => item.user_id === issue.ownerId)
    if (member?.user_name) return member.user_name
    if (member?.user_email) return member.user_email
    return member ? `Member ${member.user_id.slice(0, 8)}` : "Member"
  }

  function executorLabel(issue: Issue): string | null {
    if (issue.executorKind === "agent") {
      return agents.find((agent) => agent.id === issue.executorId)?.name || "Agent"
    }
    if (issue.executorKind === "workflow") {
      return flow?.workflow?.name || workflows.find((workflow) => workflow.id === issue.executorId)?.name || "Workflow"
    }
    return null
  }

  // A compact combined summary, for a sub-issue row where there is no room
  // for two labeled lines.
  function summaryLabel(issue: Issue): string {
    const parts = [ownerLabel(issue), executorLabel(issue)].filter((label): label is string => label != null)
    return parts.length > 0 ? parts.join(" · ") : "Unassigned"
  }

  function memberLabel(member: ApiSpaceMember): string {
    if (member.user_id === userId) return "Me"
    if (member.user_name && member.user_name.trim() !== "") return member.user_name
    if (member.user_email && member.user_email.trim() !== "") return member.user_email
    return `Member ${member.user_id.slice(0, 8)}`
  }

  function handleSave() {
    if (!token || !spaceId || !flow) return
    const [executorKind, executorID] = executorValue ? executorValue.split(":") : ["", ""]
    if (executorKind === "workflow" && !canAssignWorkflow) {
      setSaveError("Workflow assignment is limited to space owners and admins")
      return
    }
    setSaving(true)
    setSaveError(null)
    setSaveMessage(null)
    updateIssue(
      spaceId,
      flow.issue.id,
      {
        version: flow.issue.version,
        title: title.trim(),
        description,
        status,
        owner_id: ownerValue,
        executor_kind: (executorKind as "agent" | "workflow" | "") || "",
        executor_id: executorID || "",
      },
      token,
    )
      .then(() => {
        setEditing(false)
        setSaveMessage(
          executorKind === "agent" || executorKind === "workflow"
            ? "Saved. This did not start a run — use Run to schedule one."
            : "Saved.",
        )
        return load()
      })
      .catch((err) => {
        // A conflict means someone else saved first. Reloading is what makes
        // the form usable again: the version it holds is stale, so every
        // further save would be refused for the same reason.
        if (err instanceof ApiRequestError && err.status === 409) {
          setSaveError("This issue changed while you were editing it. It has been reloaded — reapply your change.")
          void load()
          return
        }
        setSaveError(getErrorMessage(err, "Failed to update issue"))
      })
      .finally(() => setSaving(false))
  }

  function handleAddSubIssue() {
    if (!token || !spaceId || !flow) return
    const trimmed = subIssueTitle.trim()
    if (!trimmed || addingSubIssue) return
    setAddingSubIssue(true)
    setSubIssueError(null)
    createIssue(spaceId, { title: trimmed, parent_issue_id: flow.issue.id }, token)
      .then(() => {
        setSubIssueTitle("")
        return load()
      })
      .catch((err) => setSubIssueError(getErrorMessage(err, "Failed to add sub-issue")))
      .finally(() => setAddingSubIssue(false))
  }

  function handleRunWorkflow() {
    if (!token || !spaceId || !flow || editing) return
    setRunningWorkflow(true)
    setRunError(null)
    runIssueWorkflow(spaceId, flow.issue.id, token)
      .then((detail) => {
        void load()
        // A successful schedule links straight to what it started, not back to
        // this form -- that link is the confirmation Run succeeded.
        navigate({ name: "workflowRun", spaceId, workflowRunId: detail.run.id })
      })
      .catch((err) => setRunError(getErrorMessage(err, "Failed to run workflow")))
      .finally(() => setRunningWorkflow(false))
  }

  function handleCancelTask(taskId: string) {
    if (!token || !spaceId || cancelingTaskId) return
    setCancelingTaskId(taskId)
    setRunError(null)
    cancelTask(spaceId, taskId, token)
      .then(() => load())
      .catch((err) => setRunError(getErrorMessage(err, "Failed to stop this run")))
      .finally(() => setCancelingTaskId(null))
  }

  function handleRetryTask(taskId: string) {
    if (!token || !spaceId || retryingTaskId) return
    setRetryingTaskId(taskId)
    setRunError(null)
    retryTask(spaceId, taskId, token)
      .then(() => load())
      .catch((err) => setRunError(getErrorMessage(err, "Failed to retry this run")))
      .finally(() => setRetryingTaskId(null))
  }

  function handleRunAgent() {
    if (!token || !spaceId || !flow || editing) return
    setRunningAgent(true)
    setRunError(null)
    runIssueAgent(spaceId, flow.issue.id, token)
      .then((created) => {
        void load()
        // Same contract as Run Workflow: land on the run this started, not on
        // a form that just quietly reloaded.
        navigate({ name: "task", spaceId, taskId: created.id })
      })
      .catch((err) => setRunError(getErrorMessage(err, "Failed to run agent")))
      .finally(() => setRunningAgent(false))
  }

  if (loading) {
    return (
      <div className="page-activity">
        <p className="page-activity__empty">Loading…</p>
      </div>
    )
  }

  if (unavailable) {
    return (
      <ResourceUnavailable
        resourceLabel="Issue"
        kind={unavailable}
        errorMessage={loadError}
        onRetry={() => void load()}
        backLabel="Back to Issues"
        onBack={() => navigate({ name: "issues", spaceId })}
      />
    )
  }

  // Neither loading nor unavailable at this point, so the load succeeded and
  // set flow -- this is what lets the rest of the render use flow.issue
  // directly instead of threading `flow?.` through every field access below.
  if (!flow) return null

  // Run is disabled until its executor is actually runnable; these name the
  // specific reason rather than leaving a disabled button unexplained.
  const workflowRunDisabledReason = !isWorkflowAssigned
    ? null
    : assignedWorkflowStatus !== "published"
      ? "This workflow is not published, so it cannot be run yet."
      : null
  const agentStillExists = agents.some((agent) => agent.id === flow?.issue.executorId)
  const agentRunDisabledReason = !isAgentAssigned
    ? null
    : !agentStillExists
      ? "The assigned agent no longer exists."
      : null

  function cancelEditing() {
    if (!flow) return
    setTitle(flow.issue.title)
    setDescription(flow.issue.description)
    setStatus(flow.issue.status)
    setOwnerValue(flow.issue.ownerId ?? "")
    setExecutorValue(
      flow.issue.executorKind && flow.issue.executorId
        ? `${flow.issue.executorKind}:${flow.issue.executorId}`
        : "",
    )
    setSaveError(null)
    setEditing(false)
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{flow.issue.title}</h1>
          <p className="page-activity__subtitle">
            {statusLabel(flow.issue.status)} · {ownerLabel(flow.issue) ?? "Unassigned owner"}
          </p>
        </div>
        <div className="page-activity__actions">
          <ButtonLink variant="tertiary" href={buildHash({ name: "issues", spaceId })}>
            Back to Issues
          </ButtonLink>
          <Button variant="tertiary" disabled={loading || editing} onClick={() => void load()}>
            Refresh
          </Button>
          {!editing ? <Button variant="secondary" onClick={() => {
            setTab("overview")
            setEditing(true)
          }}>Edit issue</Button> : null}
        </div>
      </div>

      <section className="issues-page__panel issue-detail-page__summary" aria-label="Issue summary">
        <div className="issues-page__toolbar">
          <h2 className="issues-page__section-title">Overview</h2>
          <span className="issues-page__status">{statusLabel(flow.issue.status)}</span>
        </div>
        <p className="issue-detail-page__description">{flow.issue.description || "No description yet."}</p>
        <div className="issues-page__meta-row">
          <span className="page-activity__meta">Owner: {ownerLabel(flow.issue) ?? "Unassigned"}</span>
          <span className="page-activity__meta">Executor: {executorLabel(flow.issue) ?? "None"}</span>
        </div>
        <div className="issue-detail-page__outcome">
          <strong>Latest result</strong>
          {flow.latestResult ? (
            <Button variant="tertiary" onClick={() => setTab("results")}>{flow.latestResult.title}</Button>
          ) : (
            <span className="page-activity__meta">No result yet.</span>
          )}
        </div>
        {!editing ? <div className="issues-page__form-actions">
          {isWorkflowAssigned ? <span className="page-activity__action-group">
            <Button variant="primary" busy={runningWorkflow} disabled={loading || workflowRunDisabledReason != null} title={workflowRunDisabledReason ?? undefined} onClick={handleRunWorkflow}>
              Run workflow
            </Button>
            {workflowRunDisabledReason ? <span className="page-activity__meta">{workflowRunDisabledReason}</span> : null}
          </span> : null}
          {isAgentAssigned ? <span className="page-activity__action-group">
            <Button variant="primary" busy={runningAgent} disabled={loading || agentRunDisabledReason != null} title={agentRunDisabledReason ?? undefined} onClick={handleRunAgent}>
              Run agent
            </Button>
            {agentRunDisabledReason ? <span className="page-activity__meta">{agentRunDisabledReason}</span> : null}
          </span> : null}
          {runError ? <p className="modal__error" role="alert">{runError}</p> : null}
          {saveMessage ? <p className="page-activity__meta" role="status">{saveMessage}</p> : null}
        </div> : null}
      </section>

      <>
        <nav className="issue-detail-page__tabs" aria-label="Issue sections">
          {ISSUE_TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              className={
                t.id === tab ? "issue-detail-page__tab issue-detail-page__tab--active" : "issue-detail-page__tab"
              }
              aria-current={t.id === tab}
              disabled={editing && t.id !== "overview"}
              onClick={() => setTab(t.id)}
            >
              {t.label}
              {t.id === "discussion" && comments.length > 0 ? (
                <span className="issue-detail-page__tab-count">{comments.length}</span>
              ) : null}
              {t.id === "results" && flow.outputs.length > 0 ? (
                <span className="issue-detail-page__tab-count">{flow.outputs.length}</span>
              ) : null}
            </button>
          ))}
        </nav>


          {tab === "overview" ? (
            <div className="issue-detail-page__panel issue-detail-page__grid">
              {editing ? <section className="issues-page__panel">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">Edit issue</h2>
                  <span className="issues-page__status">{statusLabel(flow.issue.status)}</span>
                </div>
                <div className="issues-page__form">
                  <label className="issues-page__field">
                    <span className="issues-page__field-label">Title</span>
                    <input className="issues-page__input" value={title} onChange={(e) => setTitle(e.target.value)} />
                  </label>
                  <label className="issues-page__field">
                    <span className="issues-page__field-label">Description</span>
                    <textarea
                      className="issues-page__textarea"
                      rows={8}
                      value={description}
                      onChange={(e) => setDescription(e.target.value)}
                    />
                  </label>
                  <div className="issue-detail-page__split">
                    <label className="issues-page__field">
                      <span className="issues-page__field-label">Business Status</span>
                      <select className="issues-page__select" value={status} onChange={(e) => setStatus(e.target.value as Issue["status"])}>
                        <option value="todo">To do</option>
                        <option value="in_progress">In progress</option>
                        <option value="done">Done</option>
                      </select>
                    </label>
                    <label className="issues-page__field">
                      <span className="issues-page__field-label">Owner</span>
                      <select className="issues-page__select" value={ownerValue} onChange={(e) => setOwnerValue(e.target.value)}>
                        <option value="">Unassigned</option>
                        {members.map((member) => (
                          <option key={member.user_id} value={member.user_id}>
                            {memberLabel(member)}
                          </option>
                        ))}
                      </select>
                      <span className="issues-page__field-label">Who is accountable for this issue.</span>
                    </label>
                  </div>
                  <label className="issues-page__field">
                    <span className="issues-page__field-label">Executor</span>
                    <select className="issues-page__select" value={executorValue} onChange={(e) => setExecutorValue(e.target.value)}>
                      <option value="">None</option>
                      {agents.map((agent) => (
                        <option key={agent.id} value={`agent:${agent.id}`}>{agent.name}</option>
                      ))}
                      {canAssignWorkflow
                        ? publishedAssignableWorkflows.map((workflow) => (
                            <option key={workflow.id} value={`workflow:${workflow.id}`}>
                              {workflow.name}{workflow.status !== "published" ? ` (${workflow.status})` : ""}
                            </option>
                          ))
                        : null}
                    </select>
                    <span className="issues-page__field-label">
                      {canAssignWorkflow
                        ? "What runs the work. Only `published` workflows are available for new assignment."
                        : "What runs the work. Workflow assignment is limited to space owners and admins."}
                    </span>
                  </label>
                  {status === "done" && openChildCount > 0 ? (
                    <p className="page-activity__meta">
                      {openChildCount} sub-issue{openChildCount === 1 ? " is" : "s are"} still open. Closing this issue
                      anyway is allowed — sub-issue status is never rolled up.
                    </p>
                  ) : null}
                  <div className="issues-page__meta-row">
                    <div className="page-activity__meta">Owner: {ownerLabel(flow.issue) ?? "Unassigned"}</div>
                    <div className="page-activity__meta">Executor: {executorLabel(flow.issue) ?? "None"}</div>
                    <div className="page-activity__meta">Created: {formatTimestamp(flow.issue.createdAt)}</div>
                    <div className="page-activity__meta">Updated: {formatTimestamp(flow.issue.updatedAt)}</div>
                  </div>
                  <div className="issues-page__form-actions">
                    <Button variant="secondary" disabled={saving} onClick={cancelEditing}>Cancel</Button>
                    <Button
                      variant="primary"
                      busy={saving}
                      disabled={saving || loading || !title.trim()}
                      onClick={handleSave}
                    >
                      Save changes
                    </Button>
                  </div>
                  {saveError ? (
                    <p className="page-activity__empty">{saveError}</p>
                  ) : saveMessage ? (
                    <p className="page-activity__meta">{saveMessage}</p>
                  ) : null}
                </div>
              </section> : null}

              <section className="issues-page__panel">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">{flow.parent ? "Parent Issue" : "Sub-issues"}</h2>
                  {flow.parent ? null : (
                    <span className="page-activity__meta">
                      {flow.issue.childCount === 0
                        ? "None yet"
                        : `${flow.issue.doneChildCount}/${flow.issue.childCount} done`}
                    </span>
                  )}
                </div>
                {flow.parent ? (
                  <div className="issue-detail-page__parent">
                    <ButtonLink variant="tertiary" href={buildHash({ name: "issue", spaceId, issueId: flow.parent.id })}>
                      ← {flow.parent.title}
                    </ButtonLink>
                    <p className="page-activity__meta">
                      This is a sub-issue. Sub-issues cannot have sub-issues of their own.
                    </p>
                  </div>
                ) : (
                  <>
                    {flow.children.length === 0 ? (
                      <p className="page-activity__empty">No sub-issues yet.</p>
                    ) : (
                      <ul className="issue-detail-page__children">
                        {flow.children.map((child) => (
                          <li key={child.id} className="issue-detail-page__child">
                            <ButtonLink variant="tertiary" href={buildHash({ name: "issue", spaceId, issueId: child.id })}>
                              {child.title}
                            </ButtonLink>
                            <span className="issues-page__status">{statusLabel(child.status)}</span>
                            <span className="page-activity__meta">{summaryLabel(child)}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                    {/* A sub-issue starts as a title. Everything else is filled in
                        on its own page, so decomposing an issue stays one keystroke
                        per piece. */}
                    <div className="issue-detail-page__child-actions">
                      <input
                        className="issues-page__input"
                        value={subIssueTitle}
                        placeholder="New sub-issue title"
                        onChange={(e) => setSubIssueTitle(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault()
                            handleAddSubIssue()
                          }
                        }}
                      />
                      <Button
                        variant="secondary"
                        busy={addingSubIssue}
                        disabled={subIssueTitle.trim() === ""}
                        onClick={handleAddSubIssue}
                      >
                        Add sub-issue
                      </Button>
                    </div>
                    {subIssueError ? <p className="page-activity__empty">{subIssueError}</p> : null}
                  </>
                )}
              </section>

              <section className="issues-page__panel issue-detail-page__wide">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">Latest Outcome</h2>
                  <span className="issues-page__status">{statusLabel(currentRun?.run.status ?? latestAgentTask?.status ?? "no_runs")}</span>
                </div>
                {currentRun ? (
                  <div className="workflow-run-page__meta">
                    <div><strong>Latest run:</strong> {currentRun.run.id}</div>
                    <div><strong>Workflow:</strong> {flow.workflow?.name ?? currentRun.run.workflowId}</div>
                    <div><strong>Started:</strong> {currentRun.run.startedAt ? formatTimestamp(currentRun.run.startedAt) : "Not started"}</div>
                    <div><strong>Steps:</strong> {currentRun.steps.filter((step) => step.status === "succeeded").length} / {currentRun.steps.length} done</div>
                    {currentRun.run.errorMessage ? <div className="modal__error">{currentRun.run.errorMessage}</div> : null}
                    <div className="workflow-run-page__step-actions">
                      <ButtonLink variant="secondary" href={buildHash({ name: "workflowRun", spaceId, workflowRunId: currentRun.run.id })}>
                        Open Run Detail
                      </ButtonLink>
                      {currentRunLatestTaskId ? (
                        <ButtonLink variant="tertiary" href={buildHash({ name: "task", spaceId, taskId: currentRunLatestTaskId })}>
                          Open Task
                        </ButtonLink>
                      ) : null}
                      <Button variant="tertiary" onClick={() => setTab("runs")}>
                        View all runs
                      </Button>
                    </div>
                  </div>
                ) : latestAgentTask ? (
                  <div className="workflow-run-page__meta">
                    <div><strong>Latest agent task:</strong> {latestAgentTask.id}</div>
                    <div><strong>Agent:</strong> {executorLabel(flow.issue) ?? "Agent"}</div>
                    <div><strong>Created:</strong> {formatTimestamp(latestAgentTask.createdAt)}</div>
                    <div><strong>Status:</strong> {statusLabel(latestAgentTask.status)}</div>
                    <div className="workflow-run-page__step-actions">
                      <ButtonLink variant="secondary" href={buildHash({ name: "task", spaceId, taskId: latestAgentTask.id })}>
                        Open Task
                      </ButtonLink>
                      {taskIsStoppable(latestAgentTask.status) ? (
                        <Button
                          variant="danger"
                          busy={cancelingTaskId === latestAgentTask.id}
                          onClick={() => handleCancelTask(latestAgentTask.id)}
                        >
                          Stop Run
                        </Button>
                      ) : null}
                      {taskIsRetryable(latestAgentTask.status) ? (
                        <Button
                          variant="secondary"
                          busy={retryingTaskId === latestAgentTask.id}
                          onClick={() => handleRetryTask(latestAgentTask.id)}
                        >
                          Retry Run
                        </Button>
                      ) : null}
                      <Button variant="tertiary" onClick={() => setTab("runs")}>
                        View all runs
                      </Button>
                    </div>
                  </div>
                ) : (
                  <p className="page-activity__empty">No execution runs recorded for this issue yet.</p>
                )}
              </section>
            </div>
          ) : null}

          {tab === "discussion" ? (
            <div className="issue-detail-page__panel">
              <section className="issues-page__panel issue-detail-page__wide">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">Discussion</h2>
                  <div className="issues-page__toolbar-actions">
                    <span className="page-activity__meta">
                      {comments.length === 0
                        ? "No comments"
                        : `${comments.length} comment${comments.length === 1 ? "" : "s"}`}
                    </span>
                    <ButtonLink variant="tertiary" href={buildHash({ name: "explore", spaceId })}>
                      Files
                    </ButtonLink>
                  </div>
                </div>
                <IssueDiscussion
                  spaceId={spaceId}
                  issueId={flow.issue.id}
                  token={token}
                  userId={userId ?? null}
                  canModerate={currentUserRole === "owner"}
                  members={members}
                  agentNames={agentNames}
                  onOpenTrace={(taskRunId) => setTraceRunId(taskRunId)}
                  onCommentsChanged={setComments}
                />
              </section>
            </div>
          ) : null}

          {tab === "results" ? (
            <div className="issue-detail-page__panel">
              <section className="issues-page__panel issue-detail-page__wide">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">Results</h2>
                  <span className="page-activity__meta">
                    {flow.outputs.length === 0
                      ? "No outputs yet"
                      : `${flow.outputs.length} output${flow.outputs.length === 1 ? "" : "s"}`}
                  </span>
                </div>
                <OutputsList
                  outputs={flow.outputs}
                  token={token}
                  onOpenConversation={(conversationId) => navigate({ name: "chat", spaceId, conversationId })}
                  onOpenRun={(workflowRunId) => navigate({ name: "workflowRun", spaceId, workflowRunId })}
                  onOpenTrace={(taskRunId) => setTraceRunId(taskRunId)}
                />
              </section>
            </div>
          ) : null}

          {tab === "runs" ? (
            <div className="issue-detail-page__panel issue-detail-page__grid">
              <section className="issues-page__panel">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">Run History</h2>
                  <span className="page-activity__meta">{flow.total} total</span>
                </div>
                <p className="page-activity__subtitle">
                  Each workflow run's steps and diagnostics live on its own run detail page.
                </p>
                {flow.runs.length === 0 ? (
                  <p className="page-activity__empty">No runs yet.</p>
                ) : (
                  <ul className="workflow-page__runs">
                    {flow.runs.map((item) => (
                      <li key={item.run.id}>
                        <button
                          type="button"
                          className="workflow-page__run-row"
                          onClick={() => navigate({ name: "workflowRun", spaceId, workflowRunId: item.run.id })}
                        >
                          <span>
                            <strong>{item.run.id}</strong>
                            <span className="page-activity__meta workflow-detail-page__run-id">
                              {item.run.createdLabel}
                            </span>
                          </span>
                          <span className="issues-page__status">{statusLabel(item.run.status)}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </section>

              <section className="issues-page__panel">
                <div className="issues-page__toolbar">
                  <h2 className="issues-page__section-title">Agent Run Sequence</h2>
                  <span className="page-activity__meta">{flow.agentTasks.length} tasks</span>
                </div>
                {flow.agentTasks.length === 0 ? (
                  <p className="page-activity__empty">No agent runs recorded for this issue yet.</p>
                ) : (
                  <ul className="workflow-page__runs">
                    {flow.agentTasks.map((task) => (
                      <li key={task.id}>
                        <button
                          type="button"
                          className="workflow-page__run-row"
                          onClick={() => navigate({ name: "task", spaceId, taskId: task.id })}
                        >
                          <span>
                            <strong>{task.title}</strong>
                            <span className="page-activity__meta workflow-detail-page__run-id">
                              {task.timeLabel}
                            </span>
                          </span>
                          <span className="issues-page__status">{statusLabel(task.status)}</span>
                        </button>
                        {taskIsStoppable(task.status) ? (
                          <Button
                            variant="danger"
                            size="compact"
                            busy={cancelingTaskId === task.id}
                            onClick={() => handleCancelTask(task.id)}
                          >
                            Stop Run
                          </Button>
                        ) : null}
                        {taskIsRetryable(task.status) ? (
                          <Button
                            variant="secondary"
                            size="compact"
                            busy={retryingTaskId === task.id}
                            onClick={() => handleRetryTask(task.id)}
                          >
                            Retry Run
                          </Button>
                        ) : null}
                        <pre className="workflow-page__step-output">{task.summary}</pre>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            </div>
          ) : null}
        </>
      <RunTraceModal
        open={traceRunId != null}
        spaceId={spaceId}
        token={token}
        taskRunId={traceRunId}
        onClose={() => setTraceRunId(null)}
      />
    </div>
  )
}
