import type { Question, Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import { schemaFields, type InputField } from "./runInput"

/**
 * How a request's answer is entered, decided by its response schema: free text
 * when it has none, Yes/No for a boolean, a generated form for an object with
 * properties, a single typed field for another scalar, and raw JSON otherwise.
 */
export type AnswerMode =
  | { kind: "text" }
  | { kind: "boolean" }
  | { kind: "fields"; fields: InputField[] }
  | { kind: "scalar"; type: "string" | "number" | "integer" }
  | { kind: "json" }

export function answerMode(schema: unknown): AnswerMode {
  if (schema == null || typeof schema !== "object") return { kind: "text" }
  const type = (schema as { type?: unknown }).type
  if (type === "boolean") return { kind: "boolean" }
  if (type === "string" || type === "number" || type === "integer") return { kind: "scalar", type }
  if (type === "object") {
    const parsed = schemaFields(schema)
    if (parsed && parsed.fields.length > 0) return { kind: "fields", fields: parsed.fields }
  }
  return { kind: "json" }
}

/** Turns a scalar field's text into the JSON value to send, or an error. */
export function scalarValue(
  type: "string" | "number" | "integer",
  text: string,
  t: Translate<MessageKey>,
): { value?: unknown; error?: string } {
  const trimmed = text.trim()
  if (trimmed === "") return { error: t("workflows.request.enterAnswer") }
  if (type === "string") return { value: trimmed }
  const num = Number(trimmed)
  if (!Number.isFinite(num) || (type === "integer" && !Number.isInteger(num))) {
    return { error: t(type === "integer" ? "workflows.request.enterWholeNumber" : "workflows.request.enterNumber") }
  }
  return { value: num }
}

/**
 * Formats the answers to an Agent's questions as the text its Task continues
 * with: each question beside its answer, so the model reads which answer goes
 * with which question even when it asked several.
 */
export function formatQuestionAnswers(questions: Question[], answers: string[]): string {
  if (questions.length === 1) return answers[0] ?? ""
  return questions.map((q, i) => `${q.question}\n${answers[i] ?? ""}`).join("\n\n")
}

/** A resolved request's response as one line of text for the run record. */
export function describeResponse(response: unknown, t: Translate<MessageKey>): string {
  if (response === undefined || response === null) return ""
  if (typeof response === "string") return response
  if (typeof response === "boolean") return t(response ? "workflows.request.yes" : "workflows.request.no")
  return JSON.stringify(response)
}
