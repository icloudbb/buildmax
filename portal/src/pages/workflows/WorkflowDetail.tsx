import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { BaseModal, Button, ButtonLink } from "@buildmax/gui"
import type { Agent, Workflow, WorkflowRevision, WorkflowRun } from "../../lib/types"
import { buildHash, navigate } from "../../router"
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
  WorkflowGraph,
  WorkflowRunInputForm,
  effectiveNeeds,
  parseInputSchema,
  buildInputValue,
  type GraphNode,
  type InputFormValues,
} from "../../features/workflows"
import { RevisionHistory } from "../../components/RevisionHistory"
import { DetailTabs } from "../../components/DetailTabs"
import { SchedulesSection } from "../../features/schedules/SchedulesSection"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { isAllowed } from "../../state/permissionState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { useApp } from "../../contexts/AppContext"
import { statusLabel } from "../../lib/statusLabels"

interface WorkflowDetailProps {
  token: string | null
  spaceId: string
  workflowId: string
}

type Tab = "overview" | "definition" | "runs" | "schedules" | "revisions"

export function WorkflowDetail({ token, spaceId, workflowId }: WorkflowDetailProps) {
  const { currentUserRole, currentSpaceMembers } = useSpace()
  const { setEntityLabel } = useApp()
  const [agents, setAgents] = useState<Agent[]>([])
  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [runs, setRuns] = useState<WorkflowRun[]>([])
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const stepsState = useWorkflowSteps(agents)
  const {
    steps,
    definition: stepsDefinition,
    errors: stepErrors,
    advanced: stepsAdvanced,
    definitionParseError,
    hydrate: hydrateSteps,
  } = stepsState
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [running, setRunning] = useState(false)
  const [tab, setTab] = useState<Tab>("overview")
  // A draft/archived workflow opens on its Definition tab, since it exists to be
  // built; a published one opens on Overview. Applied once per loaded workflow so
  // a manual tab switch is not undone by a later refresh.
  const didInitTab = useRef(false)
  const [runModalOpen, setRunModalOpen] = useState(false)
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
  // Managing schedules is member-tier (manage_schedules), unlike editing the
  // workflow itself, so the schedule section uses its own capability.
  const canManageSchedules = isAllowed(
    useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin" || currentUserRole === "member")
  )
  // A run's input form is generated from the workflow's input_schema; a workflow
  // that declares none takes no input and the form is absent.
  const inputFields = useMemo(
    () => (workflow ? (parseInputSchema(workflow.definition)?.fields ?? []) : []),
    [workflow],
  )
  // Read-only topology derived from the edited definition, so it tracks the form
  // while editing and reuses the run page's DAG rendering.
  const topologyNodes = useMemo<GraphNode[]>(() => {
    const nameOf = (id: string) => agents.find((a) => a.id === id)?.name
    return steps.map((step, i) => ({
      id: step.id,
      sublabel: nameOf(step.targetAgentId) ?? step.targetAgentId ?? undefined,
      needs: effectiveNeeds(steps, i),
    }))
  }, [steps, agents])

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
      hydrateSteps(mappedWorkflow.definition)
      if (!didInitTab.current) {
        setTab(mappedWorkflow.status === "published" ? "overview" : "definition")
        didInitTab.current = true
      }
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

  // Resolve the opaque author id to a member's name; fall back to the id only
  // when the member is not in the loaded list.
  const memberName = useCallback(
    (userId: string) => {
      const member = currentSpaceMembers.find((m) => m.user_id === userId)
      return member?.user_name || member?.user_email || userId
    },
    [currentSpaceMembers]
  )
  const revisionEntries = useMemo(
    () =>
      revisionsData?.map((rev) => ({
        id: rev.id,
        revision: rev.revision,
        createdBy: memberName(rev.createdBy),
        createdLabel: rev.createdLabel,
        summary: `${rev.name} · ${statusLabel(rev.status)}`,
      })) ?? null,
    [revisionsData, memberName]
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

  // saveWith persists the current form, writing the given lifecycle state. The
  // three authoring actions each name the state they reach — Save as draft,
  // Publish, Archive — so lifecycle changes are explicit rather than hidden in a
  // status control. All round-trip through the same update call and refresh
  // revision history.
  function saveWith(targetStatus: Workflow["status"]) {
    if (!token || !spaceId || !workflow || !canManageWorkflows) return
    setSaving(true)
    setError(null)
    updateWorkflow(
      spaceId,
      workflow.id,
      { name: name.trim(), description, definition: stepsDefinition, status: targetStatus },
      token,
    )
      .then((updated) => {
        const mapped = apiWorkflowToWorkflow(updated)
        setWorkflow(mapped)
        setName(mapped.name)
        setDescription(mapped.description)
        hydrateSteps(mapped.definition)
        loadRevisions()
      })
      .catch((err) => setError(getErrorMessage(err, "Failed to update workflow")))
      .finally(() => setSaving(false))
  }

  function handleSaveDraft() {
    saveWith("draft")
  }

  function handlePublish() {
    saveWith("published")
  }

  function handleArchive() {
    saveWith("archived")
  }

  // Discard re-hydrates the form from the loaded workflow, dropping unsaved edits.
  function discardEdits() {
    if (!workflow) return
    setName(workflow.name)
    setDescription(workflow.description)
    hydrateSteps(workflow.definition)
    setError(null)
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
        hydrateSteps(mapped.definition)
        loadRevisions()
      })
      .catch((err) => setRestoreRevisionError({ revision, message: getErrorMessage(err, "Failed to restore revision") }))
      .finally(() => setRestoringRevision(null))
  }

  // Run opens the input modal when the workflow declares an input_schema, and
  // otherwise starts the run directly.
  function handleRunClick() {
    if (inputFields.length > 0) {
      setRunModalOpen(true)
      return
    }
    void handleRunWorkflow()
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
        setRunModalOpen(false)
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

  const published = workflow?.status === "published"
  const saveDisabled =
    saving ||
    loading ||
    workflow == null ||
    !name.trim() ||
    stepErrors.length > 0 ||
    (stepsAdvanced && definitionParseError != null)

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">
            {workflow ? workflow.name : "Workflow Detail"}
            {workflow ? (
              <span className={`workflow-status-pill workflow-status-pill--${workflow.status}`}>
                {statusLabel(workflow.status)}
              </span>
            ) : null}
          </h1>
          <p className="page-activity__subtitle">
            {workflow?.description || "Edit the workflow definition, run it manually, and inspect recent executions."}
          </p>
        </div>
        <div className="page-activity__actions">
          <ButtonLink variant="tertiary" href={buildHash({ name: "workflows", spaceId })}>
            Back to Workflows
          </ButtonLink>
          <Button variant="tertiary" disabled={loading} onClick={() => void load()}>
            Refresh
          </Button>
          <Button
            variant="primary"
            busy={running}
            disabled={loading || workflow == null || !published}
            onClick={handleRunClick}
          >
            Run Workflow
          </Button>
        </div>
      </div>

      {error ? <p className="page-activity__empty">{error}</p> : null}

      {workflow ? (
        <>
          <DetailTabs<Tab>
            tabs={[
              { id: "overview", label: "Overview" },
              { id: "definition", label: "Definition" },
              { id: "runs", label: "Runs", count: runs.length },
              { id: "schedules", label: "Schedules" },
              { id: "revisions", label: "Revisions", count: workflow.revision },
            ]}
            active={tab}
            onChange={setTab}
            label="Workflow sections"
            idPrefix="workflow"
          />

          {tab === "overview" ? (
            <section className="detail-tabs__panel" role="tabpanel" id="workflow-panel-overview" aria-labelledby="workflow-tab-overview">
              {!published ? (
                <p className="workflow-detail__banner">
                  This workflow is currently {statusLabel(workflow.status)}. Open the Definition tab to keep working on
                  it, then Publish before manual runs, schedules, or issue assignment.
                </p>
              ) : null}
              <div className="issues-page__toolbar">
                <h2 className="issues-page__section-title">Plan</h2>
                <span className="page-activity__meta">
                  {steps.length} {steps.length === 1 ? "step" : "steps"}
                  {inputFields.length > 0
                    ? ` · ${inputFields.length} ${inputFields.length === 1 ? "input" : "inputs"}`
                    : ""}
                </span>
              </div>
              <WorkflowGraph nodes={topologyNodes} emptyLabel="This workflow has no steps." />
            </section>
          ) : null}

          {tab === "definition" ? (
            <section className="detail-tabs__panel" role="tabpanel" id="workflow-panel-definition" aria-labelledby="workflow-tab-definition">
              {canManageWorkflowsState === "denied" ? (
                <p className="page-activity__empty">
                  This workflow is read-only for your role. You can still inspect it, and run it when it is published.
                </p>
              ) : canManageWorkflowsState === "failed" ? (
                <p className="page-activity__empty">
                  Couldn&apos;t verify your role in this space, so editing stays unavailable. Refresh to try again.
                </p>
              ) : canManageWorkflowsState === "unknown" ? (
                <p className="page-activity__empty">Checking whether you can manage this workflow…</p>
              ) : null}

              {published ? (
                <p className="workflow-detail__banner">
                  Editing a published workflow. Save as draft keeps your changes without publishing; Publish writes them
                  as a new published revision.
                </p>
              ) : null}

              <details className="workflow-editor__settings">
                <summary className="workflow-editor__settings-summary">Settings — name and description</summary>
                <div className="workflow-editor__settings-fields">
                  <label className="issues-page__field">
                    <span className="issues-page__field-label">Name</span>
                    <input className="issues-page__input" value={name} disabled={!canManageWorkflows} onChange={(e) => setName(e.target.value)} />
                  </label>
                  <label className="issues-page__field">
                    <span className="issues-page__field-label">Description</span>
                    <textarea
                      className="issues-page__textarea"
                      rows={3}
                      value={description}
                      disabled={!canManageWorkflows}
                      onChange={(e) => setDescription(e.target.value)}
                    />
                  </label>
                </div>
              </details>

              <WorkflowStepsEditor state={stepsState} agents={agents} disabled={!canManageWorkflows} fill />

              {canManageWorkflows ? (
                <div className="workflow-page__inline-actions">
                  <Button variant="secondary" busy={saving} disabled={saveDisabled} onClick={handleSaveDraft}>
                    Save as draft
                  </Button>
                  <Button variant="primary" busy={saving} disabled={saveDisabled} onClick={handlePublish}>
                    Publish
                  </Button>
                  <Button variant="tertiary" disabled={saving} onClick={discardEdits}>
                    Discard changes
                  </Button>
                  {workflow.status !== "archived" ? (
                    <Button variant="tertiary" disabled={saving} onClick={handleArchive}>
                      Archive
                    </Button>
                  ) : null}
                </div>
              ) : null}
            </section>
          ) : null}

          {tab === "runs" ? (
            <section className="detail-tabs__panel" role="tabpanel" id="workflow-panel-runs" aria-labelledby="workflow-tab-runs">
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
                          <strong>{statusLabel(run.status)}</strong>
                          <span className="page-activity__meta workflow-detail-page__run-id">{run.id}</span>
                        </span>
                        <span className="page-activity__meta">{run.createdLabel}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          ) : null}

          {tab === "schedules" ? (
            <section className="detail-tabs__panel" role="tabpanel" id="workflow-panel-schedules" aria-labelledby="workflow-tab-schedules">
              {!published ? (
                <p className="page-activity__empty">
                  Only a published workflow can be scheduled. Publish it from the Definition tab first.
                </p>
              ) : token ? (
                <SchedulesSection
                  token={token}
                  spaceId={spaceId}
                  executorKind="workflow"
                  executorId={workflow.id}
                  executorName={workflow.name}
                  workflowDefinition={workflow.definition}
                  canManage={canManageSchedules}
                />
              ) : null}
            </section>
          ) : null}

          {tab === "revisions" ? (
            <section className="detail-tabs__panel" role="tabpanel" id="workflow-panel-revisions" aria-labelledby="workflow-tab-revisions">
              <RevisionHistory
                title="Version history"
                state={revisionsState}
                onRetry={loadRevisions}
                currentRevision={workflow.revision}
                canRestore={canManageWorkflows}
                restoringRevision={restoringRevision}
                restoreError={restoreRevisionError}
                onRestore={handleRestoreRevision}
              />
              <p className="page-activity__meta">
                Restoring writes that version's name, description, and steps back as a new version. The lifecycle state is
                left as it is.
              </p>
            </section>
          ) : null}
        </>
      ) : null}

      <BaseModal
        open={runModalOpen}
        title="Run Workflow"
        titleId="workflow-run-modal-title"
        onClose={() => setRunModalOpen(false)}
      >
        <p className="page-activity__subtitle">
          {workflow ? `${workflow.name} · v${workflow.revision}` : ""} — provide inputs, then start a run.
        </p>
        <WorkflowRunInputForm
          fields={inputFields}
          values={inputValues}
          disabled={running}
          onChange={(fieldName, value) => setInputValues((prev) => ({ ...prev, [fieldName]: value }))}
        />
        {error ? <p className="page-activity__empty">{error}</p> : null}
        <div className="workflow-page__inline-actions">
          <Button variant="primary" busy={running} disabled={workflow == null || !published} onClick={handleRunWorkflow}>Start run</Button>
          <Button variant="secondary" disabled={running} onClick={() => setRunModalOpen(false)}>
            Cancel
          </Button>
        </div>
      </BaseModal>
    </div>
  )
}
