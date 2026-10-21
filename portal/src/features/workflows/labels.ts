import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import { HUMAN_INPUT_STEP_TYPE, type WorkflowStepDraft } from "./steps"

/**
 * The person-facing words for a step in the editor and the plan. A step is
 * read as "Step N", numbered in definition order as its validation errors and
 * its run's steps are; its generated id is diagnostics, not its name.
 */
export function stepHeading(index: number, t: Translate<MessageKey>): string {
  return t("workflows.step.heading", { step: index + 1 })
}

/** Who carries the step out: its Agent by name, or the person an input step
 *  waits on. Never the Agent's id, which says nothing to a reader. */
export function stepExecutorLabel(
  step: WorkflowStepDraft,
  agentLabels: Map<string, string>,
  t: Translate<MessageKey>,
): string {
  if (step.type === HUMAN_INPUT_STEP_TYPE) return t("workflows.editor.byPerson")
  if (!step.targetAgentId) return t("workflows.editor.noAgent")
  return agentLabels.get(step.targetAgentId) ?? t("workflows.editor.agentMissing")
}

/** The runtime's Issue access modes, in the order the editor offers them, each
 *  with the label, one-line hint, and node badge a person reads instead of the
 *  wire value. `none` has no badge: a step without Issue access is the default. */
export const ISSUE_ACCESS_OPTIONS = [
  { value: "none", label: "workflows.issueAccess.none", hint: "workflows.issueAccess.noneHint", badge: null },
  {
    value: "if_bound",
    label: "workflows.issueAccess.ifBound",
    hint: "workflows.issueAccess.ifBoundHint",
    badge: "workflows.issueAccess.ifBoundBadge",
  },
  {
    value: "required",
    label: "workflows.issueAccess.required",
    hint: "workflows.issueAccess.requiredHint",
    badge: "workflows.issueAccess.requiredBadge",
  },
] as const satisfies readonly { value: string; label: MessageKey; hint: MessageKey; badge: MessageKey | null }[]

export function issueAccessOption(value: string | undefined) {
  return ISSUE_ACCESS_OPTIONS.find((option) => option.value === (value || "none"))
}
