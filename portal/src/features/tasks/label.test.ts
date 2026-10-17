import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"
import { originLabel, originRoute, taskLabel } from "./label"

const t: Translate<MessageKey> = (key, values) => translate("en", key, values)
const zh: Translate<MessageKey> = (key, values) => translate("zh-CN", key, values)

describe("taskLabel", () => {
  it("names an Issue's run by the Issue, whatever its generated title", () => {
    expect(taskLabel({ title: "deployment smoke ok", origin: { kind: "issue", id: "iss_1", name: "Fix import failures" } }, t)).toEqual({
      primary: "Fix import failures",
      secondary: "deployment smoke ok",
    })
  })

  it("names a Workflow step's run by the Workflow and the step", () => {
    const origin = { kind: "workflow" as const, id: "wr_1", name: "QA review", step: 2 }
    expect(taskLabel({ title: "deployment smoke ok", origin }, t).primary).toBe("QA review · Step 2")
    expect(taskLabel({ title: "deployment smoke ok", origin }, zh).primary).toBe("QA review · 第 2 步")
  })

  it("names a Schedule's run by the Schedule", () => {
    expect(taskLabel({ title: "Nightly digest", origin: { kind: "schedule", id: "s_1", name: "Nightly digest" } }, t)).toEqual({
      primary: "Nightly digest",
      secondary: null,
    })
  })

  it("keeps the task's own title for a Conversation's task and a direct run", () => {
    expect(taskLabel({ title: "Summarize the brief", origin: { kind: "conversation", id: "c_1", name: "Release chat" } }, t)).toEqual({
      primary: "Summarize the brief",
      secondary: null,
    })
    expect(taskLabel({ title: "Summarize the brief" }, t)).toEqual({ primary: "Summarize the brief", secondary: null })
    expect(taskLabel({ title: " " }, t).primary).toBe("Task")
  })

  it("falls back to the origin's kind when it has no name, never to an ID", () => {
    expect(taskLabel({ title: "x", origin: { kind: "issue", id: "iss_1" } }, t).primary).toBe("Issue")
    expect(originLabel({ kind: "workflow", id: "wr_1", step: 1 }, t)).toBe("Workflow · Step 1")
  })
})

describe("originRoute", () => {
  it("leads to the origin a reader navigates through", () => {
    expect(originRoute({ kind: "workflow", id: "wr_1" }, "sp")).toEqual({ name: "workflowRun", spaceId: "sp", workflowRunId: "wr_1" })
    expect(originRoute({ kind: "issue", id: "iss_1" }, "sp")).toEqual({ name: "issue", spaceId: "sp", issueId: "iss_1" })
    expect(originRoute({ kind: "conversation", id: "c_1" }, "sp")).toEqual({ name: "chat", spaceId: "sp", conversationId: "c_1" })
    expect(originRoute({ kind: "schedule", id: "s_1" }, "sp")).toEqual({ name: "schedules", spaceId: "sp" })
  })
})
