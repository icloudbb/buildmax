import { describe, expect, it } from "vitest"
import { missingKeys } from "@buildmax/gui"
import { portalMessages, translate } from "."

describe("Portal message catalog", () => {
  it("translates every key into Chinese", () => {
    expect(missingKeys(portalMessages)).toEqual([])
  })

  it("keeps placeholders identical across languages", () => {
    const placeholders = (text: unknown) => [...new Set(JSON.stringify(text ?? "").match(/\{\w+\}/g))].sort()
    const en = portalMessages.en as Record<string, unknown>
    const zh = portalMessages["zh-CN"] as Record<string, unknown>
    for (const key of Object.keys(en)) {
      expect(placeholders(zh[key]), key).toEqual(placeholders(en[key]))
    }
  })

  // A count of one reads in the singular, noun and verb alike: "1 tasks" and
  // "1 secret grant no longer resolve" were both shipped once.
  it("agrees a count of one with its noun and verb", () => {
    expect(translate("en", "issues.runs.count", { count: 1 })).toBe("1 run")
    expect(translate("en", "agents.detail.secretWarning", { count: 1 })).toBe("1 secret grant no longer resolves.")
    expect(translate("en", "agents.card.secretWarning", { count: 1 })).toBe(
      "1 secret grant no longer resolves. Open the agent to fix.",
    )
    expect(translate("en", "agents.detail.secretWarning", { count: 2 })).toBe("2 secret grants no longer resolve.")
    expect(translate("en", "files.upload.done", { count: 1 })).toBe("Uploaded 1 file")
    expect(translate("en", "files.upload.done", { count: 3 })).toBe("Uploaded 3 files")
  })

  it("resolves a key in each language", () => {
    expect(translate("en", "shell.nav.schedules")).toBe("Schedules")
    expect(translate("zh-CN", "shell.nav.schedules")).toBe("定时任务")
  })
})
