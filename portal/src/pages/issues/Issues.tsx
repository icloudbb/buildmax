import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { Agent, Issue } from "../../lib/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { statusLabel } from "../../lib/statusLabels"
import { apiAgentToAgent, apiIssueToIssue, apiWorkflowToWorkflow } from "../../lib/api/mappers"
import { createIssue, getIssues, updateIssue } from "../../features/issues"
import { getAgents } from "../../features/agents"
import { getSpaceMembers } from "../../features/spaces/api"
import { getWorkflows } from "../../features/workflows"
import { IssueModal } from "../../components/IssueModal"
import { useSpaceCapability } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed } from "../../state/permissionState"
import { useSpace } from "../../contexts/SpaceContext"
import type { ApiSpaceMember } from "../../lib/api/types"
import type { Workflow } from "../../lib/types"

const PAGE_SIZE = 10

interface IssuesProps {
  token: string | null
  spaceId: string
  userId?: string
}

export function Issues({ token, spaceId, userId }: IssuesProps) {
  const { currentUserRole } = useSpace()
  // null means "not yet successfully fetched", distinct from [] meaning this
  // page of the collection is genuinely empty. See deriveResourceState.
  const [issuesData, setIssuesData] = useState<Issue[] | null>(null)
  const [total, setTotal] = useState(0)
  const [agents, setAgents] = useState<Agent[]>([])
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [members, setMembers] = useState<ApiSpaceMember[]>([])
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [saving, setSaving] = useState(false)
  // Distinct from listError: the create-issue mutation's own error, shown
  // inside IssueModal rather than as a page-level Alert.
  const [createError, setCreateError] = useState<string | null>(null)
  const [partiallyCreatedId, setPartiallyCreatedId] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  // An entry appears here once a parent's children have been fetched; undefined
  // while the request is in flight.
  const [children, setChildren] = useState<Record<string, Issue[]>>({})
  const canAssignWorkflowState = useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin")
  const canAssignWorkflow = isAllowed(canAssignWorkflowState)

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const fetchIssues = useCallback(() => {
    if (!token || !spaceId) {
      setIssuesData(null)
      setAgents([])
      setWorkflows([])
      setMembers([])
      setTotal(0)
      setLoading(false)
      setListError(null)
      return Promise.resolve()
    }
    setLoading(true)
    setListError(null)
    return Promise.all([
      // The board shows top-level issues; sub-issues appear under the parent
      // they were split out of, not as siblings in the same list.
      getIssues(spaceId, token, { limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE, parentId: "none" }),
      getAgents(spaceId, token),
      getSpaceMembers(spaceId, token),
      getWorkflows(spaceId, token),
    ])
      .then(([issueRes, agentRes, memberRes, workflowRes]) => {
        setIssuesData(issueRes.issues.map(apiIssueToIssue))
        setTotal(issueRes.total)
        setAgents(agentRes.map(apiAgentToAgent))
        setMembers(memberRes)
        setWorkflows(workflowRes.workflows.map(apiWorkflowToWorkflow))
      })
      // issuesData from a prior successful fetch (if any) is left in place, so
      // a failed refresh reads as Stale rather than wiping the board.
      .catch((err) => setListError(classifyError(err, "Failed to load issues")))
      .finally(() => setLoading(false))
  }, [page, token, spaceId])

  const issuesState = useMemo(
    () => deriveResourceState({ loading, data: issuesData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, issuesData, listError]
  )

  useEffect(() => {
    void fetchIssues()
  }, [fetchIssues])

  useEffect(() => {
    setPage(1)
  }, [spaceId])

  // A reload invalidates every cached breakdown: statuses may have moved, and a
  // stale child list is worse than a second fetch.
  useEffect(() => {
    setExpanded({})
    setChildren({})
  }, [page, spaceId])

  function toggleChildren(issueId: string) {
    const nowOpen = !expanded[issueId]
    setExpanded((prev) => ({ ...prev, [issueId]: nowOpen }))
    if (!nowOpen || !token || !spaceId || children[issueId] !== undefined) return
    getIssues(spaceId, token, { limit: 100, parentId: issueId })
      .then((res) => setChildren((prev) => ({ ...prev, [issueId]: res.issues.map(apiIssueToIssue) })))
      .catch((err) => setListError(classifyError(err, "Failed to load sub-issues")))
  }

  // What is being done needs both halves at a glance: who is accountable and
  // what will execute, since an Issue can have one, the other, both, or
  // neither.
  function ownerLabel(issue: Issue): string | null {
    if (!issue.ownerId) return null
    if (issue.ownerId === userId) return "Me"
    const member = members.find((item) => item.user_id === issue.ownerId)
    if (member?.user_name) return member.user_name
    if (member?.user_email) return member.user_email
    if (member) return `Member ${member.user_id.slice(0, 8)}`
    return "Member"
  }

  function executorLabel(issue: Issue): string | null {
    if (issue.executorKind === "agent") {
      return agents.find((agent) => agent.id === issue.executorId)?.name || "Agent"
    }
    if (issue.executorKind === "workflow") {
      return workflows.find((workflow) => workflow.id === issue.executorId)?.name || "Workflow"
    }
    return null
  }

  function assigneeLabel(issue: Issue): string {
    const parts = [ownerLabel(issue), executorLabel(issue)].filter((label): label is string => label != null)
    return parts.length > 0 ? parts.join(" · ") : "Unassigned"
  }

  const pageLabel = useMemo(() => {
    if (total === 0) return "0 issues"
    const start = (page - 1) * PAGE_SIZE + 1
    const end = Math.min(page * PAGE_SIZE, total)
    return `${start}-${end} of ${total}`
  }, [page, total])

  async function handleCreate(values: {
    title: string
    description?: string
    status: Issue["status"]
    owner_id: string
    executor_kind: "agent" | "workflow" | ""
    executor_id: string
  }) {
    if (!token || !spaceId) return
    setSaving(true)
    setCreateError(null)
    setPartiallyCreatedId(null)
    try {
      const created = await createIssue(spaceId, { title: values.title, description: values.description }, token)
      const needsPatch =
        values.status !== "todo" ||
        values.owner_id !== "" ||
        values.executor_kind !== "" ||
        values.executor_id !== ""
      if (needsPatch) {
        try {
          await updateIssue(spaceId, created.id, {
            version: created.version,
            status: values.status,
            owner_id: values.owner_id,
            executor_kind: values.executor_kind,
            executor_id: values.executor_id,
          }, token)
        } catch (err) {
          // Creation has already committed. Preserve the new object's address
          // and do not offer another Create, which would make a duplicate.
          setPartiallyCreatedId(created.id)
          setCreateError(`Issue was created, but its details were not saved: ${getErrorMessage(err, "Update failed")}. Open it to finish setup.`)
          void fetchIssues()
          return
        }
      }
      setCreateOpen(false)
      navigate({ name: "issue", spaceId, issueId: created.id })
    } catch (err) {
      setCreateError(getErrorMessage(err, "Failed to create issue"))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Issues</h1>
          <p className="page-activity__subtitle">
            Track space work items, ownership, and current progress.
          </p>
        </div>
        <div className="page-activity__actions">
          <Button
            variant="primary"
            onClick={() => {
              setCreateError(null)
              setPartiallyCreatedId(null)
              setCreateOpen(true)
            }}
          >
            New Issue
          </Button>
        </div>
      </div>

      {(issuesState.kind === "error" ||
        issuesState.kind === "forbidden" ||
        issuesState.kind === "notFound" ||
        issuesState.kind === "stale") && (
        <Alert
          tone={issuesState.kind === "stale" ? "stale" : issuesState.kind}
          message={issuesState.error.message}
          retry={{ label: "Retry", onClick: () => void fetchIssues() }}
        />
      )}
      {canAssignWorkflowState === "denied" ? (
        <p className="page-activity__empty">
          You can create issues and assign people or agents here. Workflow assignment is reserved for space owners and admins.
        </p>
      ) : canAssignWorkflowState === "failed" ? (
        <p className="page-activity__empty">
          Couldn&apos;t verify your role in this space, so workflow assignment stays unavailable. Refresh to try again.
        </p>
      ) : canAssignWorkflowState === "unknown" ? (
        <p className="page-activity__empty">Checking whether you can assign workflows…</p>
      ) : null}

      <section className="issues-page__panel" aria-label="Issue list">
        <div className="issues-page__toolbar">
          {issuesData !== null ? <span className="page-activity__meta">{pageLabel}</span> : null}
        </div>

        {issuesState.kind === "loading" ? (
          <p className="page-activity__empty">Loading…</p>
        ) : issuesState.kind === "readyEmpty" ? (
          <EmptyState message="No issues yet. Create one to track work, ownership, and progress in this space." />
        ) : issuesState.kind === "error" || issuesState.kind === "forbidden" || issuesState.kind === "notFound" ? null : (
          <ul className="issues-page__list">
            {(issuesData ?? []).map((issue) => (
              <li key={issue.id} className="issues-page__list-item">
                <button
                  type="button"
                  className="issues-page__row"
                  onClick={() => {
                    navigate({ name: "issue", spaceId, issueId: issue.id })
                  }}
                >
                  <span className="issues-page__row-main">
                    <span className="issues-page__row-title">{issue.title}</span>
                    <span className="issues-page__row-desc">
                      {issue.description?.trim() || "No description"}
                    </span>
                  </span>
                  <span className="issues-page__row-side">
                    {issue.childCount > 0 ? (
                      <span className="page-activity__meta">
                        {issue.doneChildCount}/{issue.childCount} sub-issues
                      </span>
                    ) : null}
                    {issue.commentCount > 0 ? (
                      <span className="page-activity__meta">
                        {issue.commentCount} comment{issue.commentCount === 1 ? "" : "s"}
                      </span>
                    ) : null}
                    <span className="issues-page__status">{statusLabel(issue.status)}</span>
                    <span className="page-activity__meta">
                      {assigneeLabel(issue)}
                    </span>
                    <span className="page-activity__meta">{issue.updatedLabel}</span>
                  </span>
                </button>
                {issue.childCount > 0 ? (
                  <div className="issues-page__children">
                    {/* Children load on expand rather than with the page: a
                        board of parents would otherwise pay for every
                        breakdown nobody opened. */}
                    <Button
                      variant="tertiary"
                      size="compact"
                      onClick={() => toggleChildren(issue.id)}
                    >
                      {expanded[issue.id] ? "Hide sub-issues" : "Show sub-issues"}
                    </Button>
                    {expanded[issue.id] ? (
                      children[issue.id] === undefined ? (
                        <p className="page-activity__empty">Loading…</p>
                      ) : (
                        <ul className="issues-page__child-list">
                          {children[issue.id].map((child) => (
                            <li key={child.id} className="issues-page__child">
                              <button
                                type="button"
                                className="issues-page__row"
                                onClick={() => navigate({ name: "issue", spaceId, issueId: child.id })}
                              >
                                <span className="issues-page__row-main">
                                  <span className="issues-page__row-title">{child.title}</span>
                                </span>
                                <span className="issues-page__row-side">
                                  <span className="issues-page__status">{statusLabel(child.status)}</span>
                                  <span className="page-activity__meta">{assigneeLabel(child)}</span>
                                </span>
                              </button>
                            </li>
                          ))}
                        </ul>
                      )
                    ) : null}
                  </div>
                ) : null}
              </li>
            ))}
          </ul>
        )}

        {issuesData !== null && totalPages > 1 ? <div className="issues-page__pagination">
          <Button
            variant="secondary"
            disabled={page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            Previous
          </Button>
          <span className="page-activity__meta">
            Page {page} / {totalPages}
          </span>
          <Button
            variant="secondary"
            disabled={page >= totalPages}
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
          >
            Next
          </Button>
        </div> : null}
      </section>

      <IssueModal
        open={createOpen}
        agents={agents}
        workflows={workflows}
        members={members}
        userId={userId}
        loading={saving}
        allowWorkflowAssignment={canAssignWorkflow}
        error={createOpen ? createError : null}
        partiallyCreatedId={partiallyCreatedId}
        onOpenPartial={() => {
          if (!partiallyCreatedId) return
          setCreateOpen(false)
          navigate({ name: "issue", spaceId, issueId: partiallyCreatedId })
        }}
        onClose={() => {
          setCreateOpen(false)
          setCreateError(null)
          setPartiallyCreatedId(null)
        }}
        onSubmit={handleCreate}
      />

    </div>
  )
}
