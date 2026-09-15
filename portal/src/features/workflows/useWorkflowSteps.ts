import { useCallback, useMemo, useState } from "react"
import type { Agent } from "../../lib/types"
import {
  newStep,
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
  addStep: () => void
  removeStep: (index: number) => void
  changeStep: (index: number, patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt">>) => void
  addBinding: (stepIndex: number) => void
  removeBinding: (stepIndex: number, bindingIndex: number) => void
  changeBinding: (stepIndex: number, bindingIndex: number, patch: Partial<WorkflowStepBinding>) => void
  toggleAdvanced: () => void
  setDefinitionText: (text: string) => void
  /** Replace the whole state from a definition string already on the wire
   *  (loading a workflow, restoring a revision). */
  hydrate: (definition: string) => void
}

/**
 * Owns the Agent-step form and the advanced JSON view as one state machine
 * with a single source of truth (`steps`), so `WorkflowDetail` and the
 * create modal share one place that decides what Save is allowed to submit.
 */
export function useWorkflowSteps(agents: Agent[]): WorkflowStepsState {
  const [steps, setSteps] = useState<WorkflowStepDraft[]>([])
  const [advanced, setAdvanced] = useState(false)
  const [definitionText, setDefinitionTextRaw] = useState("")
  const [definitionParseError, setDefinitionParseError] = useState<string | null>(null)

  const hydrate = useCallback((definition: string) => {
    const parsed = parseDefinition(definition)
    setSteps(parsed?.steps ?? [])
    setDefinitionTextRaw(definition)
    setDefinitionParseError(parsed ? null : "This workflow's saved definition is not valid JSON.")
    setAdvanced(!parsed)
  }, [])

  const addStep = useCallback(() => {
    setSteps((prev) => [...prev, newStep(agents[0]?.id ?? "")])
  }, [agents])

  const removeStep = useCallback((index: number) => {
    setSteps((prev) => prev.filter((_, i) => i !== index))
  }, [])

  const changeStep = useCallback(
    (index: number, patch: Partial<Pick<WorkflowStepDraft, "targetAgentId" | "prompt">>) => {
      setSteps((prev) => prev.map((step, i) => (i === index ? { ...step, ...patch } : step)))
    },
    [],
  )

  const addBinding = useCallback((stepIndex: number) => {
    setSteps((prev) =>
      prev.map((step, i) =>
        i === stepIndex
          ? { ...step, bindings: [...(step.bindings ?? []), { name: "", source: "", pointer: "" }] }
          : step,
      ),
    )
  }, [])

  const removeBinding = useCallback((stepIndex: number, bindingIndex: number) => {
    setSteps((prev) =>
      prev.map((step, i) => {
        if (i !== stepIndex) return step
        // An empty bindings array becomes undefined so the draft matches a step
        // that never had one -- keeps stepsToDefinition from emitting `bindings`
        // and the round-trip equal.
        const bindings = (step.bindings ?? []).filter((_, j) => j !== bindingIndex)
        return { ...step, bindings: bindings.length > 0 ? bindings : undefined }
      }),
    )
  }, [])

  const changeBinding = useCallback(
    (stepIndex: number, bindingIndex: number, patch: Partial<WorkflowStepBinding>) => {
      setSteps((prev) =>
        prev.map((step, i) =>
          i === stepIndex
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

  const setDefinitionText = useCallback((text: string) => {
    setDefinitionTextRaw(text)
    const parsed = parseDefinition(text)
    if (parsed) {
      setSteps(parsed.steps)
      setDefinitionParseError(null)
    } else {
      setDefinitionParseError("This isn't valid JSON yet.")
    }
  }, [])

  const toggleAdvanced = useCallback(() => {
    if (advanced) {
      // Leaving requires JSON the step form can actually represent -- a
      // parse failure has no step state to hand back.
      const parsed = parseDefinition(definitionText)
      if (!parsed) {
        setDefinitionParseError("Fix the JSON before returning to the step form.")
        return
      }
      setSteps(parsed.steps)
      setDefinitionParseError(null)
      setAdvanced(false)
      return
    }
    setDefinitionTextRaw(stepsToDefinition(steps))
    setDefinitionParseError(null)
    setAdvanced(true)
  }, [advanced, definitionText, steps])

  const errors = useMemo(() => validateSteps(steps, agents), [steps, agents])
  const definition = useMemo(() => stepsToDefinition(steps), [steps])

  return {
    steps,
    definition,
    errors,
    advanced,
    definitionText,
    definitionParseError,
    addStep,
    removeStep,
    changeStep,
    addBinding,
    removeBinding,
    changeBinding,
    toggleAdvanced,
    setDefinitionText,
    hydrate,
  }
}
