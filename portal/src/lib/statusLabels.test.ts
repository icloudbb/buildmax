import { describe, expect, it } from "vitest"
import { conversationSourceLabel } from "./statusLabels"

describe("conversationSourceLabel", () => {
  it.each([
    ["portal", null],
    ["", null],
    ["telegram", "Telegram"],
    ["webhook", "Webhook"],
  ])("labels %j as %j", (channel, want) => {
    expect(conversationSourceLabel(channel)).toBe(want)
  })
})
