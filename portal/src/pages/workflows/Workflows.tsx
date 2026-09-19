import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { Agent, Workflow } from "../../lib/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { statusLabel } from "../../lib/statusLabels"
import {
  apiAgentToAgent,
  apiWorkflowToWorkflow,
} from "../../lib/api/mappers"
import { getAgents } from "../../features/agents"
import {
  createWorkflow,
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
  const { currentUserRole } = useSpace()
  const [agents, setAgents] = useState<Agent[]>([])
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
      .catch((err) => setListError(classifyError(err, "Failed to load workflows")))
      .finally(() => setLoading(false))
  }, [token, spaceId])

  useEffect(() => {
    void fetchWorkflows()
  }, [fetchWorkflows])

  const workflowsState = useMemo(
    () => deriveResourceState({ loading, data: workflowsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, workflowsData, listError]
  )

  const workflowCountLabel = useMemo(() => {
    const count = workflowsData?.length ?? 0
    if (count === 0) return "0 workflows"
    if (count === 1) return "1 workflow"
    return `${count} workflows`
  }, [workflowsData])

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
      .catch((err) => setCreateError(getErrorMessage(err, "Failed to create workflow")))
      .finally(() => setSaving(false))
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Workflows</h1>
          <p className="page-activity__subtitle">
            Define reusable step-by-step execution plans and run them manually.
          </p>
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
              New Workflow
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
          retry={{ label: "Retry", onClick: () => void fetchWorkflows() }}
        />
      )}
      {canManageWorkflowsState === "denied" ? (
        <p className="page-activity__empty">
          You can view workflows here, but only space owners and admins can create or edit them.
        </p>
      ) : canManageWorkflowsState === "failed" ? (
        <p className="page-activity__empty">
          Couldn&apos;t verify your role in this space, so editing stays unavailable. Refresh to try again.
        </p>
      ) : canManageWorkflowsState === "unknown" ? (
        <p className="page-activity__empty">Checking whether you can manage workflows…</p>
      ) : null}

      <section className="issues-page__panel" aria-label="Workflow list">
        <div className="issues-page__toolbar">
          {workflowsData !== null ? <span className="page-activity__meta">{workflowCountLabel}</span> : null}
        </div>

        {workflowsState.kind === "loading" ? (
          <p className="page-activity__empty">Loading…</p>
        ) : workflowsState.kind === "readyEmpty" ? (
          <EmptyState
            message={
              canManageWorkflows
                ? "No workflows yet. Create one to define a reusable execution plan for this space."
                : "No workflows are available in this space yet. Space owners and admins can publish one when a shared process is ready."
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
                      {workflow.description?.trim() || "No description"}
                    </span>
                  </span>
                  <span className="issues-page__row-side">
                    <span className="issues-page__status">{statusLabel(workflow.status)}</span>
                    <span className="page-activity__meta">{workflow.updatedLabel}</span>
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
