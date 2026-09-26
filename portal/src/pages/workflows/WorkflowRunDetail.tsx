import { useCallback, useEffect, useState } from "react"
import { Button, ButtonLink } from "@buildmax/gui"
import type { Workflow, WorkflowRun, WorkflowNodeRun } from "../../lib/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { statusLabel } from "../../lib/statusLabels"
import {
  apiWorkflowRunToWorkflowRun,
  apiWorkflowNodeRunToWorkflowNodeRun,
  apiWorkflowToWorkflow,
} from "../../lib/api/mappers"
import { getWorkflow, getWorkflowRunDetail, WorkflowGraph } from "../../features/workflows"
import { buildHash, navigate } from "../../router"
import { useApp } from "../../contexts/AppContext"
import { ApiRequestError } from "../../lib/api/client"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"

interface WorkflowRunDetailProps {
  token: string | null
  spaceId: string
  workflowRunId: string
}

export function WorkflowRunDetail({ token, spaceId, workflowRunId }: WorkflowRunDetailProps) {
  const { setEntityLabel } = useApp()
  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [run, setRun] = useState<WorkflowRun | null>(null)
  const [steps, setSteps] = useState<WorkflowNodeRun[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [unavailable, setUnavailable] = useState<ResourceUnavailableKind | null>(null)
  const [lastRefreshedAt, setLastRefreshedAt] = useState<number | null>(null)

  const load = useCallback(async (background = false) => {
    if (!token || !spaceId) {
      setWorkflow(null)
      setRun(null)
      setSteps([])
      setLoading(false)
      setRefreshing(false)
      return
    }
    if (background) {
      setRefreshing(true)
    } else {
      setLoading(true)
      setError(null)
      setUnavailable(null)
    }
    try {
      const detail = await getWorkflowRunDetail(spaceId, workflowRunId, token)
      const mappedRun = apiWorkflowRunToWorkflowRun(detail.run)
      setRun(mappedRun)
      setSteps(detail.steps.map(apiWorkflowNodeRunToWorkflowNodeRun))
      const workflowApi = await getWorkflow(spaceId, detail.run.workflow_id, token)
      setWorkflow(apiWorkflowToWorkflow(workflowApi))
      setLastRefreshedAt(Date.now())
    } catch (err) {
      // A background poll's failure is transient by nature (the run was
      // readable a moment ago) -- it must not bounce a reader watching a live
      // run to a not-found page. Only the initial load classifies the error.
      if (!background) {
        if (err instanceof ApiRequestError && err.status === 404) {
          setUnavailable("notFound")
        } else if (err instanceof ApiRequestError && err.status === 403) {
          setUnavailable("forbidden")
        } else {
          setUnavailable("error")
        }
        setRun(null)
        setError(getErrorMessage(err, "Failed to load workflow run"))
      }
    } finally {
      if (background) {
        setRefreshing(false)
      } else {
        setLoading(false)
      }
    }
  }, [token, spaceId, workflowRunId])

  useEffect(() => {
    void load()
  }, [load])

  // Publish the run's workflow name so the breadcrumb reads
  // "Workflows / <name>" instead of the opaque run id.
  useEffect(() => {
    if (run && workflow) setEntityLabel(run.id, workflow.name)
  }, [run, workflow, setEntityLabel])

  useEffect(() => {
    if (run == null) return
    if (!["pending", "running", "failing", "canceling"].includes(run.status)) return
    const timer = window.setInterval(() => {
      void load(true)
    }, 3000)
    return () => window.clearInterval(timer)
  }, [run, load])

  const isLive = run != null && ["pending", "running", "failing", "canceling"].includes(run.status)
  const refreshedLabel = lastRefreshedAt
    ? new Date(lastRefreshedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })
    : null

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
        resourceLabel="Workflow Run"
        kind={unavailable}
        errorMessage={error}
        onRetry={() => void load()}
        backLabel="Back to Workflows"
        onBack={() => navigate({ name: "workflows", spaceId })}
      />
    )
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{workflow?.name ?? "Workflow run"}</h1>
          <p className="page-activity__subtitle">
            {run ? `${statusLabel(run.status)} · ${run.createdLabel}` : "Workflow run"}
          </p>
        </div>
        <div className="page-activity__actions">
          <Button
            variant="tertiary"
            busy={refreshing}
            disabled={loading || refreshing}
            onClick={() => {
              void load(true)
            }}
          >
            Refresh
          </Button>
          {workflow ? (
            <ButtonLink variant="tertiary" href={buildHash({ name: "workflow", spaceId, workflowId: workflow.id })}>
              Back to Workflow
            </ButtonLink>
          ) : null}
        </div>
      </div>

      {run && (
        <div className="workflow-run-page__grid">
          <section className="issues-page__panel">
            <div className="issues-page__toolbar">
              <h2 className="issues-page__section-title">Result</h2>
              <span className="issues-page__status">{statusLabel(run.status)}</span>
            </div>
            {run.result != null ? (
              <div className="workflow-run-page__result">
                <pre className="workflow-page__step-output">
                  {typeof run.result === "string" ? run.result : JSON.stringify(run.result, null, 2)}
                </pre>
              </div>
            ) : <p className="page-activity__meta">{isLive ? "The run is in progress. Its result will appear here." : "No result was produced."}</p>}
            {run.errorMessage ? <p className="modal__error" role="alert">{run.errorMessage}</p> : null}
            <details className="workflow-run-page__diagnostics">
              <summary>Run details</summary>
            <div className="workflow-run-page__meta">
              <div><strong>Run ID:</strong> {run.id}</div>
              {run.workflowRevision ? (
                <div><strong>Workflow version:</strong> v{run.workflowRevision}</div>
              ) : null}
              <div><strong>Created:</strong> {run.createdLabel}</div>
              {run.startedAt ? <div><strong>Started:</strong> {new Date(run.startedAt).toLocaleString()}</div> : null}
              {run.endedAt ? <div><strong>Ended:</strong> {new Date(run.endedAt).toLocaleString()}</div> : null}
              {run.issueId ? <div><strong>Issue ID:</strong> {run.issueId}</div> : null}
              <div>
                <strong>Mode:</strong> {isLive ? "Live updates enabled" : "Final snapshot"}
              </div>
              {refreshedLabel ? <div><strong>Last refreshed:</strong> {refreshedLabel}</div> : null}
            </div>
            </details>
          </section>

          {steps.length > 0 ? (
            <section className="issues-page__panel">
              <div className="issues-page__toolbar">
                <h2 className="issues-page__section-title">Graph</h2>
                <span className="page-activity__meta">execution order by dependency</span>
              </div>
              <WorkflowGraph
                nodes={steps.map((step) => ({
                  id: step.nodeId,
                  status: step.status,
                  needs: step.needs,
                  sublabel: step.agentName ?? step.targetAgentId ?? undefined,
                  onOpen: step.taskId ? () => navigate({ name: "task", spaceId, taskId: step.taskId! }) : undefined,
                }))}
              />
            </section>
          ) : null}

          <section className="issues-page__panel">
            <div className="issues-page__toolbar">
              <h2 className="issues-page__section-title">Steps</h2>
              <span className="page-activity__meta">{steps.length} total</span>
            </div>
            {steps.length === 0 ? (
              <p className="page-activity__empty">No steps recorded.</p>
            ) : (
              <ol className="workflow-page__steps">
                {steps.map((step) => (
                  <li key={step.id} className="workflow-page__step">
                    <div className="workflow-page__step-head">
                      <strong>{step.nodeId}</strong>
                      <span className="issues-page__status">{statusLabel(step.status)}</span>
                    </div>
                    <div className="workflow-page__step-body">
                      <div className="page-activity__meta">{statusLabel(step.nodeType)}</div>
                      <div>{step.prompt}</div>
                      {step.targetAgentId ? (
                        <div className="page-activity__meta">
                          Agent: {step.agentName ? `${step.agentName} (${step.targetAgentId})` : step.targetAgentId}
                          {step.agentRevision ? ` · v${step.agentRevision}` : ""}
                        </div>
                      ) : null}
                      {step.agentInstructions ? (
                        <details className="workflow-run-page__step-agent">
                          <summary className="page-activity__meta">Agent definition used by this step</summary>
                          <pre className="workflow-page__step-output">{step.agentInstructions}</pre>
                        </details>
                      ) : null}
                      {step.taskId ? (
                        <div className="page-activity__meta">
                          Task: {step.taskId}
                          {step.taskRunId ? ` / Run: ${step.taskRunId}` : ""}
                        </div>
                      ) : null}
					  {step.taskId ? (
                        <div className="workflow-run-page__step-actions">
                          <Button
                            variant="tertiary" size="compact"
							onClick={() => navigate({ name: "task", spaceId, taskId: step.taskId! })}
                          >
							Open Task
                          </Button>
                        </div>
                      ) : null}
                      {step.resolvedInput ? (
                        <details className="workflow-run-page__step-agent">
                          <summary className="page-activity__meta">Resolved input this node received</summary>
                          <pre className="workflow-page__step-output">{step.resolvedInput}</pre>
                        </details>
                      ) : null}
                      {step.output ? <pre className="workflow-page__step-output">{step.output}</pre> : null}
                      {step.errorMessage ? <p className="modal__error">{step.errorMessage}</p> : null}
                    </div>
                  </li>
                ))}
              </ol>
            )}
          </section>
        </div>
      )}
    </div>
  )
}
