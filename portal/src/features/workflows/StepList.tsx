import { useT } from "../../i18n"
import { stepExecutorLabel, stepHeading } from "./labels"
import { effectiveNeeds, type WorkflowStepDraft } from "./steps"

/** Below this width a graph cannot be read without a horizontal scroll, nor
 *  edited: the editor and the Overview plan show this list instead. The
 *  stylesheet stacks the editor at the same breakpoint (workflows.css). */
export const STEP_LIST_QUERY = "(max-width: 767px)"

interface WorkflowStepListProps {
  steps: WorkflowStepDraft[]
  /** Agent picker labels by id (see agentOptionLabels). */
  agentLabels: Map<string, string>
  /** Names the list for assistive technology. */
  label: string
  /** When set, each step is a button that selects it for editing. */
  onSelect?: (id: string) => void
  selectedId?: string | null
}

/**
 * The plan as an ordered list: each step's number, who carries it out, what it
 * is asked to do, and the steps it waits for. It is how a narrow screen reads a
 * Workflow, where a graph would need a horizontal scroll, and in the editor it
 * is how a step is chosen there.
 */
export function WorkflowStepList({ steps, agentLabels, label, onSelect, selectedId }: WorkflowStepListProps) {
  const t = useT()
  const indexById = new Map(steps.map((step, i) => [step.id, i]))
  return (
    <ol className="wf-step-list" aria-label={label}>
      {steps.map((step, i) => {
        const after = effectiveNeeds(steps, i)
          .map((need) => indexById.get(need))
          .filter((index): index is number => index !== undefined)
          .map((index) => stepHeading(index, t))
        const body = (
          <>
            <span className="wf-step-list__heading">{stepHeading(i, t)}</span>
            <span className="wf-step-list__executor">{stepExecutorLabel(step, agentLabels, t)}</span>
            {step.prompt ? <span className="wf-step-list__prompt">{step.prompt}</span> : null}
            {after.length > 0 ? (
              <span className="wf-step-list__after">
                {t("workflows.steps.after", { steps: after.join(t("workflows.listSeparator")) })}
              </span>
            ) : null}
          </>
        )
        return (
          <li key={step.id}>
            {onSelect ? (
              <button
                type="button"
                className="wf-step-list__item wf-step-list__item--button"
                aria-current={step.id === selectedId ? "step" : undefined}
                onClick={() => onSelect(step.id)}
              >
                {body}
              </button>
            ) : (
              <div className="wf-step-list__item">{body}</div>
            )}
          </li>
        )
      })}
    </ol>
  )
}
