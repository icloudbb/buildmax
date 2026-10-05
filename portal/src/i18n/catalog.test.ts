import { describe, expect, it } from "vitest"
import { missingKeys } from "@buildmax/gui"
import { portalMessages, translate } from "."

describe("Portal message catalog", () => {
  it("translates every key into Chinese", () => {
    expect(missingKeys(portalMessages)).toEqual([])
  })

  it("keeps placeholders identical across languages", () => {
    const placeholders = (text: unknown) => JSON.stringify(text).match(/\{\w+\}/g)?.sort() ?? []
    const en = portalMessages.en as Record<string, unknown>
    const zh = portalMessages["zh-CN"] as Record<string, unknown>
    for (const key of Object.keys(en)) {
      expect(placeholders(zh[key]), key).toEqual(placeholders(en[key]))
    }
  })

  it("resolves a key in each language", () => {
    expect(translate("en", "shell.nav.schedules")).toBe("Schedules")
    expect(translate("zh-CN", "shell.nav.schedules")).toBe("定时任务")
  })
})
