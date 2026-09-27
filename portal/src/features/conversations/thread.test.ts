import { describe, expect, it } from "vitest"
import type { ApiConversationMessage, ApiTask } from "../../lib/api/types"
import { buildConversationThread, taskRunFailed, taskRunFinished, taskStatusLabel } from "./thread"

// The tests order entries by a small seed; the wire carries RFC 3339, whose
// lexicographic order over UTC instants is chronological order.
function instant(seed: number): string {
  return new Date(seed * 1000).toISOString()
}

function message(id: string, at: number, role = "user"): ApiConversationMessage {
  return { id, role, content: id, created_at: instant(at) }
}

function task(id: string, at: number, status = "RUNNING"): ApiTask {
  return {
    id,
    space_id: "tm1",
    conversation_id: "conv1",
    session_id: null,
    status,
    input: id,
    output: null,
    created_by: "u1",
    created_at: instant(at),
    started_at: null,
    ended_at: null,
    error_message: null,
  }
}

describe("buildConversationThread", () => {
  it("orders messages and tasks by when each was created", () => {
    const thread = buildConversationThread(
      [message("m1", 100), message("m3", 300)],
      [task("t2", 200)]
    )
    expect(thread.map((e) => (e.kind === "message" ? e.message.id : e.task.id))).toEqual([
      "m1",
      "t2",
      "m3",
    ])
  })

  it("puts a task after a message from the same second", () => {
    const thread = buildConversationThread([message("m1", 100)], [task("t1", 100)])
    expect(thread.map((e) => e.kind)).toEqual(["message", "task"])
  })

  it("keeps the given order among entries of one kind at the same second", () => {
    const thread = buildConversationThread([], [task("t1", 100), task("t2", 100)])
    expect(thread.map((e) => (e.kind === "task" ? e.task.id : ""))).toEqual(["t1", "t2"])
  })

  it("shows a task whose conversation has no messages", () => {
    expect(buildConversationThread([], [task("t1", 100)])).toHaveLength(1)
  })
})

describe("taskRunFinished", () => {
  it("reads both the run status and the lowercase task status", () => {
    expect(taskRunFinished("SUCCEEDED")).toBe(true)
    expect(taskRunFinished("success")).toBe(true)
    expect(taskRunFinished("CANCELED")).toBe(true)
    expect(taskRunFinished("RUNNING")).toBe(false)
    expect(taskRunFinished("PENDING")).toBe(false)
  })

  it("separates a fault from a finish", () => {
    expect(taskRunFailed("FAILED")).toBe(true)
    expect(taskRunFailed("CANCELED")).toBe(true)
    expect(taskRunFailed("SUCCEEDED")).toBe(false)
  })
})

describe("taskStatusLabel", () => {
  it("names a task waiting on the user's answer instead of calling it done", () => {
    expect(taskStatusLabel({ status: "SUCCEEDED", awaiting_answer: true })).toBe("Needs your answer")
    expect(taskStatusLabel({ status: "SUCCEEDED" })).toBe("Done")
    expect(taskStatusLabel({ status: "RUNNING" })).toBe("Running")
  })
})
