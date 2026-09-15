/**
 * A workflow may declare an `input_schema` (the shared JSON Schema subset). When
 * it does, a run must supply input satisfying it, so the Portal generates a form
 * from the schema's top-level properties rather than asking a person to hand-write
 * JSON. These helpers turn the schema into a flat field list and turn the form's
 * values back into the input object the server validates authoritatively.
 */

export type InputFieldType = "string" | "number" | "integer" | "boolean" | "enum" | "json"

export interface InputField {
  name: string
  type: InputFieldType
  required: boolean
  enumValues?: string[]
  description?: string
}

export interface ParsedInputSchema {
  fields: InputField[]
}

/** A form value is a string for text/number/enum/json inputs and a boolean for a
 *  checkbox; the field's type decides how it is coerced into the input object. */
export type InputFormValues = Record<string, string | boolean>

/**
 * Reads a definition's `input_schema` into the field list the run form renders.
 * Returns null when the definition declares no input_schema, so the form is shown
 * only when a run actually takes input. A property whose type is object or array
 * (or otherwise outside the scalar subset) becomes a raw-JSON field.
 */
export function parseInputSchema(definition: string): ParsedInputSchema | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(definition)
  } catch {
    return null
  }
  const schema = (parsed as { input_schema?: unknown } | null)?.input_schema
  if (!schema || typeof schema !== "object") return null
  const obj = schema as Record<string, unknown>
  const props = obj.properties
  if (!props || typeof props !== "object") return { fields: [] }
  const required = new Set(
    Array.isArray(obj.required) ? obj.required.filter((r): r is string => typeof r === "string") : [],
  )
  const fields: InputField[] = Object.entries(props as Record<string, unknown>).map(([name, raw]) => {
    const p = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>
    const description = typeof p.description === "string" ? p.description : undefined
    const req = required.has(name)
    if (Array.isArray(p.enum)) {
      return { name, type: "enum", required: req, enumValues: p.enum.map((v) => String(v)), description }
    }
    switch (typeof p.type === "string" ? p.type : "") {
      case "boolean":
        return { name, type: "boolean", required: req, description }
      case "integer":
        return { name, type: "integer", required: req, description }
      case "number":
        return { name, type: "number", required: req, description }
      case "string":
        return { name, type: "string", required: req, description }
      default:
        return { name, type: "json", required: req, description }
    }
  })
  return { fields }
}

/**
 * Builds the input object to POST from the form's values, coercing each field by
 * its type and collecting user-facing errors. A required field left empty and a
 * malformed number or JSON value are reported; the server still validates the
 * result against the full schema.
 */
export function buildInputValue(
  fields: InputField[],
  values: InputFormValues,
): { value: Record<string, unknown>; errors: string[] } {
  const value: Record<string, unknown> = {}
  const errors: string[] = []
  for (const field of fields) {
    const raw = values[field.name]
    if (field.type === "boolean") {
      value[field.name] = raw === true
      continue
    }
    const text = typeof raw === "string" ? raw.trim() : ""
    if (text === "") {
      if (field.required) errors.push(`"${field.name}" is required.`)
      continue
    }
    if (field.type === "number" || field.type === "integer") {
      const num = Number(text)
      if (!Number.isFinite(num) || (field.type === "integer" && !Number.isInteger(num))) {
        errors.push(`"${field.name}" must be ${field.type === "integer" ? "an integer" : "a number"}.`)
        continue
      }
      value[field.name] = num
      continue
    }
    if (field.type === "json") {
      try {
        value[field.name] = JSON.parse(text)
      } catch {
        errors.push(`"${field.name}" must be valid JSON.`)
      }
      continue
    }
    value[field.name] = text
  }
  return { value, errors }
}
