import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"
import { agentFields, buildAgentDefinition } from "./definition"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

describe("buildAgentDefinition", () => {
  it("sends the chosen model", () => {
    const def = buildAgentDefinition({ name: "picker", model: "Fast" }, [], { env: [] })
    expect(def?.model).toBe("Fast")
  })

  it("omits the model when the deployment default is chosen", () => {
    const def = buildAgentDefinition({ name: "picker", model: "" }, [], { env: [] })
    expect(def?.model).toBeUndefined()
  })
})

describe("agentFields", () => {
  it("offers the deployment default first, then the catalog models", () => {
    const field = agentFields(["Fast", "Deep"], t).find((f) => f.key === "model")
    expect(field?.options).toEqual([
      { value: "", label: "Deployment default" },
      { value: "Fast", label: "Fast" },
      { value: "Deep", label: "Deep" },
    ])
  })

  it("offers only the deployment default when the catalog is empty", () => {
    const field = agentFields([], t).find((f) => f.key === "model")
    expect(field?.options).toEqual([{ value: "", label: "Deployment default" }])
  })
})
