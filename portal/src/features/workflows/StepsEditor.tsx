import type { Agent } from "../../lib/types"
import { Button, Field, Textarea } from "@buildmax/gui"
import { useT } from "../../i18n"
import { WorkflowVisualEditor } from "./VisualEditor"
import type { WorkflowStepsState } from "./useWorkflowSteps"

interface WorkflowStepsEditorProps {
  state: WorkflowStepsState
  agents: Agent[]
  disabled?: boolean
  /** Let the visual canvas fill the available height. The workflow detail page
   *  sets it so editing is dominated by the graph; the create modal leaves it
   *  off to keep the canvas within the dialog. */
  fill?: boolean
}

/**
 * The Workflow definition editor, in two modes driven by one draft state: a
 * visual graph canvas and a raw JSON view. Both validate through the same
 * {@link validateSteps}, so neither can leave Save enabled for a definition the
 * runtime would refuse. Raw JSON is for exact inspection and for fields the
 * visual editor does not author (input_schema, result, output_schema), which it
 * preserves rather than strips.
 */
export function WorkflowStepsEditor({ state, agents, disabled = false, fill = false }: WorkflowStepsEditorProps) {
  const t = useT()
  const { errors, advanced, definitionText, definitionParseError } = state
  const listError = errors.find((e) => e.index === -1)

  return (
    <section className="workflow-page__builder">
      <div className="issues-page__toolbar">
        <h3 className="issues-page__section-title">{t("workflows.editor.steps")}</h3>
        <div className="workflow-page__builder-actions">
          <Button variant="tertiary" onClick={state.toggleAdvanced}>
            {advanced ? t("workflows.editor.visual") : t("workflows.editor.rawJson")}
          </Button>
        </div>
      </div>

      {listError ? <p className="page-activity__empty">{listError.message}</p> : null}

      {advanced ? (
        <>
          <Field
            label={t("workflows.editor.definitionLabel")}
            error={definitionParseError ? t(definitionParseError) : undefined}
          >
            <Textarea
              className="workflow-page__definition"
              rows={16}
              value={definitionText}
              disabled={disabled}
              onChange={(e) => state.setDefinitionText(e.target.value)}
            />
          </Field>
          {!definitionParseError && errors.length > 0 ? (
            <ul className="workflow-page__errors">
              {errors.map((e, i) => (
                <li key={i}>
                  {e.index === -1 ? e.message : t("workflows.editor.stepError", { step: e.index + 1, message: e.message })}
                </li>
              ))}
            </ul>
          ) : null}
        </>
      ) : (
        <WorkflowVisualEditor state={state} agents={agents} disabled={disabled} fill={fill} />
      )}
    </section>
  )
}
