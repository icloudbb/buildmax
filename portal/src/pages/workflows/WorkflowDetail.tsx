import { useCallback, useEffect, useMemo, useState } from "react"
import type { Agent, Workflow, WorkflowRevision, WorkflowRun } from "../../lib/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { ApiRequestError } from "../../lib/api/client"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"
import {
  apiAgentToAgent,
  apiWorkflowRevisionToWorkflowRevision,
  apiWorkflowRunToWorkflowRun,
  apiWorkflowToWorkflow,
} from "../../lib/api/mappers"
import { getAgents } from "../../features/agents"
import {
  getWorkflow,
  getWorkflowRevisions,
  getWorkflowRuns,
  restoreWorkflowRevision,
  runWorkflow,
  updateWorkflow,
  useWorkflowSteps,
  WorkflowStepsEditor,
  WorkflowRunInputForm,
  parseInputSchema,
  buildInputValue,
  type InputFormValues,
} from "../../features/workflows"
import { RevisionHistory } from "../../components/RevisionHistory"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { isAllowed } from "../../state/permissionState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { useApp } from "../../contexts/AppContext"

interface WorkflowDetailProps {
  token: string | null
  spaceId: string
  workflowId: string
}

export function WorkflowDetail({ token, spaceId, workflowId }: WorkflowDetailProps) {
  const { currentUserRole } = useSpace()
  const { setEntityLabel } = useApp()
  const [agents, setAgents] = useState<Agent[]>([])
  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [runs, setRuns] = useState<WorkflowRun[]>([])
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [status, setStatus] = useState<Workflow["status"]>("draft")
  const {
    steps,
    definition: stepsDefinition,
    errors: stepErrors,
    advanced: stepsAdvanced,
    definitionText,
    definitionParseError,
    addStep,
    removeStep,
    changeStep,
    addBinding,
    removeBinding,
    changeBinding,
    toggleAdvanced,
    setDefinitionText,
    hydrate: hydrateSteps,
  } = useWorkflowSteps(agents)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [running, setRunning] = useState(false)
  const [inputValues, setInputValues] = useState<InputFormValues>({})
  const [error, setError] = useState<string | null>(null)
  const [unavailable, setUnavailable] = useState<ResourceUnavailableKind | null>(null)
  // null means "not yet successfully fetched", distinct from [] meaning the
  // workflow genuinely has no revisions. See deriveResourceState.
  const [revisionsData, setRevisionsData] = useState<WorkflowRevision[] | null>(null)
  const [revisionsLoading, setRevisionsLoading] = useState(false)
  const [revisionsListError, setRevisionsListError] = useState<RequestError | null>(null)
  const [restoringRevision, setRestoringRevision] = useState<number | null>(null)
  // Restore's own error, tagged with which revision it was.
  const [restoreRevisionError, setRestoreRevisionError] = useState<{ revision: number; message: string } | null>(null)
  const canManageWorkflowsState = useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin")
  const canManageWorkflows = isAllowed(canManageWorkflowsState)
  // A run's input form is generated from the workflow's input_schema; a workflow
  // that declares none takes no input and the form is absent.
  const inputFields = useMemo(
    () => (workflow ? (parseInputSchema(workflow.definition)?.fields ?? []) : []),
    [workflow],
  )

  const load = useCallback(async () => {
    if (!token || !spaceId) {
      setAgents([])
      setWorkflow(null)
      setRuns([])
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    setUnavailable(null)
    try {
      const [workflowApi, runsApi, agentsApi, revisionsApi] = await Promise.all([
        getWorkflow(spaceId, workflowId, token),
        getWorkflowRuns(spaceId, workflowId, token),
        getAgents(spaceId, token),
        getWorkflowRevisions(spaceId, workflowId, token),
      ])
      const mappedWorkflow = apiWorkflowToWorkflow(workflowApi)
      setWorkflow(mappedWorkflow)
      setRuns(runsApi.runs.map(apiWorkflowRunToWorkflowRun))
      setAgents(agentsApi.map(apiAgentToAgent))
      setRevisionsData(revisionsApi.revisions.map(apiWorkflowRevisionToWorkflowRevision))
      setName(mappedWorkflow.name)
      setDescription(mappedWorkflow.description)
      setStatus(mappedWorkflow.status)
      hydrateSteps(mappedWorkflow.definition)
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 404) {
        setUnavailable("notFound")
      } else if (err instanceof ApiRequestError && err.status === 403) {
        setUnavailable("forbidden")
      } else {
        setUnavailable("error")
      }
      setWorkflow(null)
      setError(getErrorMessage(err, "Failed to load workflow"))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, workflowId, hydrateSteps])

  const loadRevisions = useCallback(() => {
    if (!token || !spaceId) return
    setRevisionsLoading(true)
    setRevisionsListError(null)
    getWorkflowRevisions(spaceId, workflowId, token)
      .then((res) => setRevisionsData(res.revisions.map(apiWorkflowRevisionToWorkflowRevision)))
      // revisionsData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping history.
      .catch((err) => setRevisionsListError(classifyError(err, "Failed to load history")))
      .finally(() => setRevisionsLoading(false))
  }, [token, spaceId, workflowId])

  const revisionEntries = useMemo(
    () =>
      revisionsData?.map((rev) => ({
        id: rev.id,
        revision: rev.revision,
        createdBy: rev.createdBy,
        createdLabel: rev.createdLabel,
        summary: `${rev.name} · ${rev.status}`,
      })) ?? null,
    [revisionsData]
  )
  const revisionsState = useMemo(
    () =>
      deriveResourceState({
        loading: revisionsLoading,
        data: revisionEntries,
        error: revisionsListError,
        isEmpty: (data) => data.length === 0,
      }),
    [revisionsLoading, revisionEntries, revisionsListError]
  )

  useEffect(() => {
    void load()
  }, [load])

  // Publish the loaded name so the breadcrumb reads "Workflows / <name>"
  // instead of the opaque id, and updates in place after a rename.
  useEffect(() => {
    if (workflow) setEntityLabel(workflow.id, workflow.name)
  }, [workflow, setEntityLabel])

  function handleSave() {
    if (!token || !spaceId || !workflow || !canManageWorkflows) return
    setSaving(true)
    setError(null)
    updateWorkflow(
      spaceId,
      workflow.id,
      { name: name.trim(), description, definition: stepsDefinition, status },
      token,
    )
      .then((updated) => {
        const mapped = apiWorkflowToWorkflow(updated)
        setWorkflow(mapped)
        setName(mapped.name)
        setDescription(mapped.description)
        setStatus(mapped.status)
        hydrateSteps(mapped.definition)
        loadRevisions()
      })
      .catch((err) => setError(getErrorMessage(err, "Failed to update workflow")))
      .finally(() => setSaving(false))
  }

  function handleRestoreRevision(revision: number) {
    if (!token || !spaceId || !workflow || !canManageWorkflows) return
    setRestoreRevisionError(null)
    setRestoringRevision(revision)
    restoreWorkflowRevision(spaceId, workflow.id, revision, token)
      .then((restored) => {
        const mapped = apiWorkflowToWorkflow(restored)
        setWorkflow(mapped)
        setName(mapped.name)
        setDescription(mapped.description)
        setStatus(mapped.status)
        hydrateSteps(mapped.definition)
        loadRevisions()
      })
      .catch((err) => setRestoreRevisionError({ revision, message: getErrorMessage(err, "Failed to restore revision") }))
      .finally(() => setRestoringRevision(null))
  }

  function handleRunWorkflow() {
    if (!token || !spaceId || !workflow) return
    let input: unknown
    if (inputFields.length > 0) {
      const built = buildInputValue(inputFields, inputValues)
      if (built.errors.length > 0) {
        setError(built.errors.join(" "))
        return
      }
      input = built.value
    }
    setRunning(true)
    setError(null)
    runWorkflow(spaceId, workflow.id, token, undefined, input)
      .then((detail) => {
        const mappedRun = apiWorkflowRunToWorkflowRun(detail.run)
        setRuns((prev) => [mappedRun, ...prev.filter((run) => run.id !== mappedRun.id)])
        navigate({ name: "workflowRun", spaceId, workflowRunId: mappedRun.id })
      })
      .catch((err) => setError(getErrorMessage(err, "Failed to run workflow")))
      .finally(() => setRunning(false))
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
        resourceLabel="Workflow"
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
          <h1 className="page-activity__title">Workflow Detail</h1>
          <p className="page-activity__subtitle">
            Edit the workflow definition, run it manually, and inspect recent executions.
          </p>
        </div>
        <div className="page-activity__actions">
          <button
            type="button"
            className="page-activity__action-btn"
            onClick={() => navigate({ name: "workflows", spaceId })}
          >
            Back to Workflows
          </button>
          <button
            type="button"
            className="page-activity__action-btn"
            disabled={loading}
            onClick={() => {
              void load()
            }}
          >
            Refresh
          </button>
          <button
            type="button"
            className="page-activity__action-btn"
            disabled={running || loading || workflow == null || workflow.status !== "published"}
            onClick={handleRunWorkflow}
          >
            {running ? "Running…" : "Run Workflow"}
          </button>
          {canManageWorkflows ? (
            <button
              type="button"
              className="page-activity__action-btn"
              disabled={
                saving ||
                loading ||
                workflow == null ||
                !name.trim() ||
                stepErrors.length > 0 ||
                (stepsAdvanced && definitionParseError != null)
              }
              onClick={handleSave}
            >
              {saving ? "Saving…" : "Save"}
            </button>
          ) : null}
        </div>
      </div>

      {error ? <p className="page-activity__empty">{error}</p> : null}
      {workflow && inputFields.length > 0 ? (
        <WorkflowRunInputForm
          fields={inputFields}
          values={inputValues}
          disabled={running || workflow.status !== "published"}
          onChange={(fieldName, value) => setInputValues((prev) => ({ ...prev, [fieldName]: value }))}
        />
      ) : null}
      {canManageWorkflowsState === "denied" ? (
        <p className="page-activity__empty">
          This workflow is read-only for your role. You can still inspect it here, and you can run it when it is `published`.
        </p>
      ) : canManageWorkflowsState === "failed" ? (
        <p className="page-activity__empty">
          Couldn&apos;t verify your role in this space, so editing stays unavailable. Refresh to try again.
        </p>
      ) : canManageWorkflowsState === "unknown" ? (
        <p className="page-activity__empty">Checking whether you can manage this workflow…</p>
      ) : null}

      {workflow && (
        <div className="workflow-detail-page__grid">
          <section className="issues-page__panel">
            <div className="issues-page__toolbar">
              <h2 className="issues-page__section-title">Definition</h2>
              <span className="page-activity__meta">{workflow.id}</span>
            </div>

            <div className="workflow-page__form">
              <label className="issues-page__field">
                <span className="issues-page__field-label">Status</span>
                <select
                  className="issues-page__input"
                  value={status}
                  disabled={!canManageWorkflows}
                  onChange={(e) => setStatus(e.target.value as Workflow["status"])}
                >
                  <option value="draft">Draft</option>
                  <option value="published">Published</option>
                  <option value="archived">Archived</option>
                </select>
                <span className="issues-page__field-label">
                  Only `published` workflows can be assigned to issues or run manually.
                </span>
              </label>
              <label className="issues-page__field">
                <span className="issues-page__field-label">Name</span>
                <input className="issues-page__input" value={name} disabled={!canManageWorkflows} onChange={(e) => setName(e.target.value)} />
              </label>
              <label className="issues-page__field">
                <span className="issues-page__field-label">Description</span>
                <textarea
                  className="issues-page__textarea"
                  rows={4}
                  value={description}
                  disabled={!canManageWorkflows}
                  onChange={(e) => setDescription(e.target.value)}
                />
              </label>
              {workflow.status !== "published" ? (
                <p className="page-activity__empty">
                  This workflow is currently `{workflow.status}`. Publish it before manual runs or issue assignment.
                </p>
              ) : null}

              <WorkflowStepsEditor
                steps={steps}
                agents={agents}
                disabled={!canManageWorkflows}
                errors={stepErrors}
                advanced={stepsAdvanced}
                definitionText={definitionText}
                definitionParseError={definitionParseError}
                onAddStep={addStep}
                onRemoveStep={removeStep}
                onChangeStep={changeStep}
                onAddBinding={addBinding}
                onRemoveBinding={removeBinding}
                onChangeBinding={changeBinding}
                onToggleAdvanced={toggleAdvanced}
                onDefinitionTextChange={setDefinitionText}
              />
            </div>
          </section>

          <section className="issues-page__panel">
            <RevisionHistory
              title="History"
              state={revisionsState}
              onRetry={loadRevisions}
              currentRevision={workflow?.revision ?? 0}
              canRestore={canManageWorkflows}
              restoringRevision={restoringRevision}
              restoreError={restoreRevisionError}
              onRestore={handleRestoreRevision}
            />
            <p className="page-activity__meta">
              Restoring writes that version's name, description, and steps back as a new
              version. The lifecycle state is left as it is.
            </p>
          </section>

          <section className="issues-page__panel">
            <div className="issues-page__toolbar">
              <h2 className="issues-page__section-title">Recent Runs</h2>
              <span className="page-activity__meta">{runs.length} total</span>
            </div>
            {runs.length === 0 ? (
              <p className="page-activity__empty">No runs yet.</p>
            ) : (
              <ul className="workflow-page__runs">
                {runs.map((run) => (
                  <li key={run.id}>
                    <button
                      type="button"
                      className="workflow-page__run-row"
                      onClick={() => navigate({ name: "workflowRun", spaceId, workflowRunId: run.id })}
                    >
                      <span>
                        <strong>{run.status}</strong>
                        <span className="page-activity__meta workflow-detail-page__run-id">{run.id}</span>
                      </span>
                      <span className="page-activity__meta">{run.createdLabel}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      )}
    </div>
  )
}
