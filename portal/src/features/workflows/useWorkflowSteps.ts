import { useCallback, useMemo, useState } from "react"
import type { Agent } from "../../lib/types"
import {
  newStep,
  newStepId,
  normalizeNeeds,
  parseDefinition,
  stepsToDefinition,
  validateSteps,
  type StepError,
  type WorkflowStepBinding,
  type WorkflowStepDraft,
} from "./steps"

export interface WorkflowStepsState {
  steps: WorkflowStepDraft[]
  /** The definition JSON to submit. Always derived from `steps`, the one
   *  state Save and the runtime both ultimately see, whichever mode is
   *  showing right now. */
  definition: string
  errors: StepError[]
  advanced: boolean
  definitionText: string
  definitionParseError: string | null
  /** The definition's `policy.max_parallel_nodes`, editable beside the canvas. */
  maxParallelNodes: number | null
  setMaxParallelNodes: (value: number | null) => void
  /** Appends a root step (no needs) and returns its id so the caller can select
   *  and position it. */
  addStep: () => string
  removeStep: (id: string) => void
  changeStep: (
    id: string,
    patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt" | "issueAccess">>,
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
  const [inputSchema, setInputSchema] = useState<string | undefined>(undefined)
  const [result, setResult] = useState<string | undefined>(undefined)
  const [advanced, setAdvanced] = useState(false)
  const [definitionText, setDefinitionTextRaw] = useState("")
  const [definitionParseError, setDefinitionParseError] = useState<string | null>(null)

  const hydrate = useCallback((definition: string) => {
    const parsed = parseDefinition(definition)
    setSteps(parsed ? normalizeNeeds(parsed.steps) : [])
    setMaxParallelNodesRaw(parsed?.maxParallelNodes ?? null)
    setInputSchema(parsed?.inputSchema)
    setResult(parsed?.result)
    setDefinitionTextRaw(definition)
    setDefinitionParseError(parsed ? null : "This workflow's saved definition is not valid JSON.")
    setAdvanced(!parsed)
  }, [])

  const addStep = useCallback(() => {
    const step: WorkflowStepDraft = { ...newStep(agents[0]?.id ?? ""), id: newStepId(), needs: [] }
    setSteps((prev) => [...prev, step])
    return step.id
  }, [agents])

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
    (id: string, patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt" | "issueAccess">>) => {
      setSteps((prev) => prev.map((step) => (step.id === id ? { ...step, ...patch } : step)))
    },
    [],
  )

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

  const setDefinitionText = useCallback((text: string) => {
    setDefinitionTextRaw(text)
    const parsed = parseDefinition(text)
    if (parsed) {
      setSteps(normalizeNeeds(parsed.steps))
      setMaxParallelNodesRaw(parsed.maxParallelNodes)
      setInputSchema(parsed.inputSchema)
      setResult(parsed.result)
      setDefinitionParseError(null)
    } else {
      setDefinitionParseError("This isn't valid JSON yet.")
    }
  }, [])

  const toggleAdvanced = useCallback(() => {
    if (advanced) {
      // Leaving requires JSON the visual editor can actually represent -- a
      // parse failure has no step state to hand back.
      const parsed = parseDefinition(definitionText)
      if (!parsed) {
        setDefinitionParseError("Fix the JSON before returning to the visual editor.")
        return
      }
      setSteps(normalizeNeeds(parsed.steps))
      setMaxParallelNodesRaw(parsed.maxParallelNodes)
      setInputSchema(parsed.inputSchema)
      setResult(parsed.result)
      setDefinitionParseError(null)
      setAdvanced(false)
      return
    }
    setDefinitionTextRaw(stepsToDefinition(steps, maxParallelNodes, inputSchema, result))
    setDefinitionParseError(null)
    setAdvanced(true)
  }, [advanced, definitionText, steps, maxParallelNodes, inputSchema, result])

  const errors = useMemo(() => validateSteps(steps, agents), [steps, agents])
  const definition = useMemo(
    () => stepsToDefinition(steps, maxParallelNodes, inputSchema, result),
    [steps, maxParallelNodes, inputSchema, result],
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
    addStep,
    removeStep,
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
