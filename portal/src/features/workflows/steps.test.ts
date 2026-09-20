import { describe, expect, it } from "vitest"
import type { Agent } from "../../lib/types"
import {
  newStep,
  normalizeNeeds,
  parseDefinition,
  renameStepId,
  stepsToDefinition,
  validateSteps,
  type WorkflowStepDraft,
} from "./steps"

function agent(id: string): Agent {
  return { id, name: id, revision: 1, createdAt: "1970-01-01T00:00:00Z" }
}

function step(over: Partial<WorkflowStepDraft> = {}): WorkflowStepDraft {
  return { id: "step_1", type: "agent_task", targetAgentId: "a_1", prompt: "Do the thing.", ...over }
}

describe("stepsToDefinition / parseDefinition", () => {
  it("round-trips a step through the wire shape, making the linear chain explicit", () => {
    // A form-authored step carries no needs; serialization derives the linear
    // chain, so it comes back as an explicit root ([]).
    const parsed = parseDefinition(stepsToDefinition([step()]))
    expect(parsed?.steps).toEqual([{ ...step(), needs: [] }])
  })

  it("says no rather than guessing when the JSON does not parse", () => {
    expect(parseDefinition("{not json")).toBeNull()
  })

  it("says no when there is no nodes array at all", () => {
    expect(parseDefinition(JSON.stringify({ notNodes: [] }))).toBeNull()
  })

  it("reads a node's type verbatim rather than defaulting it to agent_task", () => {
    // An unsupported type from hand-edited JSON has to survive parsing so
    // validateSteps can catch it -- silently coercing it here would hide the
    // exact mistake the advanced mode exists to let a reader see.
    const parsed = parseDefinition(
      JSON.stringify({ nodes: [{ id: "s1", type: "shell_command", target_agent_id: "a_1", prompt: "x" }] }),
    )
    expect(parsed?.steps[0].type).toBe("shell_command")
  })

  it("generates an id for a node whose JSON left it out", () => {
    const parsed = parseDefinition(JSON.stringify({ nodes: [{ target_agent_id: "a_1", prompt: "x" }] }))
    expect(parsed?.steps[0].id).toBeTruthy()
  })

  it("round-trips a node's needs and bindings so advanced JSON does not drop them", () => {
    const original = [
      step({ id: "collect", needs: [] }),
      step({
        id: "summarize",
        needs: ["collect"],
        bindings: [{ name: "research", source: "node.collect.output", pointer: "/text" }],
      }),
    ]
    const parsed = parseDefinition(stepsToDefinition(original))
    expect(parsed?.steps).toEqual(original)
  })

  it("emits needs edges in the wire shape for a linear chain", () => {
    const wire = JSON.parse(stepsToDefinition([step({ id: "a" }), step({ id: "b" })]))
    expect(wire.nodes[0].needs).toBeUndefined()
    expect(wire.nodes[1].needs).toEqual(["a"])
  })

  it("declares the schema version the runtime requires", () => {
    expect(JSON.parse(stepsToDefinition([step()])).schema_version).toBe(1)
  })

  it("emits the nested agent and input wire shape", () => {
    const node = JSON.parse(stepsToDefinition([step({ id: "a", targetAgentId: "agt_1", prompt: "Do it." })])).nodes[0]
    expect(node.agent).toEqual({ id: "agt_1" })
    expect(node.input).toEqual({ instruction: "Do it." })
    expect(node).not.toHaveProperty("target_agent_id")
    expect(node).not.toHaveProperty("prompt")
  })

  it("carries a pinned agent.revision through parse and serialize", () => {
    expect(JSON.parse(stepsToDefinition([step()])).nodes[0].agent).toEqual({ id: "a_1" })
    const wire = stepsToDefinition([step({ agentRevision: 3 })])
    expect(JSON.parse(wire).nodes[0].agent).toEqual({ id: "a_1", revision: 3 })
    expect(parseDefinition(wire)?.steps[0].agentRevision).toBe(3)
  })

  it("carries issue_access through parse and serialize", () => {
    expect(stepsToDefinition([step()])).not.toContain("issue_access")
    const wire = stepsToDefinition([step({ issueAccess: "required" })])
    expect(JSON.parse(wire).nodes[0].issue_access).toBe("required")
    expect(parseDefinition(wire)?.steps[0].issueAccess).toBe("required")
  })

  it("nests a node's bindings under input", () => {
    const wire = JSON.parse(
      stepsToDefinition([
        step({ id: "a" }),
        step({ id: "b", bindings: [{ name: "r", source: "node.a.output", pointer: "/text" }] }),
      ]),
    )
    expect(wire.nodes[1].input.bindings).toEqual([{ name: "r", source: "node.a.output", pointer: "/text" }])
  })

  it("carries policy.max_parallel_nodes through parse and serialize", () => {
    expect(stepsToDefinition([step()])).not.toContain("policy")
    const wire = stepsToDefinition([step()], 3)
    expect(JSON.parse(wire).policy).toEqual({ max_parallel_nodes: 3 })
    expect(parseDefinition(wire)?.maxParallelNodes).toBe(3)
    expect(parseDefinition(stepsToDefinition([step()]))?.maxParallelNodes).toBeNull()
  })

  it("emits bindings in the wire snake_case shape only when a step has them", () => {
    expect(stepsToDefinition([step()])).not.toContain("bindings")
    const wire = stepsToDefinition([
      step({ id: "b", bindings: [{ name: "r", source: "node.a.output", pointer: "/text" }] }),
    ])
    expect(wire).toContain(`"source": "node.a.output"`)
    expect(wire).toContain(`"pointer": "/text"`)
  })

  it("carries input_schema through parse and serialize, only when present", () => {
    expect(stepsToDefinition([step()])).not.toContain("input_schema")
    const schema = { type: "object", properties: { topic: { type: "string" } } }
    const wire = stepsToDefinition([step()], null, JSON.stringify(schema))
    expect(JSON.parse(wire).input_schema).toEqual(schema)
    expect(JSON.parse(parseDefinition(wire)!.inputSchema!)).toEqual(schema)
  })

  it("carries result through parse and serialize, only when present", () => {
    expect(stepsToDefinition([step()])).not.toContain(`"result"`)
    const result = { source: "node.step_1.output", pointer: "/text" }
    const wire = stepsToDefinition([step()], null, undefined, JSON.stringify(result))
    expect(JSON.parse(wire).result).toEqual(result)
    expect(JSON.parse(parseDefinition(wire)!.result!)).toEqual(result)
  })

  it("carries a node's output_schema through parse and serialize, only when present", () => {
    expect(stepsToDefinition([step()])).not.toContain("output_schema")
    const outputSchema = { type: "object", properties: { done: { type: "boolean" } } }
    const wire = stepsToDefinition([step({ outputSchema: JSON.stringify(outputSchema) })])
    expect(JSON.parse(wire).nodes[0].output_schema).toEqual(outputSchema)
    expect(JSON.parse(parseDefinition(wire)!.steps[0].outputSchema!)).toEqual(outputSchema)
  })
})

describe("normalizeNeeds", () => {
  it("makes a form node's linear need explicit and leaves a root empty", () => {
    const normalized = normalizeNeeds([step({ id: "a" }), step({ id: "b" })])
    expect(normalized[0].needs).toEqual([])
    expect(normalized[1].needs).toEqual(["a"])
  })

  it("leaves an already-explicit graph unchanged", () => {
    const graph = [step({ id: "a", needs: [] }), step({ id: "b", needs: [] }), step({ id: "c", needs: ["a", "b"] })]
    expect(normalizeNeeds(graph)).toEqual(graph)
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
    expect(validateSteps(steps, agents).some((e) => e.index === 0 && /depends on/.test(e.message))).toBe(true)
  })

  it("refuses a binding to the step itself", () => {
    const steps = [step({ id: "a" }), step({ id: "b", bindings: [{ name: "x", source: "node.b.output", pointer: "" }] })]
    expect(validateSteps(steps, agents).some((e) => e.index === 1 && /depends on/.test(e.message))).toBe(true)
  })

  it("refuses a binding to an existing node that is not a predecessor", () => {
    // Two explicit roots: b reads a's output but does not depend on it.
    const steps = [
      step({ id: "a", needs: [] }),
      step({ id: "b", needs: [], bindings: [{ name: "x", source: "node.a.output", pointer: "" }] }),
    ]
    expect(validateSteps(steps, agents).some((e) => e.index === 1 && /depends on/.test(e.message))).toBe(true)
  })

  it("accepts a binding to a transitive predecessor across a fan-in", () => {
    // report depends on analyze, which depends on collect; report may read collect.
    const steps = [
      step({ id: "collect", needs: [] }),
      step({ id: "analyze", needs: ["collect"] }),
      step({ id: "report", needs: ["analyze"], bindings: [{ name: "r", source: "node.collect.output", pointer: "/text" }] }),
    ]
    expect(validateSteps(steps, agents)).toEqual([])
  })

  it("rejects a needs cycle", () => {
    const steps = [
      step({ id: "a", needs: ["b"] }),
      step({ id: "b", needs: ["a"] }),
    ]
    expect(validateSteps(steps, agents).some((e) => /depend on each other/.test(e.message))).toBe(true)
  })

  it("rejects a needs edge to an unknown node", () => {
    const steps = [step({ id: "a", needs: ["ghost"] })]
    expect(validateSteps(steps, agents).some((e) => /unknown step/.test(e.message))).toBe(true)
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

describe("renameStepId", () => {
  it("rewrites the node id, dependent needs edges, and binding sources", () => {
    const steps = [
      step({ id: "collect", needs: [] }),
      step({
        id: "analyze",
        needs: ["collect"],
        bindings: [{ name: "research", source: "node.collect.output", pointer: "/text" }],
      }),
      step({ id: "draft", needs: ["collect"] }),
    ]
    const renamed = renameStepId(steps, "collect", "gather")
    expect(renamed[0].id).toBe("gather")
    expect(renamed[1].needs).toEqual(["gather"])
    expect(renamed[1].bindings?.[0].source).toBe("node.gather.output")
    expect(renamed[2].needs).toEqual(["gather"])
  })

  it("leaves an unrelated binding source untouched", () => {
    const steps = [
      step({ id: "a", needs: [] }),
      step({ id: "b", needs: ["a"], bindings: [{ name: "x", source: "node.a.output", pointer: "" }] }),
    ]
    // Renaming "b" (nothing depends on it) must not touch the binding that reads a.
    const renamed = renameStepId(steps, "b", "c")
    expect(renamed[1].id).toBe("c")
    expect(renamed[1].bindings?.[0].source).toBe("node.a.output")
  })

  it("refuses to rename onto an id another step already uses", () => {
    const steps = [step({ id: "a", needs: [] }), step({ id: "b", needs: ["a"] })]
    expect(renameStepId(steps, "b", "a")).toBe(steps)
  })

  it("is a no-op for an empty or unchanged id", () => {
    const steps = [step({ id: "a", needs: [] })]
    expect(renameStepId(steps, "a", "")).toBe(steps)
    expect(renameStepId(steps, "a", "a")).toBe(steps)
  })

  it("preserves an absent needs rather than materializing it", () => {
    const steps = [step({ id: "a" }), step({ id: "b" })]
    const renamed = renameStepId(steps, "a", "z")
    expect(renamed[0].needs).toBeUndefined()
    expect(renamed[1].needs).toBeUndefined()
  })
})
