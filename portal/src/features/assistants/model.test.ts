import { describe, expect, it } from "vitest"
import type { ApiAssistant } from "../../lib/api/types"
import {
  describeAvailability,
  draftFromAssistant,
  draftToDefinition,
  emptyDraft,
  parseFieldList,
  parseOutputSchema,
  schemaProperties,
  workflowResultProperties,
} from "./model"

describe("schemaProperties", () => {
  it("lists an object schema's top-level properties in order", () => {
    expect(schemaProperties({ type: "object", properties: { summary: {}, answer: {} } })).toEqual(["answer", "summary"])
  })

  it("refuses anything but an object schema, as the server does", () => {
    // Releasing is per top-level field, so a result that is not an object has
    // nothing a contract could name.
    expect(schemaProperties({ type: "string" })).toBeNull()
    expect(schemaProperties([])).toBeNull()
    expect(schemaProperties(null)).toBeNull()
  })

  it("reads an object schema with no properties as offering none", () => {
    expect(schemaProperties({ type: "object" })).toEqual([])
  })
})

describe("parseOutputSchema", () => {
  it("names each way a typed schema can be unusable", () => {
    expect(parseOutputSchema("")).toEqual({ error: "An Agent on the roster needs an output schema." })
    expect(parseOutputSchema("{")).toEqual({ error: "The output schema is not valid JSON." })
    expect("error" in parseOutputSchema('{"type":"array"}')).toBe(true)
  })

  it("returns the parsed schema with its releasable candidates", () => {
    expect(parseOutputSchema('{"type":"object","properties":{"days_left":{"type":"integer"}}}')).toEqual({
      schema: { type: "object", properties: { days_left: { type: "integer" } } },
      properties: ["days_left"],
    })
  })
})

describe("workflowResultProperties", () => {
  const definition = (result: unknown) =>
    JSON.stringify({
      schema_version: 1,
      nodes: [
        { id: "lookup", agent: { id: "a_1" }, input: { instruction: "x" } },
        {
          id: "answer",
          agent: { id: "a_2" },
          input: { instruction: "y" },
          output_schema: { type: "object", properties: { reply: { type: "string" }, ticket: { type: "string" } } },
        },
      ],
      result,
    })

  it("reads the properties of the node whose structured output the result selects", () => {
    expect(workflowResultProperties(definition({ source: "node.answer.output", pointer: "/structured" }))).toEqual(["reply", "ticket"])
  })

  it("leaves the decision to the server when it cannot read the shape", () => {
    // The whole envelope, a pointer elsewhere, a node without a schema, no
    // result at all, and malformed JSON are all refusals or unknowns the
    // server explains.
    expect(workflowResultProperties(definition({ source: "node.answer.output" }))).toBeNull()
    expect(workflowResultProperties(definition({ source: "node.answer.output", pointer: "/structured/reply" }))).toBeNull()
    expect(workflowResultProperties(definition({ source: "node.lookup.output", pointer: "/structured" }))).toBeNull()
    expect(workflowResultProperties(definition(undefined))).toBeNull()
    expect(workflowResultProperties("{")).toBeNull()
  })
})

describe("describeAvailability", () => {
  it("gives each automatic pause its own reason", () => {
    expect(describeAvailability("available").tone).toBe("active")
    expect(describeAvailability("paused").label).toBe("Paused")
    expect(describeAvailability("service_account_disabled").reason).toContain("service account")
    expect(describeAvailability("needs_sponsor").reason).toContain("sponsor")
  })

  it("shows an availability a newer server added verbatim", () => {
    expect(describeAvailability("quota_exhausted" as never).label).toBe("quota_exhausted")
  })
})

describe("parseFieldList", () => {
  it("trims, drops blanks, and keeps each name once", () => {
    expect(parseFieldList(" reply, ,ticket,reply ")).toEqual(["reply", "ticket"])
  })
})

describe("draftToDefinition", () => {
  it("omits the service account on create so the server makes one", () => {
    const got = draftToDefinition({ ...emptyDraft(), name: "  HR desk " })
    expect(got).toEqual({
      definition: {
        name: "HR desk",
        description: "",
        instructions: "",
        model: "",
        roster: [],
        readable_files: [],
        audience: "space_members",
      },
    })
  })

  it("requires a name", () => {
    expect(draftToDefinition(emptyDraft())).toEqual({ error: "An assistant needs a name." })
  })

  it("drops releasable fields the edited schema no longer has", () => {
    const got = draftToDefinition({
      ...emptyDraft(),
      name: "Desk",
      roster: [
        {
          kind: "agent",
          id: "a_1",
          schemaText: '{"type":"object","properties":{"reply":{"type":"string"}}}',
          releasable: ["reply", "removed"],
        },
      ],
    })
    expect("definition" in got && got.definition.roster[0]).toEqual({
      kind: "agent",
      id: "a_1",
      output_schema: { type: "object", properties: { reply: { type: "string" } } },
      releasable: ["reply"],
    })
  })

  it("refuses an Agent entry without a usable schema or a chosen id", () => {
    expect(
      draftToDefinition({ ...emptyDraft(), name: "Desk", roster: [{ kind: "agent", id: "a_1", schemaText: "", releasable: [] }] })
    ).toEqual({ error: "An Agent on the roster needs an output schema." })
    expect(
      draftToDefinition({ ...emptyDraft(), name: "Desk", roster: [{ kind: "workflow", id: "", schemaText: "", releasable: [] }] })
    ).toEqual({ error: "Choose the workflow for every roster entry." })
  })

  it("round-trips an assistant's definition, keeping its service account", () => {
    const assistant = {
      name: "Desk",
      description: "d",
      instructions: "i",
      model: "m",
      audience: "all_users",
      roster: [{ kind: "workflow", id: "w_1", releasable: ["reply"] }],
      readable_files: ["ar_1"],
      service_account_id: "u_sa",
    } as unknown as ApiAssistant
    expect(draftToDefinition(draftFromAssistant(assistant))).toEqual({
      definition: {
        name: "Desk",
        description: "d",
        instructions: "i",
        model: "m",
        audience: "all_users",
        roster: [{ kind: "workflow", id: "w_1", releasable: ["reply"] }],
        readable_files: ["ar_1"],
        service_account_id: "u_sa",
      },
    })
  })
})
