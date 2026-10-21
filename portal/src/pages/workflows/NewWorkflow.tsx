import { useCallback, useEffect, useId, useRef, useState } from "react"
import { Button, EditorActions, Field, Input, PageHeader, Skeleton, Textarea } from "@buildmax/gui"
import type { Agent } from "../../lib/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { apiAgentToAgent } from "../../lib/api/mappers"
import { getAgents } from "../../features/agents"
import { createWorkflow, newStep, stepsToDefinition, useWorkflowSteps, WorkflowStepsEditor } from "../../features/workflows"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { useStableT, useT } from "../../i18n"

interface NewWorkflowProps {
  token: string | null
  spaceId: string
}

/**
 * Authors a new Workflow on a page sized for its graph, the same editor its
 * detail page's Definition tab uses. Nothing is stored until Create: the server
 * validates a definition on creation, so the plan has to be runnable-shaped
 * before it exists. Afterwards it lives on its detail page.
 */
export function NewWorkflow({ token, spaceId }: NewWorkflowProps) {
  const t = useT()
  const stableT = useStableT()
  const { currentUserRole } = useSpace()
  const canManageState = useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin")
  const createReasonId = useId()
  // null until the Space's Agents have loaded: the first step names one.
  const [agents, setAgents] = useState<Agent[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const stepsState = useWorkflowSteps(agents ?? [])
  const { definition, errors, advanced, definitionParseError, hydrate } = stepsState

  const loadAgents = useCallback(() => {
    if (!token || !spaceId) return
    setLoadError(null)
    getAgents(spaceId, token)
      .then((list) => setAgents(list.map(apiAgentToAgent)))
      .catch((err) => setLoadError(getErrorMessage(err, stableT("workflows.create.error.agents"))))
  }, [token, spaceId, stableT])

  useEffect(() => {
    loadAgents()
  }, [loadAgents])

  // Start the plan with one Agent step once the Agents are known. Only once: a
  // later refetch must not wipe what the author has already written.
  const started = useRef(false)
  useEffect(() => {
    if (agents === null || started.current) return
    started.current = true
    hydrate(stepsToDefinition([newStep(agents[0]?.id ?? "", stableT("workflows.step.defaultPrompt"))]))
  }, [agents, hydrate, stableT])

  const createBlockedReason = !name.trim()
    ? t("workflows.create.needName")
    : advanced && definitionParseError
      ? t(definitionParseError)
      : errors.length > 0
        ? t("workflows.create.fixSteps")
        : null

  function handleCreate() {
    if (!token || !spaceId || createBlockedReason) return
    setSaving(true)
    setError(null)
    createWorkflow(spaceId, { name: name.trim(), description, definition }, token)
      // Success lands on the created Workflow, a draft until it is published.
      .then((created) => navigate({ name: "workflow", spaceId, workflowId: created.id }))
      .catch((err) => setError(getErrorMessage(err, stableT("workflows.error.create"))))
      .finally(() => setSaving(false))
  }

  return (
    <div className="page-activity">
      <PageHeader title={t("workflows.new")} subtitle={t("workflows.create.subtitle")} />

      {canManageState === "denied" ? (
        <p className="page-activity__empty">{t("workflows.access.denied")}</p>
      ) : canManageState === "failed" ? (
        <p className="page-activity__empty">{t("workflows.access.unverified")}</p>
      ) : canManageState === "unknown" ? (
        <p className="page-activity__empty">{t("workflows.access.checking")}</p>
      ) : loadError ? (
        <Alert tone="error" message={loadError} retry={{ label: t("shell.retry"), onClick: loadAgents }} />
      ) : agents === null ? (
        <Skeleton />
      ) : (
        <div className="workflow-page__form">
          <Field label={t("workflows.field.name")}>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("workflows.create.namePlaceholder")}
            />
          </Field>
          <Field label={t("workflows.field.description")} optional>
            <Textarea
              rows={2}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("workflows.create.descriptionPlaceholder")}
            />
          </Field>

          <WorkflowStepsEditor state={stepsState} agents={agents} />
          {agents.length === 0 ? <p className="page-activity__meta">{t("workflows.create.needAgent")}</p> : null}

          {error ? (
            <p className="workflow-page__create-error" role="alert">
              {error}
            </p>
          ) : null}
          <EditorActions>
            {createBlockedReason ? (
              <span id={createReasonId} className="page-activity__meta">
                {createBlockedReason}
              </span>
            ) : null}
            <Button variant="tertiary" disabled={saving} onClick={() => navigate({ name: "workflows", spaceId })}>
              {t("workflows.cancel")}
            </Button>
            <Button
              variant="primary"
              busy={saving}
              disabled={createBlockedReason !== null}
              aria-describedby={createBlockedReason ? createReasonId : undefined}
              onClick={handleCreate}
            >
              {t("workflows.create.submit")}
            </Button>
          </EditorActions>
        </div>
      )}
    </div>
  )
}
