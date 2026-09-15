import type { Agent } from "../../lib/types"
import { nodeOutputSource, WORKFLOW_INPUT_SOURCE } from "./steps"
import type { StepError, WorkflowStepBinding, WorkflowStepDraft } from "./steps"

interface WorkflowStepsEditorProps {
  steps: WorkflowStepDraft[]
  agents: Agent[]
  disabled?: boolean
  errors: StepError[]
  advanced: boolean
  definitionText: string
  definitionParseError: string | null
  onAddStep: () => void
  onRemoveStep: (index: number) => void
  onChangeStep: (index: number, patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt">>) => void
  onAddBinding: (stepIndex: number) => void
  onRemoveBinding: (stepIndex: number, bindingIndex: number) => void
  onChangeBinding: (stepIndex: number, bindingIndex: number, patch: Partial<WorkflowStepBinding>) => void
  onToggleAdvanced: () => void
  onDefinitionTextChange: (text: string) => void
}

/**
 * The normal Workflow editor: one Agent-step card per step, the only step
 * type the runtime executes. Raw definition JSON is available through the
 * "advanced" toggle for exact inspection, not shown beside this form as an
 * equivalent path -- and both paths validate through the same
 * {@link validateSteps}, so neither can leave Save enabled for a step the
 * runtime would refuse.
 */
export function WorkflowStepsEditor({
  steps,
  agents,
  disabled = false,
  errors,
  advanced,
  definitionText,
  definitionParseError,
  onAddStep,
  onRemoveStep,
  onChangeStep,
  onAddBinding,
  onRemoveBinding,
  onChangeBinding,
  onToggleAdvanced,
  onDefinitionTextChange,
}: WorkflowStepsEditorProps) {
  const listError = errors.find((e) => e.index === -1)

  return (
    <section className="workflow-page__builder">
      <div className="issues-page__toolbar">
        <h3 className="issues-page__section-title">Steps</h3>
        <div className="workflow-page__builder-actions">
          {!disabled && !advanced ? (
            <button type="button" className="page-activity__action-btn" onClick={onAddStep}>
              Add Agent Step
            </button>
          ) : null}
          <button type="button" className="page-activity__action-btn" onClick={onToggleAdvanced}>
            {advanced ? "Hide advanced JSON" : "Advanced: edit raw JSON"}
          </button>
        </div>
      </div>

      {listError ? <p className="page-activity__empty">{listError.message}</p> : null}

      {advanced ? (
        <label className="issues-page__field">
          <span className="issues-page__field-label">
            Definition (JSON) -- for exact inspection or a change the step form cannot express yet. The step
            form above reflects it once it parses.
          </span>
          <textarea
            className="issues-page__textarea workflow-page__definition"
            rows={12}
            value={definitionText}
            disabled={disabled}
            onChange={(e) => onDefinitionTextChange(e.target.value)}
          />
          {definitionParseError ? (
            <span className="issues-page__field-label">{definitionParseError}</span>
          ) : (
            errors.map((e, i) => (
              <p key={i} className="modal__error">
                {e.index === -1 ? e.message : `Step ${e.index + 1}: ${e.message}`}
              </p>
            ))
          )}
        </label>
      ) : steps.length === 0 ? (
        <p className="page-activity__empty">No steps yet.</p>
      ) : (
        <ol className="workflow-page__steps">
          {steps.map((step, index) => {
            const stepErrors = errors.filter((e) => e.index === index)
            return (
              <li key={step.id} className="workflow-page__step">
                <div className="workflow-page__step-head">
                  <strong>Agent Step {index + 1}</strong>
                  <span className="page-activity__meta workflow-page__step-id">id: {step.id}</span>
                  {!disabled ? (
                    <button
                      type="button"
                      className="page-activity__action-btn"
                      disabled={steps.length === 1}
                      onClick={() => onRemoveStep(index)}
                    >
                      Remove
                    </button>
                  ) : null}
                </div>
                <label className="issues-page__field">
                  <span className="issues-page__field-label">Agent</span>
                  <select
                    className="issues-page__input"
                    value={step.targetAgentId}
                    disabled={disabled}
                    onChange={(e) => onChangeStep(index, { targetAgentId: e.target.value })}
                  >
                    <option value="">Select an agent</option>
                    {agents.map((agent) => (
                      <option key={agent.id} value={agent.id}>
                        {agent.name} ({agent.id})
                      </option>
                    ))}
                  </select>
                </label>
                <label className="issues-page__field">
                  <span className="issues-page__field-label">Prompt</span>
                  <textarea
                    className="issues-page__textarea"
                    rows={4}
                    value={step.prompt}
                    disabled={disabled}
                    onChange={(e) => onChangeStep(index, { prompt: e.target.value })}
                  />
                </label>
                {(() => {
                  // A binding selects a value from the workflow input or an
                  // earlier step's output envelope at an RFC 6901 pointer, and
                  // feeds it into this step's prompt under a name. A node source
                  // can only name a step above this one; the workflow input is
                  // always available, so the control shows on every step.
                  const earlierSteps = steps.slice(0, index)
                  const bindings = step.bindings ?? []
                  return (
                    <div className="workflow-page__step-bindings">
                      <span className="issues-page__field-label">Inputs from the workflow input and earlier steps</span>
                      {bindings.map((binding, bindingIndex) => (
                        <div key={bindingIndex} className="workflow-page__binding">
                          <input
                            className="issues-page__input"
                            aria-label={`Input ${bindingIndex + 1} name`}
                            placeholder="name"
                            value={binding.name}
                            disabled={disabled}
                            onChange={(e) => onChangeBinding(index, bindingIndex, { name: e.target.value })}
                          />
                          <select
                            className="issues-page__input"
                            aria-label={`Input ${bindingIndex + 1} source`}
                            value={binding.source}
                            disabled={disabled}
                            onChange={(e) => onChangeBinding(index, bindingIndex, { source: e.target.value })}
                          >
                            <option value="">Select a source</option>
                            <option value={WORKFLOW_INPUT_SOURCE}>Workflow input</option>
                            {earlierSteps.map((earlier, earlierIndex) => (
                              <option key={earlier.id} value={nodeOutputSource(earlier.id)}>
                                Step {earlierIndex + 1} output ({earlier.id})
                              </option>
                            ))}
                          </select>
                          <input
                            className="issues-page__input"
                            aria-label={`Input ${bindingIndex + 1} pointer`}
                            placeholder="pointer, e.g. /text (empty = whole value)"
                            value={binding.pointer}
                            disabled={disabled}
                            onChange={(e) => onChangeBinding(index, bindingIndex, { pointer: e.target.value })}
                          />
                          {!disabled ? (
                            <button
                              type="button"
                              className="page-activity__action-btn"
                              onClick={() => onRemoveBinding(index, bindingIndex)}
                            >
                              Remove input
                            </button>
                          ) : null}
                        </div>
                      ))}
                      {!disabled ? (
                        <button
                          type="button"
                          className="page-activity__action-btn"
                          onClick={() => onAddBinding(index)}
                        >
                          Add input
                        </button>
                      ) : null}
                    </div>
                  )
                })()}
                {stepErrors.map((e, i) => (
                  <p key={i} className="modal__error">
                    {e.message}
                  </p>
                ))}
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}
