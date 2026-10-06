import { useCallback, useMemo, useState } from "react"
import type { Agent } from "../../lib/types"
import { useStableT, useT } from "../../i18n"
import {
  newHumanStep,
  newStep,
  newStepId,
  nodeOutputSource,
  normalizeNeeds,
  parseDefinition,
  renameStepId,
  stepsToDefinition,
  validateDefinitionPolicy,
  validateSteps,
  type StepError,
  type WorkflowStepBinding,
  type WorkflowStepDraft,
} from "./steps"

/** Why the raw JSON cannot be used, as a message key so it follows the
 *  interface language. */
export type DefinitionParseError =
  | "workflows.editor.savedInvalid"
  | "workflows.editor.invalidJson"
  | "workflows.editor.fixJson"

export interface WorkflowStepsState {
  steps: WorkflowStepDraft[]
  /** The definition JSON to submit. Always derived from `steps`, the one
   *  state Save and the runtime both ultimately see, whichever mode is
   *  showing right now. */
  definition: string
  errors: StepError[]
  advanced: boolean
  definitionText: string
  definitionParseError: DefinitionParseError | null
  /** The definition's `policy.max_parallel_nodes`, editable beside the canvas. */
  maxParallelNodes: number | null
  setMaxParallelNodes: (value: number | null) => void
  /** The definition's `policy.timeout_seconds`, the whole run's deadline. */
  runTimeoutSeconds: number | null
  setRunTimeoutSeconds: (value: number | null) => void
  /** Appends a root step (no needs) -- an Agent step, or an input step a person
   *  answers -- and returns its id so the caller can select and position it. */
  addStep: (kind?: "agent" | "human") => string
  removeStep: (id: string) => void
  /** Renames a step's id, rewriting every reference to it — other steps' `needs`
   *  edges, binding sources that read its output, and the definition's `result`
   *  selector — so the graph stays consistent. A no-op when the new id is empty,
   *  unchanged, or already taken. */
  renameStep: (oldId: string, newId: string) => void
  changeStep: (
    id: string,
    patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt" | "issueAccess" | "maxAttempts" | "timeoutSeconds" | "outputSchema">>,
  ) => void
  /** Add or remove a `needs` edge — the graph's dependency between two steps. */
  connectNeed: (targetId: string, sourceId: string) => void
  disconnectNeed: (targetId: string, sourceId: string) => void
  addBinding: (stepId: string) => void
  removeBinding: (stepId: string, bindingIndex: number) => void
  changeBinding: (stepId: string, bindingIndex: number, patch: Partial<WorkflowStepBinding>) => void
  toggleAdvanced: () => void
  setDefinitionText: (text: string) => void
  /** Replace the whole state from a definition string already on the wire
   *  (loading a workflow, restoring a revision). */
  hydrate: (definition: string) => void
}

/**
 * Owns the visual editor and the raw JSON view as one state machine with a
 * single source of truth (`steps`, with explicit `needs` edges), so
 * `WorkflowDetail` and the create modal share one place that decides what Save
 * is allowed to submit. `input_schema`, `result`, and each node's
 * `output_schema` are carried verbatim so switching to the visual editor and
 * saving does not strip a definition that declares them.
 */
export function useWorkflowSteps(agents: Agent[]): WorkflowStepsState {
  const [steps, setSteps] = useState<WorkflowStepDraft[]>([])
  const [maxParallelNodes, setMaxParallelNodesRaw] = useState<number | null>(null)
  const [runTimeoutSeconds, setRunTimeoutSecondsRaw] = useState<number | null>(null)
  const [inputSchema, setInputSchema] = useState<string | undefined>(undefined)
  const [result, setResult] = useState<string | undefined>(undefined)
  const [advanced, setAdvanced] = useState(false)
  const [definitionText, setDefinitionTextRaw] = useState("")
  const [definitionParseError, setDefinitionParseError] = useState<DefinitionParseError | null>(null)
  const t = useT()
  const stableT = useStableT()

  const hydrate = useCallback((definition: string) => {
    const parsed = parseDefinition(definition)
    setSteps(parsed ? normalizeNeeds(parsed.steps) : [])
    setMaxParallelNodesRaw(parsed?.maxParallelNodes ?? null)
    setRunTimeoutSecondsRaw(parsed?.runTimeoutSeconds ?? null)
    setInputSchema(parsed?.inputSchema)
    setResult(parsed?.result)
    setDefinitionTextRaw(definition)
    setDefinitionParseError(parsed ? null : "workflows.editor.savedInvalid")
    setAdvanced(!parsed)
  }, [])

  const addStep = useCallback((kind: "agent" | "human" = "agent") => {
    const base =
      kind === "human"
        ? newHumanStep(stableT("workflows.step.defaultQuestion"))
        : newStep(agents[0]?.id ?? "", stableT("workflows.step.defaultPrompt"))
    const step: WorkflowStepDraft = { ...base, id: newStepId(), needs: [] }
    setSteps((prev) => [...prev, step])
    return step.id
  }, [agents, stableT])

  const removeStep = useCallback((id: string) => {
    setSteps((prev) =>
      prev
        .filter((step) => step.id !== id)
        // Drop any edge that pointed at the removed step, so no node is left
        // depending on a step that no longer exists.
        .map((step) => ({ ...step, needs: (step.needs ?? []).filter((need) => need !== id) })),
    )
  }, [])

  const changeStep = useCallback(
    (id: string, patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt" | "issueAccess" | "maxAttempts" | "timeoutSeconds" | "outputSchema">>) => {
      setSteps((prev) => prev.map((step) => (step.id === id ? { ...step, ...patch } : step)))
    },
    [],
  )

  const renameStep = useCallback((oldId: string, newId: string) => {
    if (!newId || newId === oldId) return
    const oldSource = nodeOutputSource(oldId)
    const newSource = nodeOutputSource(newId)
    setSteps((prev) => renameStepId(prev, oldId, newId))
    // `result` is carried verbatim from raw JSON; if it selects the renamed
    // node's output, rewrite the source so the round-trip stays consistent.
    setResult((prev) => {
      if (prev === undefined) return prev
      try {
        const parsed = JSON.parse(prev) as { source?: unknown }
        if (parsed && typeof parsed === "object" && parsed.source === oldSource) {
          return JSON.stringify({ ...parsed, source: newSource })
        }
      } catch {
        // Leave malformed result text untouched.
      }
      return prev
    })
  }, [])

  const connectNeed = useCallback((targetId: string, sourceId: string) => {
    if (targetId === sourceId) return
    setSteps((prev) =>
      prev.map((step) => {
        if (step.id !== targetId) return step
        const needs = step.needs ?? []
        return needs.includes(sourceId) ? step : { ...step, needs: [...needs, sourceId] }
      }),
    )
  }, [])

  const disconnectNeed = useCallback((targetId: string, sourceId: string) => {
    setSteps((prev) =>
      prev.map((step) =>
        step.id === targetId ? { ...step, needs: (step.needs ?? []).filter((need) => need !== sourceId) } : step,
      ),
    )
  }, [])

  const addBinding = useCallback((stepId: string) => {
    setSteps((prev) =>
      prev.map((step) =>
        step.id === stepId
          ? { ...step, bindings: [...(step.bindings ?? []), { name: "", source: "", pointer: "" }] }
          : step,
      ),
    )
  }, [])

  const removeBinding = useCallback((stepId: string, bindingIndex: number) => {
    setSteps((prev) =>
      prev.map((step) => {
        if (step.id !== stepId) return step
        // An empty bindings array becomes undefined so the draft matches a step
        // that never had one -- keeps stepsToDefinition from emitting `bindings`
        // and the round-trip equal.
        const bindings = (step.bindings ?? []).filter((_, j) => j !== bindingIndex)
        return { ...step, bindings: bindings.length > 0 ? bindings : undefined }
      }),
    )
  }, [])

  const changeBinding = useCallback(
    (stepId: string, bindingIndex: number, patch: Partial<WorkflowStepBinding>) => {
      setSteps((prev) =>
        prev.map((step) =>
          step.id === stepId
            ? {
                ...step,
                bindings: (step.bindings ?? []).map((binding, j) =>
                  j === bindingIndex ? { ...binding, ...patch } : binding,
                ),
              }
            : step,
        ),
      )
    },
    [],
  )

  const setMaxParallelNodes = useCallback((value: number | null) => {
    setMaxParallelNodesRaw(value)
  }, [])

  const setRunTimeoutSeconds = useCallback((value: number | null) => {
    setRunTimeoutSecondsRaw(value)
  }, [])

  const setDefinitionText = useCallback((text: string) => {
    setDefinitionTextRaw(text)
    const parsed = parseDefinition(text)
    if (parsed) {
      setSteps(normalizeNeeds(parsed.steps))
      setMaxParallelNodesRaw(parsed.maxParallelNodes)
      setRunTimeoutSecondsRaw(parsed.runTimeoutSeconds)
      setInputSchema(parsed.inputSchema)
      setResult(parsed.result)
      setDefinitionParseError(null)
    } else {
      setDefinitionParseError("workflows.editor.invalidJson")
    }
  }, [])

  const toggleAdvanced = useCallback(() => {
    if (advanced) {
      // Leaving requires JSON the visual editor can actually represent -- a
      // parse failure has no step state to hand back.
      const parsed = parseDefinition(definitionText)
      if (!parsed) {
        setDefinitionParseError("workflows.editor.fixJson")
        return
      }
      setSteps(normalizeNeeds(parsed.steps))
      setMaxParallelNodesRaw(parsed.maxParallelNodes)
      setRunTimeoutSecondsRaw(parsed.runTimeoutSeconds)
      setInputSchema(parsed.inputSchema)
      setResult(parsed.result)
      setDefinitionParseError(null)
      setAdvanced(false)
      return
    }
    setDefinitionTextRaw(stepsToDefinition(steps, { maxParallelNodes, runTimeoutSeconds, inputSchema, result }))
    setDefinitionParseError(null)
    setAdvanced(true)
  }, [advanced, definitionText, steps, maxParallelNodes, runTimeoutSeconds, inputSchema, result])

  const errors = useMemo(
    () => [...validateDefinitionPolicy({ runTimeoutSeconds }, t), ...validateSteps(steps, agents, t)],
    [steps, agents, runTimeoutSeconds, t],
  )
  const definition = useMemo(
    () => stepsToDefinition(steps, { maxParallelNodes, runTimeoutSeconds, inputSchema, result }),
    [steps, maxParallelNodes, runTimeoutSeconds, inputSchema, result],
  )

  return {
    steps,
    definition,
    errors,
    advanced,
    definitionText,
    definitionParseError,
    maxParallelNodes,
    setMaxParallelNodes,
    runTimeoutSeconds,
    setRunTimeoutSeconds,
    addStep,
    removeStep,
    renameStep,
    changeStep,
    connectNeed,
    disconnectNeed,
    addBinding,
    removeBinding,
    changeBinding,
    toggleAdvanced,
    setDefinitionText,
    hydrate,
  }
}
