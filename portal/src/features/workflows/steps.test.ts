import { describe, expect, it } from "vitest"
import type { Agent } from "../../lib/types"
import { newStep, parseDefinition, stepsToDefinition, validateSteps, type WorkflowStepDraft } from "./steps"

function agent(id: string): Agent {
  return { id, name: id, revision: 1, createdAt: "1970-01-01T00:00:00Z" }
}

function step(over: Partial<WorkflowStepDraft> = {}): WorkflowStepDraft {
  return { id: "step_1", type: "agent_task", targetAgentId: "a_1", prompt: "Do the thing.", ...over }
}

describe("stepsToDefinition / parseDefinition", () => {
  it("round-trips a step through the wire shape", () => {
    const original = [step()]
    const parsed = parseDefinition(stepsToDefinition(original))
    expect(parsed?.steps).toEqual(original)
  })

  it("says no rather than guessing when the JSON does not parse", () => {
    expect(parseDefinition("{not json")).toBeNull()
  })

  it("says no when there is no steps array at all", () => {
    expect(parseDefinition(JSON.stringify({ notSteps: [] }))).toBeNull()
  })

  it("reads a step's type verbatim rather than defaulting it to agent_task", () => {
    // An unsupported type from hand-edited JSON has to survive parsing so
    // validateSteps can catch it -- silently coercing it here would hide the
    // exact mistake the advanced mode exists to let a reader see.
    const parsed = parseDefinition(
      JSON.stringify({ steps: [{ step_id: "s1", type: "shell_command", target_agent_id: "a_1", prompt: "x" }] }),
    )
    expect(parsed?.steps[0].type).toBe("shell_command")
  })

  it("generates an id for a step whose JSON left it out", () => {
    const parsed = parseDefinition(JSON.stringify({ steps: [{ target_agent_id: "a_1", prompt: "x" }] }))
    expect(parsed?.steps[0].id).toBeTruthy()
  })

  it("round-trips a step's input bindings so advanced JSON does not drop them", () => {
    const original = [
      step({ id: "collect" }),
      step({ id: "summarize", bindings: [{ name: "research", source: "node.collect.output", pointer: "/text" }] }),
    ]
    const parsed = parseDefinition(stepsToDefinition(original))
    expect(parsed?.steps).toEqual(original)
  })

  it("declares the schema version the runtime requires", () => {
    expect(JSON.parse(stepsToDefinition([step()])).schema_version).toBe(1)
  })

  it("emits bindings in the wire snake_case shape only when a step has them", () => {
    expect(stepsToDefinition([step()])).not.toContain("bindings")
    const wire = stepsToDefinition([
      step({ id: "b", bindings: [{ name: "r", source: "node.a.output", pointer: "/text" }] }),
    ])
    expect(wire).toContain(`"source": "node.a.output"`)
    expect(wire).toContain(`"pointer": "/text"`)
  })
})

describe("newStep", () => {
  it("gives every new step a distinct id", () => {
    const a = newStep("a_1")
    const b = newStep("a_1")
    expect(a.id).not.toBe(b.id)
  })

  it("is always an Agent step", () => {
    expect(newStep().type).toBe("agent_task")
  })
})

describe("validateSteps", () => {
  const agents = [agent("a_1"), agent("a_2")]

  it("accepts a well-formed single Agent step", () => {
    expect(validateSteps([step()], agents)).toEqual([])
  })

  it("refuses an empty step list rather than persisting nothing", () => {
    const errors = validateSteps([], agents)
    expect(errors).toEqual([{ index: -1, message: expect.stringContaining("at least one step") }])
  })

  it("rejects a step type the runtime does not execute, from the step form or advanced JSON alike", () => {
    const errors = validateSteps([step({ type: "shell_command" })], agents)
    expect(errors.some((e) => e.index === 0 && e.message.includes("shell_command"))).toBe(true)
  })

  it("catches duplicate step ids", () => {
    const errors = validateSteps([step({ id: "dup" }), step({ id: "dup" })], agents)
    const duplicateErrors = errors.filter((e) => e.message.includes("more than one step"))
    expect(duplicateErrors).toHaveLength(1)
    expect(duplicateErrors[0].index).toBe(1)
  })

  it("requires an agent that actually exists in this space", () => {
    expect(validateSteps([step({ targetAgentId: "" })], agents)[0].message).toContain("Choose an agent")
    expect(validateSteps([step({ targetAgentId: "gone" })], agents)[0].message).toContain("no longer exists")
  })

  it("requires a prompt", () => {
    expect(validateSteps([step({ prompt: "  " })], agents)[0].message).toContain("prompt")
  })

  // The form and the advanced JSON both run this, so these mirror the server's
  // binding rules exactly: an input names a distinct value read from the
  // workflow input or an earlier step's output at a pointer.
  it("accepts a binding to an earlier step output", () => {
    const steps = [
      step({ id: "collect" }),
      step({ id: "summarize", bindings: [{ name: "research", source: "node.collect.output", pointer: "/text" }] }),
    ]
    expect(validateSteps(steps, agents)).toEqual([])
  })

  it("accepts a binding to the workflow input on the first step", () => {
    const steps = [step({ id: "a", bindings: [{ name: "topic", source: "workflow.input", pointer: "/topic" }] })]
    expect(validateSteps(steps, agents)).toEqual([])
  })

  it("refuses a binding to a later step", () => {
    const steps = [
      step({ id: "collect", bindings: [{ name: "x", source: "node.summarize.output", pointer: "" }] }),
      step({ id: "summarize" }),
    ]
    expect(validateSteps(steps, agents).some((e) => e.index === 0 && /earlier step/.test(e.message))).toBe(true)
  })

  it("refuses a binding to the step itself", () => {
    const steps = [step({ id: "a" }), step({ id: "b", bindings: [{ name: "x", source: "node.b.output", pointer: "" }] })]
    expect(validateSteps(steps, agents).some((e) => e.index === 1 && /earlier step/.test(e.message))).toBe(true)
  })

  it("refuses a binding with no name", () => {
    const steps = [step({ id: "a" }), step({ id: "b", bindings: [{ name: "  ", source: "node.a.output", pointer: "" }] })]
    expect(validateSteps(steps, agents).some((e) => e.index === 1 && /needs a name/.test(e.message))).toBe(true)
  })

  it("refuses a binding with no source chosen", () => {
    const steps = [step({ id: "a" }), step({ id: "b", bindings: [{ name: "x", source: "", pointer: "" }] })]
    expect(validateSteps(steps, agents).some((e) => e.index === 1 && /needs a source/.test(e.message))).toBe(true)
  })

  it("refuses a pointer that does not begin with a slash", () => {
    const steps = [step({ id: "a" }), step({ id: "b", bindings: [{ name: "x", source: "node.a.output", pointer: "text" }] })]
    expect(validateSteps(steps, agents).some((e) => e.index === 1 && /pointer must/.test(e.message))).toBe(true)
  })

  it("refuses two bindings sharing a name on one step", () => {
    const steps = [
      step({ id: "a" }),
      step({
        id: "b",
        bindings: [
          { name: "x", source: "node.a.output", pointer: "/text" },
          { name: "x", source: "node.a.output", pointer: "/text" },
        ],
      }),
    ]
    expect(validateSteps(steps, agents).filter((e) => /more than once/.test(e.message))).toHaveLength(1)
  })
})
