import type { Translate } from "@buildmax/gui"
import type { Agent } from "../../lib/types"
import { translate, type MessageKey } from "../../i18n"

/**
 * `agent_task` is the only step type the runtime executes today, so it is not
 * a user-editable field: it exists on the draft only so a step parsed from
 * advanced-mode JSON can carry -- and validation can reject -- a type this
 * Portal build does not know how to run.
 */
export const AGENT_TASK_STEP_TYPE = "agent_task"

/** A step a person completes: the run waits, holding no worker, until someone
 *  answers its request, and the answer is the step's output. */
export const HUMAN_INPUT_STEP_TYPE = "human_input"

/** The output schema an approval step uses: a Yes/No answer. */
export const YES_NO_SCHEMA = JSON.stringify({ type: "boolean" })

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
  /** The node's `policy.max_attempts` (first attempt included) and
   *  `policy.timeout_seconds` (per attempt). `undefined` is the default: one
   *  attempt, no timeout. */
  maxAttempts?: number
  timeoutSeconds?: number
}

/** Bounds the server enforces on node and run policy, mirrored so Save stays
 *  disabled for a definition publication would refuse. */
export const MAX_NODE_ATTEMPTS = 5
export const MIN_TIMEOUT_SECONDS = 60
export const MAX_TIMEOUT_SECONDS = 30 * 24 * 60 * 60

export interface ParsedWorkflowDefinition {
  steps: WorkflowStepDraft[]
  /** The definition's `policy.max_parallel_nodes`, or null when absent. Carried
   *  through parse and serialize so a hand-authored concurrency limit is not
   *  stripped when the definition round-trips through the step model. */
  maxParallelNodes: number | null
  /** The definition's `policy.timeout_seconds` -- the whole run's deadline --
   *  or null when absent. */
  runTimeoutSeconds: number | null
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

/** A new step's id is generated so it is unique on creation; the inspector lets
 *  the author rename it to something meaningful (it shows on the node and in
 *  binding sources). Uniqueness and non-emptiness are then enforced by
 *  {@link validateSteps} and the rename commit. */
export function newStepId(): string {
  const random =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2)
  return `step_${random.replace(/-/g, "").slice(0, 8)}`
}

/** The starting text is the caller's to translate: it becomes the step's
 *  instruction once saved. */
export function newStep(agentId = "", prompt = translate("en", "workflows.step.defaultPrompt")): WorkflowStepDraft {
  return {
    id: newStepId(),
    type: AGENT_TASK_STEP_TYPE,
    targetAgentId: agentId,
    prompt,
  }
}

export function newHumanStep(prompt = translate("en", "workflows.step.defaultQuestion")): WorkflowStepDraft {
  return {
    id: newStepId(),
    type: HUMAN_INPUT_STEP_TYPE,
    targetAgentId: "",
    prompt,
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

/** The definition-level fields that travel beside the steps. */
export type DefinitionOptions = Partial<Omit<ParsedWorkflowDefinition, "steps">>

/** Builds a `policy` object from the fields that are set, or undefined when
 *  none is, so a definition without policy does not grow an empty one. */
function policyObject(fields: Record<string, number | null | undefined>): Record<string, number> | undefined {
  const entries = Object.entries(fields).filter((entry): entry is [string, number] => typeof entry[1] === "number" && entry[1] > 0)
  return entries.length > 0 ? Object.fromEntries(entries) : undefined
}

export function stepsToDefinition(steps: WorkflowStepDraft[], options: DefinitionOptions = {}): string {
  const inputSchemaValue = embedRawJSON(options.inputSchema)
  const resultValue = embedRawJSON(options.result)
  const policy = policyObject({ max_parallel_nodes: options.maxParallelNodes, timeout_seconds: options.runTimeoutSeconds })
  return JSON.stringify(
    {
      schema_version: WORKFLOW_SCHEMA_VERSION,
      ...(inputSchemaValue !== undefined ? { input_schema: inputSchemaValue } : {}),
      ...(policy ? { policy } : {}),
      nodes: steps.map((step, index) => {
        const needs = effectiveNeeds(steps, index)
        const outputSchemaValue = embedRawJSON(step.outputSchema)
        const nodePolicy = policyObject({ timeout_seconds: step.timeoutSeconds, max_attempts: step.maxAttempts })
        const human = step.type === HUMAN_INPUT_STEP_TYPE
        return {
          id: step.id,
          type: step.type,
          ...(needs.length > 0 ? { needs } : {}),
          ...(step.issueAccess && step.issueAccess !== "none" ? { issue_access: step.issueAccess } : {}),
          // A person's step runs no Agent, so it names none.
          ...(human ? {} : { agent: { id: step.targetAgentId, ...(step.agentRevision ? { revision: step.agentRevision } : {}) } }),
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
          ...(nodePolicy ? { policy: nodePolicy } : {}),
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

/**
 * Returns the steps with `oldId` renamed to `newId` everywhere it is referenced:
 * the node's own id, other nodes' `needs` edges, and binding sources that read
 * its output. Returns the input unchanged when `newId` is empty, equal to
 * `oldId`, or already used by another step, so a rename can never fold two nodes
 * into one id. A node's absent `needs` (a form node) is preserved as absent
 * rather than materialized, so the round-trip is unaffected.
 */
export function renameStepId(steps: WorkflowStepDraft[], oldId: string, newId: string): WorkflowStepDraft[] {
  if (!newId || newId === oldId) return steps
  if (steps.some((step) => step.id === newId)) return steps
  const oldSource = nodeOutputSource(oldId)
  const newSource = nodeOutputSource(newId)
  return steps.map((step) => ({
    ...step,
    id: step.id === oldId ? newId : step.id,
    needs: step.needs === undefined ? undefined : step.needs.map((need) => (need === oldId ? newId : need)),
    bindings: step.bindings?.map((binding) =>
      binding.source === oldSource ? { ...binding, source: newSource } : binding,
    ),
  }))
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
      policy?: { max_parallel_nodes?: unknown; timeout_seconds?: unknown }
      input_schema?: unknown
      result?: unknown
    }
    if (!Array.isArray(parsed.nodes)) return null
    const limit = parsed.policy?.max_parallel_nodes
    const runTimeout = parsed.policy?.timeout_seconds
    return {
      maxParallelNodes: typeof limit === "number" ? limit : null,
      runTimeoutSeconds: typeof runTimeout === "number" ? runTimeout : null,
      inputSchema: parsed.input_schema !== undefined ? JSON.stringify(parsed.input_schema) : undefined,
      result: parsed.result !== undefined ? JSON.stringify(parsed.result) : undefined,
      steps: parsed.nodes.map((node): WorkflowStepDraft => {
        const record = typeof node === "object" && node != null ? (node as Record<string, unknown>) : {}
        const agent = typeof record.agent === "object" && record.agent != null ? (record.agent as Record<string, unknown>) : {}
        const input = typeof record.input === "object" && record.input != null ? (record.input as Record<string, unknown>) : {}
        const policy = typeof record.policy === "object" && record.policy != null ? (record.policy as Record<string, unknown>) : {}
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
          maxAttempts: typeof policy.max_attempts === "number" ? policy.max_attempts : undefined,
          timeoutSeconds: typeof policy.timeout_seconds === "number" ? policy.timeout_seconds : undefined,
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
export function validateSteps(steps: WorkflowStepDraft[], agents: Agent[], t: Translate<MessageKey>): StepError[] {
  const errors: StepError[] = []
  if (steps.length === 0) {
    errors.push({ index: -1, message: t("workflows.validate.noSteps") })
    return errors
  }
  const seenIds = new Set<string>()
  const allIds = new Set(steps.map((s) => s.id))
  const preds = transitivePredecessors(steps)
  steps.forEach((step, index) => {
    if (!step.id.trim()) {
      errors.push({ index, message: t("workflows.validate.missingId") })
    } else if (seenIds.has(step.id)) {
      errors.push({ index, message: t("workflows.validate.duplicateId", { id: step.id }) })
    }
    seenIds.add(step.id)
    // Each needs edge must name another existing node, and the edges cannot form
    // a cycle -- a node reachable from itself. These only fire for a hand-authored
    // DAG; the linear form derives valid edges from order.
    for (const need of effectiveNeeds(steps, index)) {
      if (need === step.id) {
        errors.push({ index, message: t("workflows.validate.selfNeed", { id: step.id }) })
      } else if (!allIds.has(need)) {
        errors.push({ index, message: t("workflows.validate.unknownNeed", { id: step.id, need }) })
      } else if (preds.get(need)?.has(step.id)) {
        errors.push({ index, message: t("workflows.validate.cycle", { id: step.id, need }) })
      }
    }
    if (step.type !== AGENT_TASK_STEP_TYPE && step.type !== HUMAN_INPUT_STEP_TYPE) {
      errors.push({ index, message: t("workflows.validate.unsupportedType", { type: step.type }) })
    }
    if (step.type === HUMAN_INPUT_STEP_TYPE) {
      if (step.targetAgentId) errors.push({ index, message: t("workflows.validate.inputNoAgent") })
      if (step.maxAttempts !== undefined && step.maxAttempts > 1) {
        errors.push({ index, message: t("workflows.validate.inputNoRetry") })
      }
      if (step.issueAccess && step.issueAccess !== "none") {
        errors.push({ index, message: t("workflows.validate.inputNoIssue") })
      }
      if (!step.prompt.trim()) errors.push({ index, message: t("workflows.validate.inputQuestion") })
    } else {
      if (!step.targetAgentId) {
        errors.push({ index, message: t("workflows.validate.chooseAgent") })
      } else if (!agents.some((agent) => agent.id === step.targetAgentId)) {
        errors.push({ index, message: t("workflows.validate.agentGone") })
      }
      if (!step.prompt.trim()) {
        errors.push({ index, message: t("workflows.validate.needsPrompt") })
      }
    }
    if (step.maxAttempts !== undefined && (!Number.isInteger(step.maxAttempts) || step.maxAttempts < 1 || step.maxAttempts > MAX_NODE_ATTEMPTS)) {
      errors.push({ index, message: t("workflows.validate.attempts", { max: MAX_NODE_ATTEMPTS }) })
    }
    if (step.timeoutSeconds !== undefined && !validTimeout(step.timeoutSeconds)) {
      errors.push({ index, message: t("workflows.validate.stepTimeout") })
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
      const label = name || t("workflows.validate.unnamed")
      if (!name) {
        errors.push({ index, message: t("workflows.validate.bindingName") })
      } else if (bindingNames.has(name)) {
        errors.push({ index, message: t("workflows.validate.bindingDuplicate", { name }) })
      }
      bindingNames.add(name)
      if (!binding.source) {
        errors.push({ index, message: t("workflows.validate.bindingSource", { name: label }) })
      } else if (binding.source !== WORKFLOW_INPUT_SOURCE) {
        const fromStep = parseNodeOutputSource(binding.source)
        if (fromStep === null) {
          errors.push({ index, message: t("workflows.validate.bindingUnknownSource", { name: label }) })
        } else if (!predIds.has(fromStep)) {
          errors.push({ index, message: t("workflows.validate.bindingPredecessor", { name: label }) })
        }
      }
      if (binding.pointer && !binding.pointer.startsWith("/")) {
        errors.push({ index, message: t("workflows.validate.bindingPointer", { name: label }) })
      }
    })
  })
  return errors
}

function validTimeout(seconds: number): boolean {
  return Number.isInteger(seconds) && seconds >= MIN_TIMEOUT_SECONDS && seconds <= MAX_TIMEOUT_SECONDS
}

/** Validates the definition-level policy the toolbar edits. Its problems have
 *  no step, so they carry index -1. */
export function validateDefinitionPolicy(options: DefinitionOptions, t: Translate<MessageKey>): StepError[] {
  const errors: StepError[] = []
  if (options.runTimeoutSeconds != null && !validTimeout(options.runTimeoutSeconds)) {
    errors.push({ index: -1, message: t("workflows.validate.runTimeout") })
  }
  return errors
}
