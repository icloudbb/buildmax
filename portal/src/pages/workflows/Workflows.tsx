import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, CollectionFrame, PageHeader, StatusLabel } from "@buildmax/gui"
import type { Workflow, WorkflowRequest } from "../../lib/types"
import { navigate } from "../../router"
import { useStableT, useT } from "../../i18n"
import { useRelativeTime } from "../../lib/dateFormat"
import { apiWorkflowRequestToWorkflowRequest, apiWorkflowToWorkflow } from "../../lib/api/mappers"
import { getPendingWorkflowRequests, getWorkflows } from "../../features/workflows"
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
  const { currentUserRole } = useSpace()
  const [pendingRequests, setPendingRequests] = useState<WorkflowRequest[]>([])
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no workflows. See deriveResourceState.
  const [workflowsData, setWorkflowsData] = useState<Workflow[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const canManageWorkflowsState = useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin")
  const canManageWorkflows = isAllowed(canManageWorkflowsState)

  const fetchWorkflows = useCallback(() => {
    if (!token || !spaceId) {
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
    return getWorkflows(spaceId, token)
      .then((workflowRes) => setWorkflowsData(workflowRes.workflows.map(apiWorkflowToWorkflow)))
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

  return (
    <div className="page-activity">
      <PageHeader
        title={t("workflows.title")}
        subtitle={t("workflows.subtitle")}
        actions={
          canManageWorkflows ? (
            // A Workflow is authored on a page of its own, sized for its graph.
            <Button variant="primary" onClick={() => navigate({ name: "workflowNew", spaceId })}>
              {t("workflows.new")}
            </Button>
          ) : null
        }
      />

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
        <CollectionFrame
          label={t("workflows.pending.title")}
          loading={false}
          toolbar={
            <>
              <h2 className="issues-page__section-title">{t("workflows.pending.title")}</h2>
              <span className="page-activity__meta">
                {t("workflows.pending.count", { count: pendingRequests.length })}
              </span>
            </>
          }
        >
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
        </CollectionFrame>
      ) : null}

      <CollectionFrame
        label={t("workflows.list.label")}
        loading={workflowsState.kind === "loading"}
        toolbar={workflowsData !== null ? <span className="page-activity__meta">{workflowCountLabel}</span> : null}
        empty={
          workflowsState.kind === "readyEmpty" ? (
            <EmptyState message={canManageWorkflows ? t("workflows.list.empty") : t("workflows.list.emptyViewer")} />
          ) : null
        }
      >
        {workflowsState.kind === "error" || workflowsState.kind === "forbidden" || workflowsState.kind === "notFound" ? null : (
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
                    <StatusLabel status={workflow.status} />
                    <span className="page-activity__meta">{relativeTime(workflow.updatedAt)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </CollectionFrame>
    </div>
  )
}
