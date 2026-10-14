import { describe, expect, it } from "vitest"
import type { ApiNeedsMeResponse } from "../../lib/api/types"
import { requestSummary, toNeedsMe, waitingCount } from "./model"

const empty = { items: [], total: 0 }

describe("needs-me model", () => {
  it("counts every group's total, not only the items it returned", () => {
    const api: ApiNeedsMeResponse = {
      attention_issues: empty,
      failed_tasks: {
        items: [
          {
            id: "t_1",
            space_id: "s",
            session_id: null,
            status: "FAILED",
            input: "Summarize the brief",
            output: null,
            created_by: "u",
            created_at: "2026-03-04T10:00:00Z",
            started_at: null,
            ended_at: "2026-03-04T10:05:00Z",
            error_message: "boom",
            failure_class: "model",
          },
        ],
        total: 14,
      },
      awaiting_answer_tasks: empty,
      workflow_requests: empty,
    }
    const needs = toNeedsMe(api)
    expect(waitingCount(needs)).toBe(14)
    expect(needs.failed.items[0]).toMatchObject({ id: "t_1", title: "Summarize the brief", timeAt: "2026-03-04T10:05:00Z" })
  })

  it("reads nothing waiting as zero", () => {
    expect(
      waitingCount(toNeedsMe({ attention_issues: empty, failed_tasks: empty, awaiting_answer_tasks: empty, workflow_requests: empty })),
    ).toBe(0)
  })

  it("summarizes a request by its first line, or its first question", () => {
    expect(requestSummary({ prompt: "\nApprove the draft release notes.\n\nDraft: ...", questions: [] })).toBe(
      "Approve the draft release notes.",
    )
    expect(requestSummary({ prompt: "", questions: [{ question: "Which area should this check cover?" }] })).toBe(
      "Which area should this check cover?",
    )
    expect(requestSummary({ prompt: "", questions: [] })).toBe("")
    expect(requestSummary({ prompt: "x".repeat(200), questions: [] })).toHaveLength(140)
  })
})
