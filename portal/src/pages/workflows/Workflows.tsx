import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { Agent, Workflow, WorkflowRequest } from "../../lib/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStatusLabel } from "../../lib/statusLabels"
import { useStableT, useT } from "../../i18n"
import { useRelativeTime } from "../../lib/dateFormat"
import {
  apiAgentToAgent,
  apiWorkflowRequestToWorkflowRequest,
  apiWorkflowToWorkflow,
} from "../../lib/api/mappers"
import { getAgents } from "../../features/agents"
import {
  createWorkflow,
  getPendingWorkflowRequests,
  getWorkflows,
} from "../../features/workflows"
import { WorkflowModal } from "../../components/WorkflowModal"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed } from "../../state/permissionState"

interface WorkflowsProps {
  token: string | null
  spaceId: string
}

export function Workflows({ token, spaceId }: WorkflowsProps) {
  const t = useT()
  const relativeTime = useRelativeTime()
  const stableT = useStableT()
  const statusLabel = useStatusLabel()
  const { currentUserRole } = useSpace()
  const [agents, setAgents] = useState<Agent[]>([])
  const [pendingRequests, setPendingRequests] = useState<WorkflowRequest[]>([])
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no workflows. See deriveResourceState.
  const [workflowsData, setWorkflowsData] = useState<Workflow[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [saving, setSaving] = useState(false)
  // Distinct from listError: the create-workflow mutation's own error.
  const [createError, setCreateError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const canManageWorkflowsState = useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin")
  const canManageWorkflows = isAllowed(canManageWorkflowsState)

  const fetchWorkflows = useCallback(() => {
    if (!token || !spaceId) {
      setAgents([])
      setWorkflowsData(null)
      setLoading(false)
      setListError(null)
      return Promise.resolve()
    }
    setLoading(true)
    setListError(null)
    // Pending requests are a side panel: failing to read them must not fail
    // the workflow list.
    void getPendingWorkflowRequests(spaceId, token)
      .then((res) => setPendingRequests(res.requests.map(apiWorkflowRequestToWorkflowRequest)))
      .catch(() => setPendingRequests([]))
    return Promise.all([
      getWorkflows(spaceId, token),
      getAgents(spaceId, token),
    ])
      .then(([workflowRes, agentRes]) => {
        setWorkflowsData(workflowRes.workflows.map(apiWorkflowToWorkflow))
        setAgents(agentRes.map(apiAgentToAgent))
      })
      // workflowsData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping the list.
      .catch((err) => setListError(classifyError(err, stableT("workflows.error.load"))))
      .finally(() => setLoading(false))
  }, [token, spaceId, stableT])

  useEffect(() => {
    void fetchWorkflows()
  }, [fetchWorkflows])

  const workflowsState = useMemo(
    () => deriveResourceState({ loading, data: workflowsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, workflowsData, listError]
  )

  const workflowCountLabel = t("workflows.list.count", { count: workflowsData?.length ?? 0 })

  function handleCreate(values: { name: string; description: string; definition: string }) {
    if (!token || !spaceId) return
    setSaving(true)
    setCreateError(null)
    createWorkflow(spaceId, values, token)
      .then((created) => {
        setCreateOpen(false)
        // Success lands on the created workflow, not back on the list.
        navigate({ name: "workflow", spaceId, workflowId: created.id })
      })
      .catch((err) => setCreateError(getErrorMessage(err, stableT("workflows.error.create"))))
      .finally(() => setSaving(false))
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{t("workflows.title")}</h1>
          <p className="page-activity__subtitle">{t("workflows.subtitle")}</p>
        </div>
        <div className="page-activity__actions">
          {canManageWorkflows ? (
            <Button
              variant="primary"
              onClick={() => {
                setCreateError(null)
                setCreateOpen(true)
              }}
            >
              {t("workflows.new")}
            </Button>
          ) : null}
        </div>
      </div>

      {(workflowsState.kind === "error" ||
        workflowsState.kind === "forbidden" ||
        workflowsState.kind === "notFound" ||
        workflowsState.kind === "stale") && (
        <Alert
          tone={workflowsState.kind === "stale" ? "stale" : workflowsState.kind}
          message={workflowsState.error.message}
          retry={{ label: t("shell.retry"), onClick: () => void fetchWorkflows() }}
        />
      )}
      {canManageWorkflowsState === "denied" ? (
        <p className="page-activity__empty">{t("workflows.access.denied")}</p>
      ) : canManageWorkflowsState === "failed" ? (
        <p className="page-activity__empty">{t("workflows.access.unverified")}</p>
      ) : canManageWorkflowsState === "unknown" ? (
        <p className="page-activity__empty">{t("workflows.access.checking")}</p>
      ) : null}

      {pendingRequests.length > 0 ? (
        <section className="issues-page__panel" aria-label={t("workflows.pending.title")}>
          <div className="issues-page__toolbar">
            <h2 className="issues-page__section-title">{t("workflows.pending.title")}</h2>
            <span className="page-activity__meta">
              {t("workflows.pending.count", { count: pendingRequests.length })}
            </span>
          </div>
          <ul className="workflow-page__pending">
            {pendingRequests.map((request) => (
              <li key={request.id}>
                <button
                  type="button"
                  className="workflow-page__pending-link"
                  onClick={() => navigate({ name: "workflowRun", spaceId, workflowRunId: request.workflowRunId })}
                >
                  <strong>{request.nodeId}</strong>{" "}
                  <span className="page-activity__meta">
                    {request.kind === "question" ? t("workflows.pending.question") : t("workflows.pending.input")}
                    {request.prompt ? ` · ${request.prompt.split("\n")[0]}` : ""}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="issues-page__panel" aria-label={t("workflows.list.label")}>
        <div className="issues-page__toolbar">
          {workflowsData !== null ? <span className="page-activity__meta">{workflowCountLabel}</span> : null}
        </div>

        {workflowsState.kind === "loading" ? (
          <p className="page-activity__empty">{t("shell.loading")}</p>
        ) : workflowsState.kind === "readyEmpty" ? (
          <EmptyState
            message={
              canManageWorkflows
                ? t("workflows.list.empty")
                : t("workflows.list.emptyViewer")
            }
          />
        ) : workflowsState.kind === "error" || workflowsState.kind === "forbidden" || workflowsState.kind === "notFound" ? null : (
          <ul className="issues-page__list">
            {(workflowsData ?? []).map((workflow) => (
              <li key={workflow.id} className="issues-page__list-item">
                <button
                  type="button"
                  className="issues-page__row"
                  onClick={() => navigate({ name: "workflow", spaceId, workflowId: workflow.id })}
                >
                  <span className="issues-page__row-main">
                    <span className="issues-page__row-title">{workflow.name}</span>
                    <span className="issues-page__row-desc">
                      {workflow.description?.trim() || t("workflows.list.noDescription")}
                    </span>
                  </span>
                  <span className="issues-page__row-side">
                    <span className="issues-page__status">{statusLabel(workflow.status)}</span>
                    <span className="page-activity__meta">{relativeTime(workflow.updatedAt)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <WorkflowModal
        open={createOpen}
        agents={agents}
        loading={saving}
        error={createOpen ? createError : null}
        onClose={() => {
          setCreateOpen(false)
          setCreateError(null)
        }}
        onSubmit={handleCreate}
      />
    </div>
  )
}
