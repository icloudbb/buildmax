import type { Ref } from "react"
import { Button, Field, Select, useStatusLabel } from "@buildmax/gui"
import type { Agent, Workflow } from "../../lib/types"
import type { PermissionState } from "../../state/permissionState"
import { useT, type MessageKey } from "../../i18n"
import { executorHint, type ExecutorHint } from "./execution"

const HINT_TEXT: Record<ExecutorHint, MessageKey> = {
  agentOrWorkflow: "issues.executor.hint.agentOrWorkflow",
  agentOnly: "issues.executor.hint.agentOnly",
  createAgent: "issues.executor.hint.createAgent",
  askForAgent: "issues.executor.hint.askForAgent",
  nothingYet: "issues.executor.hint.nothingYet",
}

interface ExecutorFieldProps {
  /** `agent:<id>`, `workflow:<id>`, or empty for no executor. */
  value: string
  /** The Issue's saved executor, in the same form, when editing one. */
  savedValue?: string
  onChange: (value: string) => void
  agents: Agent[]
  workflows: Workflow[]
  /** Whether the reader may create Agents and assign Workflows. */
  manage: PermissionState
  onCreateAgent: () => void
  selectRef?: Ref<HTMLSelectElement>
}

/**
 * The Executor field of the New Issue dialog and the Issue edit form. Its hint
 * says what can run an Issue, and in a Space with nothing to run it, how to
 * get something: create an Agent, or ask someone who can.
 */
export function ExecutorField({ value, savedValue, onChange, agents, workflows, manage, onCreateAgent, selectRef }: ExecutorFieldProps) {
  const t = useT()
  const statusLabel = useStatusLabel()
  const allowWorkflows = manage === "allowed"
  // A Workflow already assigned stays listed, labelled with its status, so
  // editing an Issue does not silently drop it; only published ones are new
  // choices.
  const assigned = savedValue ?? value
  const selectedWorkflowId = assigned.startsWith("workflow:") ? assigned.slice("workflow:".length) : ""
  const selectableWorkflows = allowWorkflows
    ? workflows.filter((workflow) => workflow.status === "published" || workflow.id === selectedWorkflowId)
    : []
  const hint = executorHint({
    agentCount: agents.length,
    publishedWorkflowCount: workflows.filter((workflow) => workflow.status === "published").length,
    manage,
  })

  return (
    <Field label={t("issues.field.executor")} hint={t(HINT_TEXT[hint])}>
      <Select ref={selectRef} value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">{t("issues.none")}</option>
        {agents.map((agent) => (
          <option key={agent.id} value={`agent:${agent.id}`}>
            {agent.name}
          </option>
        ))}
        {selectableWorkflows.map((workflow) => (
          <option key={workflow.id} value={`workflow:${workflow.id}`}>
            {workflow.status !== "published"
              ? t("issues.workflowWithStatus", { name: workflow.name, status: statusLabel(workflow.status).toLowerCase() })
              : workflow.name}
          </option>
        ))}
      </Select>
      {/* A block of its own, so the button keeps its width in the field's column. */}
      {hint === "createAgent" ? (
        <div>
          <Button variant="secondary" size="compact" onClick={onCreateAgent}>
            {t("issues.executor.createAgent")}
          </Button>
        </div>
      ) : null}
    </Field>
  )
}
