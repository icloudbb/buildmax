import { describe, expect, it } from "vitest"
import { buildInputValue, parseInputSchema } from "./runInput"

const withSchema = (schema: unknown) =>
  JSON.stringify({ schema_version: 1, input_schema: schema, steps: [] })

describe("parseInputSchema", () => {
  it("returns null when the definition declares no input_schema", () => {
    expect(parseInputSchema(JSON.stringify({ schema_version: 1, steps: [] }))).toBeNull()
  })

  it("returns null for definition text that is not JSON", () => {
    expect(parseInputSchema("not json")).toBeNull()
  })

  it("maps each property to a typed field and marks required ones", () => {
    const parsed = parseInputSchema(
      withSchema({
        type: "object",
        additionalProperties: false,
        properties: {
          topic: { type: "string", description: "what to research" },
          count: { type: "integer" },
          ratio: { type: "number" },
          deep: { type: "boolean" },
          mode: { enum: ["fast", "slow"] },
          extra: { type: "object" },
        },
        required: ["topic", "count"],
      }),
    )
    expect(parsed?.fields).toEqual([
      { name: "topic", type: "string", required: true, description: "what to research" },
      { name: "count", type: "integer", required: true, description: undefined },
      { name: "ratio", type: "number", required: false, description: undefined },
      { name: "deep", type: "boolean", required: false, description: undefined },
      { name: "mode", type: "enum", required: false, enumValues: ["fast", "slow"], description: undefined },
      { name: "extra", type: "json", required: false, description: undefined },
    ])
  })

  it("treats an input_schema without properties as taking no fields", () => {
    expect(parseInputSchema(withSchema({ type: "object" }))?.fields).toEqual([])
  })
})

describe("buildInputValue", () => {
  const fields = parseInputSchema(
    withSchema({
      type: "object",
      additionalProperties: false,
      properties: {
        topic: { type: "string" },
        count: { type: "integer" },
        deep: { type: "boolean" },
        extra: { type: "object" },
      },
      required: ["topic"],
    }),
  )!.fields

  it("coerces each field by type and omits empty optional fields", () => {
    const { value, errors } = buildInputValue(fields, { topic: "markets", count: "3", deep: true, extra: "" })
    expect(errors).toEqual([])
    // A boolean always contributes its checkbox state; an empty optional text or
    // JSON field is omitted rather than sent as an empty string.
    expect(value).toEqual({ topic: "markets", count: 3, deep: true })
  })

  it("reports a missing required field", () => {
    const { errors } = buildInputValue(fields, { topic: "  " })
    expect(errors.some((e) => /"topic" is required/.test(e))).toBe(true)
  })

  it("reports a non-integer number and invalid JSON", () => {
    const { errors } = buildInputValue(fields, { topic: "x", count: "1.5", extra: "{bad" })
    expect(errors.some((e) => /"count" must be an integer/.test(e))).toBe(true)
    expect(errors.some((e) => /"extra" must be valid JSON/.test(e))).toBe(true)
  })

  it("parses a JSON field into a nested value", () => {
    const { value, errors } = buildInputValue(fields, { topic: "x", extra: '{"a":1}' })
    expect(errors).toEqual([])
    // deep is a boolean field left unchecked, so it contributes false.
    expect(value).toEqual({ topic: "x", deep: false, extra: { a: 1 } })
  })
})
