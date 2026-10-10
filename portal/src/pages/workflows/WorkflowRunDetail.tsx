import { useCallback, useEffect, useState } from "react"
import { Button, ButtonLink, useLocale, type Translate } from "@buildmax/gui"
import type { Workflow, WorkflowRun, WorkflowNodeRun, WorkflowRequest } from "../../lib/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStatusLabel } from "../../lib/statusLabels"
import { useStableT, useT, type MessageKey } from "../../i18n"
import { formatRelativeTime, formatTimestamp, intlLocale } from "../../lib/dateFormat"
import {
  apiWorkflowRunToWorkflowRun,
  apiWorkflowNodeRunToWorkflowNodeRun,
  apiWorkflowRequestToWorkflowRequest,
  apiWorkflowToWorkflow,
} from "../../lib/api/mappers"
import {
  cancelWorkflowRun,
  getWorkflow,
  getWorkflowRunDetail,
  respondToWorkflowRequest,
  WorkflowGraph,
  WorkflowRequestCard,
  describeResponse,
  type RequestResponse,
} from "../../features/workflows"
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
  const t = useT()
  const { locale } = useLocale()
  const stableT = useStableT()
  const statusLabel = useStatusLabel()
  const { setEntityLabel } = useApp()
  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [run, setRun] = useState<WorkflowRun | null>(null)
  const [steps, setSteps] = useState<WorkflowNodeRun[]>([])
  const [requests, setRequests] = useState<WorkflowRequest[]>([])
  const [canceling, setCanceling] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
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
      setRequests((detail.requests ?? []).map(apiWorkflowRequestToWorkflowRequest))
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
        setError(getErrorMessage(err, stableT("workflows.run.error.load")))
      }
    } finally {
      if (background) {
        setRefreshing(false)
      } else {
        setLoading(false)
      }
    }
  }, [token, spaceId, workflowRunId, stableT])

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
  const pendingRequests = requests.filter((request) => request.status === "pending")

  async function respond(request: WorkflowRequest, response: RequestResponse) {
    if (!token) return
    await respondToWorkflowRequest(spaceId, request.id, response, token)
    await load(true)
  }

  async function cancelRun() {
    if (!token || !run) return
    setCanceling(true)
    setActionError(null)
    try {
      await cancelWorkflowRun(spaceId, run.id, token)
      await load(true)
    } catch (err) {
      setActionError(getErrorMessage(err, stableT("workflows.run.error.cancel")))
    } finally {
      setCanceling(false)
    }
  }
  // Node types the runtime knows read as words; an unknown one falls back to
  // the generic status formatting.
  const nodeTypeLabel = (nodeType: string) =>
    nodeType === "agent_task" || nodeType === "human_input"
      ? t(`workflows.nodeType.${nodeType}`)
      : statusLabel(nodeType)
  const refreshedLabel = lastRefreshedAt
    ? new Date(lastRefreshedAt).toLocaleTimeString(intlLocale(locale), {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      })
    : null

  if (loading) {
    return (
      <div className="page-activity">
        <p className="page-activity__empty">{t("shell.loading")}</p>
      </div>
    )
  }

  if (unavailable) {
    return (
      <ResourceUnavailable
        resourceLabel={t("workflows.run.resource")}
        kind={unavailable}
        errorMessage={error}
        onRetry={() => void load()}
        backLabel={t("workflows.backToList")}
        onBack={() => navigate({ name: "workflows", spaceId })}
      />
    )
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{workflow?.name ?? t("workflows.run.fallbackTitle")}</h1>
          <p className="page-activity__subtitle">
            {run ? `${statusLabel(run.status)} · ${formatRelativeTime(run.createdAt, locale)}` : t("workflows.run.fallbackTitle")}
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
            {t("workflows.refresh")}
          </Button>
          {run && ["pending", "running"].includes(run.status) ? (
            <Button variant="danger" busy={canceling} disabled={canceling} onClick={() => void cancelRun()}>
              {t("workflows.run.cancel")}
            </Button>
          ) : null}
          {workflow ? (
            <ButtonLink variant="tertiary" href={buildHash({ name: "workflow", spaceId, workflowId: workflow.id })}>
              {t("workflows.run.back")}
            </ButtonLink>
          ) : null}
        </div>
      </div>

      {actionError ? <p className="modal__error" role="alert">{actionError}</p> : null}
      {pendingRequests.length > 0 ? (
        <div className="workflow-run-page__requests">
          {pendingRequests.map((request, i) => (
            <WorkflowRequestCard key={request.id} request={request} keys={i === 0} onRespond={(response) => respond(request, response)} />
          ))}
        </div>
      ) : null}

      {run && (
        <div className="workflow-run-page__grid">
          <section className="issues-page__panel">
            <div className="issues-page__toolbar">
              <h2 className="issues-page__section-title">{t("workflows.run.result")}</h2>
              <span className="issues-page__status">{statusLabel(run.status)}</span>
            </div>
            {run.result != null ? (
              <div className="workflow-run-page__result">
                <pre className="workflow-page__step-output">
                  {typeof run.result === "string" ? run.result : JSON.stringify(run.result, null, 2)}
                </pre>
              </div>
            ) : (
              // No declared result is not "nothing produced": the steps'
              // outputs are what the run produced, so the page says where they are.
              <p className="page-activity__meta">
                {isLive
                  ? t("workflows.run.inProgress")
                  : steps.some((step) => step.output)
                    ? t("workflows.run.noDeclaredResult")
                    : t("workflows.run.noOutput")}
              </p>
            )}
            {run.errorMessage ? <p className="modal__error" role="alert">{run.errorMessage}</p> : null}
            <details className="workflow-run-page__diagnostics">
              <summary>{t("workflows.run.details")}</summary>
            <div className="workflow-run-page__meta">
              <div><strong>{t("workflows.run.id")}</strong> {run.id}</div>
              {run.workflowRevision ? (
                <div><strong>{t("workflows.run.version")}</strong> v{run.workflowRevision}</div>
              ) : null}
              <div><strong>{t("workflows.run.created")}</strong> {formatRelativeTime(run.createdAt, locale)}</div>
              {run.startedAt ? <div><strong>{t("workflows.run.started")}</strong> {formatTimestamp(run.startedAt, locale)}</div> : null}
              {run.endedAt ? <div><strong>{t("workflows.run.ended")}</strong> {formatTimestamp(run.endedAt, locale)}</div> : null}
              {run.deadlineAt ? <div><strong>{t("workflows.run.deadline")}</strong> {formatTimestamp(run.deadlineAt, locale)}</div> : null}
              {run.issueId ? <div><strong>{t("workflows.run.issueId")}</strong> {run.issueId}</div> : null}
              <div>
                <strong>{t("workflows.run.mode")}</strong> {isLive ? t("workflows.run.live") : t("workflows.run.final")}
              </div>
              {refreshedLabel ? <div><strong>{t("workflows.run.lastRefreshed")}</strong> {refreshedLabel}</div> : null}
            </div>
            </details>
          </section>

          {steps.length > 0 ? (
            <section className="issues-page__panel">
              <div className="issues-page__toolbar">
                <h2 className="issues-page__section-title">{t("workflows.run.graph")}</h2>
                <span className="page-activity__meta">{t("workflows.run.graphHint")}</span>
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
              <h2 className="issues-page__section-title">{t("workflows.run.steps")}</h2>
              <span className="page-activity__meta">{t("workflows.total", { count: steps.length })}</span>
            </div>
            {steps.length === 0 ? (
              <p className="page-activity__empty">{t("workflows.run.noSteps")}</p>
            ) : (
              <ol className="workflow-page__steps">
                {steps.map((step) => (
                  <li key={step.id} className="workflow-page__step">
                    <div className="workflow-page__step-head">
                      <strong>{step.nodeId}</strong>
                      <span className="issues-page__status">{statusLabel(step.status)}</span>
                    </div>
                    <div className="workflow-page__step-body">
                      <div className="page-activity__meta">{nodeTypeLabel(step.nodeType)}</div>
                      <div>{step.prompt}</div>
                      <StepAttempt step={step} />
                      {requests
                        .filter((request) => request.nodeRunId === step.id && request.status !== "pending")
                        .map((request) => (
                          <div key={request.id} className="page-activity__meta">
                            {resolvedRequestLabel(request, t)}
                          </div>
                        ))}
                      {step.targetAgentId ? (
                        <div className="page-activity__meta">
                          {t("workflows.run.agentLine", {
                            agent: step.agentName ? `${step.agentName} (${step.targetAgentId})` : step.targetAgentId,
                          })}
                          {step.agentRevision ? ` · v${step.agentRevision}` : ""}
                        </div>
                      ) : null}
                      {step.agentInstructions ? (
                        <details className="workflow-run-page__step-agent">
                          <summary className="page-activity__meta">{t("workflows.run.agentDefinition")}</summary>
                          <pre className="workflow-page__step-output">{step.agentInstructions}</pre>
                        </details>
                      ) : null}
                      {step.taskId ? (
                        <div className="page-activity__meta">
                          {t("workflows.run.taskLine", { task: step.taskId })}
                          {step.taskRunId ? t("workflows.run.taskRunSuffix", { run: step.taskRunId }) : ""}
                        </div>
                      ) : null}
					  {step.taskId ? (
                        <div className="workflow-run-page__step-actions">
                          <Button
                            variant="tertiary" size="compact"
							onClick={() => navigate({ name: "task", spaceId, taskId: step.taskId! })}
                          >
							{t("workflows.run.openTask")}
                          </Button>
                        </div>
                      ) : null}
                      {/* Output first and labelled, so it never reads as the
                          collapsed input that follows it. */}
                      {step.output ? (
                        <figure className="workflow-run-page__step-output">
                          <figcaption className="page-activity__meta">{t("workflows.run.output")}</figcaption>
                          <pre className="workflow-page__step-output">{step.output}</pre>
                        </figure>
                      ) : null}
                      {step.resolvedInput ? (
                        <details className="workflow-run-page__step-agent">
                          <summary className="page-activity__meta">{t("workflows.run.resolvedInput")}</summary>
                          <pre className="workflow-page__step-output">{step.resolvedInput}</pre>
                        </details>
                      ) : null}
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

/** A step's retry and timeout progress: which attempt it is on, when a waiting
 *  step retries, and when a running attempt times out. Silent for a one-attempt
 *  step with no timeout, which is most of them. */
function StepAttempt({ step }: { step: WorkflowNodeRun }) {
  const t = useT()
  const { locale } = useLocale()
  const parts: string[] = []
  if (step.maxAttempts > 1 && step.attempt > 0) {
    parts.push(t("workflows.run.attempt", { attempt: step.attempt, max: step.maxAttempts }))
  }
  if (step.status === "retry_wait" && step.nextAttemptAt) {
    parts.push(t("workflows.run.nextAttempt", { time: new Date(step.nextAttemptAt).toLocaleTimeString(intlLocale(locale)) }))
  }
  if (step.status === "waiting") parts.push(t("workflows.run.waitingForPerson"))
  if (step.status === "running" && step.deadlineAt) {
    parts.push(t("workflows.run.timesOut", { time: formatTimestamp(step.deadlineAt, locale) }))
  }
  if (parts.length === 0) return null
  return <div className="page-activity__meta">{parts.join(" · ")}</div>
}

/** One line recording how a request a step waited on was resolved. */
function resolvedRequestLabel(request: WorkflowRequest, t: Translate<MessageKey>): string {
  const what = t(request.kind === "question" ? "workflows.request.question" : "workflows.request.input")
  const answer = describeResponse(request.response, t)
  switch (request.status) {
    case "answered":
      return answer ? t("workflows.request.answeredWith", { what, answer }) : t("workflows.request.answered", { what })
    case "declined":
      return answer ? t("workflows.request.declinedWith", { what, answer }) : t("workflows.request.declined", { what })
    case "expired":
      return t("workflows.request.expired", { what })
    case "canceled":
      return t("workflows.request.canceled", { what })
    default:
      return t("workflows.request.otherStatus", { what, status: request.status })
  }
}
