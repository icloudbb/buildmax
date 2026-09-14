import type { Agent } from "../../lib/types"

/**
 * `agent_task` is the only step type the runtime executes today, so it is not
 * a user-editable field: it exists on the draft only so a step parsed from
 * advanced-mode JSON can carry -- and validation can reject -- a type this
 * Portal build does not know how to run.
 */
export const AGENT_TASK_STEP_TYPE = "agent_task"

/** The only workflow definition contract version the runtime accepts. The
 *  Portal always emits it so a published plan names the contract it targets;
 *  the server rejects any other value. */
export const WORKFLOW_SCHEMA_VERSION = 1

/** A binding feeds an earlier step's whole output into this step's input under
 *  a name. The step form authors it directly; it also round-trips through
 *  advanced JSON mode so a definition authored there is not silently stripped. */
export interface WorkflowStepBinding {
  name: string
  fromStep: string
}

export interface WorkflowStepDraft {
  id: string
  type: string
  targetAgentId: string
  prompt: string
  bindings?: WorkflowStepBinding[]
}

export interface ParsedWorkflowDefinition {
  steps: WorkflowStepDraft[]
}

/** A step's id is generated, not typed -- there is no meaning to a person
 *  choosing one, only a risk of an accidental collision. */
export function newStepId(): string {
  const random =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2)
  return `step_${random.replace(/-/g, "").slice(0, 8)}`
}

export function newStep(agentId = ""): WorkflowStepDraft {
  return {
    id: newStepId(),
    type: AGENT_TASK_STEP_TYPE,
    targetAgentId: agentId,
    prompt: "Describe what this step should do.",
  }
}

export function stepsToDefinition(steps: WorkflowStepDraft[]): string {
  return JSON.stringify(
    {
      schema_version: WORKFLOW_SCHEMA_VERSION,
      steps: steps.map((step) => ({
        step_id: step.id,
        type: step.type,
        target_agent_id: step.targetAgentId,
        prompt: step.prompt,
        ...(step.bindings && step.bindings.length > 0
          ? { bindings: step.bindings.map((binding) => ({ name: binding.name, from_step: binding.fromStep })) }
          : {}),
      })),
    },
    null,
    2,
  )
}

/** Reads a step's `bindings` array, keeping malformed entries (as empty
 *  strings) so server validation surfaces the mistake rather than the Portal
 *  dropping it silently. */
function parseStepBindings(value: unknown): WorkflowStepBinding[] | undefined {
  if (!Array.isArray(value)) return undefined
  const bindings = value.flatMap((entry): WorkflowStepBinding[] => {
    if (typeof entry !== "object" || entry == null) return []
    const record = entry as Record<string, unknown>
    return [
      {
        name: typeof record.name === "string" ? record.name : "",
        fromStep: typeof record.from_step === "string" ? record.from_step : "",
      },
    ]
  })
  return bindings.length > 0 ? bindings : undefined
}

/**
 * Reads a definition JSON string into the draft shape the editor uses.
 *
 * A step's `type` is read verbatim rather than defaulted to `agent_task`:
 * advanced mode is how a definition with an unsupported type would arrive,
 * and silently coercing it here would hide exactly the mistake
 * {@link validateSteps} exists to catch.
 */
export function parseDefinition(definition: string): ParsedWorkflowDefinition | null {
  try {
    const parsed = JSON.parse(definition) as { steps?: unknown }
    if (!Array.isArray(parsed.steps)) return null
    return {
      steps: parsed.steps.map((step): WorkflowStepDraft => {
        const record = typeof step === "object" && step != null ? (step as Record<string, unknown>) : {}
        return {
          id: typeof record.step_id === "string" && record.step_id.trim() ? record.step_id : newStepId(),
          type: typeof record.type === "string" && record.type.trim() ? record.type : AGENT_TASK_STEP_TYPE,
          targetAgentId: typeof record.target_agent_id === "string" ? record.target_agent_id : "",
          prompt: typeof record.prompt === "string" ? record.prompt : "",
          bindings: parseStepBindings(record.bindings),
        }
      }),
    }
  } catch {
    return null
  }
}

/** A validation problem with the step list. `index` is -1 for a problem with
 *  the list as a whole (e.g. no steps), not with one step. */
export interface StepError {
  index: number
  message: string
}

/**
 * The one validation both the Agent-step form and the advanced JSON mode run
 * against, so neither path can leave Save enabled for a definition the
 * runtime would refuse -- an unsupported step type included.
 */
export function validateSteps(steps: WorkflowStepDraft[], agents: Agent[]): StepError[] {
  const errors: StepError[] = []
  if (steps.length === 0) {
    errors.push({ index: -1, message: "A workflow needs at least one step." })
    return errors
  }
  const seenIds = new Set<string>()
  steps.forEach((step, index) => {
    if (!step.id.trim()) {
      errors.push({ index, message: "This step is missing its id." })
    } else if (seenIds.has(step.id)) {
      errors.push({ index, message: `Step id "${step.id}" is used by more than one step.` })
    }
    seenIds.add(step.id)
    if (step.type !== AGENT_TASK_STEP_TYPE) {
      errors.push({
        index,
        message: `"${step.type}" is not a step type the runtime supports yet -- only Agent steps are.`,
      })
    }
    if (!step.targetAgentId) {
      errors.push({ index, message: "Choose an agent for this step." })
    } else if (!agents.some((agent) => agent.id === step.targetAgentId)) {
      errors.push({ index, message: "The agent this step targets no longer exists." })
    }
    if (!step.prompt.trim()) {
      errors.push({ index, message: "This step needs a prompt." })
    }
    // A binding feeds an earlier step's whole output into this step, so it can
    // only name a step that already ran, and each name on a step is distinct --
    // the same rules the server enforces, checked here so Save stays disabled
    // for a definition the server would reject.
    const earlierIds = new Set(steps.slice(0, index).map((s) => s.id))
    const bindingNames = new Set<string>()
    step.bindings?.forEach((binding) => {
      const name = binding.name.trim()
      const label = name || "(unnamed)"
      if (!name) {
        errors.push({ index, message: "An input binding needs a name." })
      } else if (bindingNames.has(name)) {
        errors.push({ index, message: `Input binding "${name}" is defined more than once on this step.` })
      }
      bindingNames.add(name)
      if (!binding.fromStep) {
        errors.push({ index, message: `Input binding "${label}" needs an earlier step to read from.` })
      } else if (!earlierIds.has(binding.fromStep)) {
        errors.push({ index, message: `Input binding "${label}" must read from an earlier step.` })
      }
    })
  })
  return errors
}
