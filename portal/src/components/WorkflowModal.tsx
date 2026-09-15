import { useEffect, useRef, useState } from "react"
import { BaseModal } from "@buildmax/gui"
import type { Agent } from "../lib/types"
import { newStep, stepsToDefinition, useWorkflowSteps, WorkflowStepsEditor } from "../features/workflows"

interface WorkflowModalProps {
  open: boolean
  agents?: Agent[]
  loading: boolean
  error: string | null
  onClose: () => void
  onSubmit: (values: { name: string; description: string; definition: string }) => void
}

/** Creates a new Workflow. Editing an existing one happens on its own detail
 *  page (`WorkflowDetail`), which needs Runs and History alongside the same
 *  step editor -- there is no reason to fit both into one modal. */
export function WorkflowModal({ open, agents = [], loading, error, onClose, onSubmit }: WorkflowModalProps) {
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const {
    steps,
    definition,
    errors,
    advanced,
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
    hydrate,
  } = useWorkflowSteps(agents)

  // Initialize the form once per open, not on every `agents` change: the agent
  // list can load or refetch after the dialog is open, and re-running this would
  // wipe the name, description, and step the user has already filled in -- which
  // left the Create button stuck disabled.
  const initializedForOpen = useRef(false)
  useEffect(() => {
    if (!open) {
      initializedForOpen.current = false
      return
    }
    if (initializedForOpen.current) return
    initializedForOpen.current = true
    setName("")
    setDescription("")
    hydrate(stepsToDefinition([newStep(agents[0]?.id ?? "")]))
  }, [open, agents, hydrate])

  const canSubmit = !loading && name.trim() !== "" && errors.length === 0 && !(advanced && definitionParseError)

  return (
    <BaseModal open={open} title="New Workflow" titleId="workflow-modal-title" onClose={onClose} className="modal--large">
      <div className="modal__body">
        <div className="workflow-page__form">
          <label className="issues-page__field">
            <span className="issues-page__field-label">Name</span>
            <input className="issues-page__input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Customer research workflow" />
          </label>
          <label className="issues-page__field">
            <span className="issues-page__field-label">Description</span>
            <textarea className="issues-page__textarea" rows={4} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this workflow does" />
          </label>

          <WorkflowStepsEditor
            steps={steps}
            agents={agents}
            errors={errors}
            advanced={advanced}
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
          {agents.length === 0 ? (
            <p className="page-activity__meta">Create at least one agent first to assign it to a step.</p>
          ) : null}

          {error ? (
            <p className="modal__error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="modal__actions">
            <button type="button" className="modal__btn modal__btn--secondary" onClick={onClose} disabled={loading}>
              Cancel
            </button>
            <button
              type="button"
              className="modal__btn modal__btn--secondary"
              disabled={!canSubmit}
              onClick={() => onSubmit({ name: name.trim(), description, definition })}
            >
              {loading ? "Creating workflow…" : "Create workflow"}
            </button>
          </div>
        </div>
      </div>
    </BaseModal>
  )
}
