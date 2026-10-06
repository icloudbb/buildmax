import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"
import { answerMode, describeResponse, formatQuestionAnswers, scalarValue } from "./request"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

describe("answerMode", () => {
  it("chooses how an answer is entered from the response schema", () => {
    expect(answerMode(null)).toEqual({ kind: "text" })
    expect(answerMode({ type: "boolean" })).toEqual({ kind: "boolean" })
    expect(answerMode({ type: "integer" })).toEqual({ kind: "scalar", type: "integer" })
    const fields = answerMode({ type: "object", properties: { approved: { type: "boolean" } }, required: ["approved"] })
    expect(fields.kind).toBe("fields")
    expect(answerMode({ type: "array", items: { type: "string" } })).toEqual({ kind: "json" })
  })
})

describe("scalarValue", () => {
  it("coerces typed answers and reports bad ones", () => {
    expect(scalarValue("integer", " 3 ", t)).toEqual({ value: 3 })
    expect(scalarValue("integer", "3.5", t).error).toBeTruthy()
    expect(scalarValue("string", "  ", t).error).toBeTruthy()
  })
})

describe("formatQuestionAnswers", () => {
  it("sends one answer bare and pairs several with their questions", () => {
    expect(formatQuestionAnswers([{ question: "Color?" }], ["blue"])).toBe("blue")
    expect(formatQuestionAnswers([{ question: "Color?" }, { question: "Size?" }], ["blue", "large"])).toBe(
      "Color?\nblue\n\nSize?\nlarge",
    )
  })
})

describe("describeResponse", () => {
  it("renders a response for the run record", () => {
    expect(describeResponse(true, t)).toBe("Yes")
    expect(describeResponse("ship it", t)).toBe("ship it")
    expect(describeResponse({ approved: false }, t)).toBe('{"approved":false}')
  })
})
