import { describe, expect, it } from "vitest"
import { formatRelativeTime, formatTimestamp } from "./dateFormat"

describe("formatRelativeTime", () => {
  const now = new Date(2026, 9, 6, 15, 0)
  const at = (days: number) => new Date(2026, 9, 6 - days, 9, 5).toISOString()

  it("names today and yesterday in English", () => {
    expect(formatRelativeTime(at(0), "en", now)).toMatch(/^Today \S/)
    expect(formatRelativeTime(at(1), "en", now)).toMatch(/^Yesterday \S/)
  })

  it("names today and yesterday in Chinese with a Chinese clock", () => {
    expect(formatRelativeTime(at(0), "zh-CN", now)).toBe("今天 09:05")
    expect(formatRelativeTime(at(1), "zh-CN", now)).toBe("昨天 09:05")
  })

  it("falls back to a full date and time for older instants", () => {
    const older = at(5)
    expect(formatRelativeTime(older, "en", now)).toBe(new Date(older).toLocaleString())
    expect(formatRelativeTime(older, "zh-CN", now)).toBe(formatTimestamp(older, "zh-CN"))
  })
})
