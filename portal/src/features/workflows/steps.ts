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

/** The binding source that selects into the run's immutable input JSON. */
export const WORKFLOW_INPUT_SOURCE = "workflow.input"

/** Builds/parses the "node.<id>.output" binding source that selects an earlier
 *  step's output envelope. The id may contain dots, so only the fixed prefix and
 *  suffix are stripped -- mirrors the server's ParseNodeOutputSource. */
export function nodeOutputSource(stepId: string): string {
  return `node.${stepId}.output`
}

export function parseNodeOutputSource(source: string): string | null {
  const prefix = "node."
  const suffix = ".output"
  if (source.length <= prefix.length + suffix.length) return null
  if (!source.startsWith(prefix) || !source.endsWith(suffix)) return null
  return source.slice(prefix.length, source.length - suffix.length)
}

/** A binding selects a value from a source (the run input, or an earlier step's
 *  output envelope) at an RFC 6901 pointer, and feeds it into this step's input
 *  under a name. The step form authors it directly; it also round-trips through
 *  advanced JSON mode so a definition authored there is not silently stripped. */
export interface WorkflowStepBinding {
  name: string
  source: string
  pointer: string
}

export interface WorkflowStepDraft {
  id: string
  type: string
  /**
   * The ids of the nodes that must succeed before this one runs -- the `needs`
   * edges of the definition's DAG. `undefined` marks a node the step form
   * created: the form authors a linear chain, so such a node's effective needs
   * are the node directly above it (see {@link effectiveNeeds}). A value read
   * from advanced JSON is preserved verbatim, including an explicit empty array
   * for a root, so a hand-authored DAG round-trips without being flattened.
   */
  needs?: string[]
  /** The node's Issue access mode: "none" (default), "if_bound", or "required".
   *  The step form does not edit it yet; advanced JSON does, and carrying it here
   *  keeps a hand-authored value from being stripped on save. `undefined` is
   *  treated as "none". */
  issueAccess?: string
  targetAgentId: string
  /** The pinned agent revision, when the definition names one (publication pins
   *  it). Carried through parse and serialize so editing a published definition
   *  in the form does not drop the pin. `undefined` means "latest". */
  agentRevision?: number
  prompt: string
  bindings?: WorkflowStepBinding[]
  /** The node's `output_schema` as verbatim JSON text, when it declares one. The
   *  visual editor does not author it (raw JSON does), but carrying it here keeps
   *  a save from stripping a schema the definition already had. `undefined` means
   *  the node declares none. */
  outputSchema?: string
}

export interface ParsedWorkflowDefinition {
  steps: WorkflowStepDraft[]
  /** The definition's `policy.max_parallel_nodes`, or null when absent. Carried
   *  through parse and serialize so a hand-authored concurrency limit is not
   *  stripped when the definition round-trips through the step model. */
  maxParallelNodes: number | null
  /** The definition's `input_schema` and `result` as verbatim JSON text, when it
   *  declares them. Authored in raw JSON, not the visual editor, but preserved
   *  through the round-trip so switching to the visual editor and saving does not
   *  strip them. `undefined` means the definition declares none. */
  inputSchema?: string
  result?: string
}

/**
 * The needs edges a node actually has. A node parsed from JSON carries its own
 * `needs` (possibly empty); a node the form created carries none, and the form
 * authors a linear chain, so its effective need is the node directly above it.
 * Serialization and validation both read edges through here so the definition
 * they emit and the definition they check agree.
 */
export function effectiveNeeds(steps: WorkflowStepDraft[], index: number): string[] {
  const step = steps[index]
  if (step.needs !== undefined) return step.needs
  return index > 0 ? [steps[index - 1].id] : []
}

/**
 * Maps each node id to the set of its transitive predecessors -- every node
 * that must run before it -- from the effective needs edges. A binding may read
 * only a predecessor's output, and this mirrors the server's graph so Save
 * stays disabled for a definition the server would reject.
 */
function transitivePredecessors(steps: WorkflowStepDraft[]): Map<string, Set<string>> {
  const byId = new Map(steps.map((s) => [s.id, s]))
  const preds = new Map<string, Set<string>>()
  const resolve = (id: string, stack: Set<string>): Set<string> => {
    const cached = preds.get(id)
    if (cached) return cached
    const set = new Set<string>()
    if (stack.has(id)) return set // a cycle: stop rather than recurse forever
    stack.add(id)
    const index = steps.findIndex((s) => s.id === id)
    if (index >= 0) {
      for (const need of effectiveNeeds(steps, index)) {
        if (!byId.has(need)) continue
        set.add(need)
        for (const p of resolve(need, stack)) set.add(p)
      }
    }
    stack.delete(id)
    preds.set(id, set)
    return set
  }
  for (const s of steps) resolve(s.id, new Set())
  return preds
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

/** Re-embeds a field carried as verbatim JSON text back into the definition as a
 *  JSON value. The text originated from a successful parse, so it re-parses; a
 *  value that somehow does not is omitted rather than emitted as a string. */
function embedRawJSON(text: string | undefined): unknown {
  if (text === undefined) return undefined
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}

export function stepsToDefinition(
  steps: WorkflowStepDraft[],
  maxParallelNodes: number | null = null,
  inputSchema?: string,
  result?: string,
): string {
  const inputSchemaValue = embedRawJSON(inputSchema)
  const resultValue = embedRawJSON(result)
  return JSON.stringify(
    {
      schema_version: WORKFLOW_SCHEMA_VERSION,
      ...(inputSchemaValue !== undefined ? { input_schema: inputSchemaValue } : {}),
      ...(maxParallelNodes && maxParallelNodes > 0
        ? { policy: { max_parallel_nodes: maxParallelNodes } }
        : {}),
      nodes: steps.map((step, index) => {
        const needs = effectiveNeeds(steps, index)
        const outputSchemaValue = embedRawJSON(step.outputSchema)
        return {
          id: step.id,
          type: step.type,
          ...(needs.length > 0 ? { needs } : {}),
          ...(step.issueAccess && step.issueAccess !== "none" ? { issue_access: step.issueAccess } : {}),
          agent: { id: step.targetAgentId, ...(step.agentRevision ? { revision: step.agentRevision } : {}) },
          input: {
            instruction: step.prompt,
            ...(step.bindings && step.bindings.length > 0
              ? {
                  bindings: step.bindings.map((binding) => ({
                    name: binding.name,
                    source: binding.source,
                    pointer: binding.pointer,
                  })),
                }
              : {}),
          },
          ...(outputSchemaValue !== undefined ? { output_schema: outputSchemaValue } : {}),
        }
      }),
      ...(resultValue !== undefined ? { result: resultValue } : {}),
    },
    null,
    2,
  )
}

/**
 * Returns the steps with every node's `needs` made explicit — the value
 * {@link effectiveNeeds} computes — so the visual editor works with an
 * unambiguous graph and each edge edit is a plain array operation. A node
 * already carrying explicit `needs` is unchanged; a legacy node that relied on
 * the linear default gains the single edge that default meant.
 */
export function normalizeNeeds(steps: WorkflowStepDraft[]): WorkflowStepDraft[] {
  return steps.map((step, index) => ({ ...step, needs: effectiveNeeds(steps, index) }))
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
        source: typeof record.source === "string" ? record.source : "",
        pointer: typeof record.pointer === "string" ? record.pointer : "",
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
    const parsed = JSON.parse(definition) as {
      nodes?: unknown
      policy?: { max_parallel_nodes?: unknown }
      input_schema?: unknown
      result?: unknown
    }
    if (!Array.isArray(parsed.nodes)) return null
    const limit = parsed.policy?.max_parallel_nodes
    return {
      maxParallelNodes: typeof limit === "number" ? limit : null,
      inputSchema: parsed.input_schema !== undefined ? JSON.stringify(parsed.input_schema) : undefined,
      result: parsed.result !== undefined ? JSON.stringify(parsed.result) : undefined,
      steps: parsed.nodes.map((node): WorkflowStepDraft => {
        const record = typeof node === "object" && node != null ? (node as Record<string, unknown>) : {}
        const agent = typeof record.agent === "object" && record.agent != null ? (record.agent as Record<string, unknown>) : {}
        const input = typeof record.input === "object" && record.input != null ? (record.input as Record<string, unknown>) : {}
        return {
          id: typeof record.id === "string" && record.id.trim() ? record.id : newStepId(),
          type: typeof record.type === "string" && record.type.trim() ? record.type : AGENT_TASK_STEP_TYPE,
          // Absent `needs` is an explicit root ([]), not a form node, so a parsed
          // definition round-trips without the linear default rewriting its graph.
          needs: parseNeeds(record.needs),
          issueAccess: typeof record.issue_access === "string" ? record.issue_access : undefined,
          targetAgentId: typeof agent.id === "string" ? agent.id : "",
          agentRevision: typeof agent.revision === "number" ? agent.revision : undefined,
          prompt: typeof input.instruction === "string" ? input.instruction : "",
          bindings: parseStepBindings(input.bindings),
          outputSchema: record.output_schema !== undefined ? JSON.stringify(record.output_schema) : undefined,
        }
      }),
    }
  } catch {
    return null
  }
}

/** Reads a node's `needs` as a string array, keeping malformed entries (as
 *  empty strings) so server validation surfaces the mistake. An absent `needs`
 *  is a root, represented as an explicit empty array to distinguish it from a
 *  form-created node. */
function parseNeeds(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map((entry) => (typeof entry === "string" ? entry : ""))
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
  const allIds = new Set(steps.map((s) => s.id))
  const preds = transitivePredecessors(steps)
  steps.forEach((step, index) => {
    if (!step.id.trim()) {
      errors.push({ index, message: "This step is missing its id." })
    } else if (seenIds.has(step.id)) {
      errors.push({ index, message: `Step id "${step.id}" is used by more than one step.` })
    }
    seenIds.add(step.id)
    // Each needs edge must name another existing node, and the edges cannot form
    // a cycle -- a node reachable from itself. These only fire for a hand-authored
    // DAG; the linear form derives valid edges from order.
    for (const need of effectiveNeeds(steps, index)) {
      if (need === step.id) {
        errors.push({ index, message: `Step "${step.id}" cannot depend on itself.` })
      } else if (!allIds.has(need)) {
        errors.push({ index, message: `Step "${step.id}" depends on unknown step "${need}".` })
      } else if (preds.get(need)?.has(step.id)) {
        errors.push({ index, message: `Steps "${step.id}" and "${need}" depend on each other.` })
      }
    }
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
    // A binding selects from the run input or a predecessor node's output at a
    // pointer. A node source can only name a transitive predecessor, each name on
    // a step is distinct, and a pointer is empty or begins with "/" -- the same
    // rules the server enforces, checked here so Save stays disabled for a
    // definition the server would reject.
    const predIds = preds.get(step.id) ?? new Set<string>()
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
      if (!binding.source) {
        errors.push({ index, message: `Input binding "${label}" needs a source.` })
      } else if (binding.source !== WORKFLOW_INPUT_SOURCE) {
        const fromStep = parseNodeOutputSource(binding.source)
        if (fromStep === null) {
          errors.push({ index, message: `Input binding "${label}" has an unknown source.` })
        } else if (!predIds.has(fromStep)) {
          errors.push({ index, message: `Input binding "${label}" must read from the workflow input or a step this one depends on.` })
        }
      }
      if (binding.pointer && !binding.pointer.startsWith("/")) {
        errors.push({ index, message: `Input binding "${label}" pointer must be empty or begin with "/".` })
      }
    })
  })
  return errors
}
