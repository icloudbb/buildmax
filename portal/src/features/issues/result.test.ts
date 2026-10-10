import { describe, expect, it } from "vitest"
import type { IssueOutput, IssueRun, Task, WorkflowNodeRun, WorkflowRun } from "../../lib/types"
import { issueRunInFlight, issueRunProduct, textResults } from "./result"

function agentRun(id: string, status: Task["status"], output: string | null): IssueRun {
  return {
    kind: "agent",
    task: { id, title: id, status, timeAt: "2026-03-04T14:00:00Z", summary: "the input", createdAt: "2026-03-04T14:00:00Z" },
    output,
  }
}

function step(nodeId: string, nodeIndex: number, output: string | null): WorkflowNodeRun {
  return { nodeId, nodeIndex, output, status: "succeeded" } as WorkflowNodeRun
}

function workflowRun(id: string, status: WorkflowRun["status"], steps: WorkflowNodeRun[], result?: unknown): IssueRun {
  return { kind: "workflow", run: { id, status, result } as WorkflowRun, steps }
}

function file(id: string, source: Partial<IssueOutput["source"]>): IssueOutput {
  return { id, title: id, kind: "artifact", artifactId: id, createdAt: "2026-03-04T14:00:00Z", source: { sourceType: "task_run", ...source } }
}

describe("issueRunProduct", () => {
  it("is an Agent run's reply and the files its Task published, never its input", () => {
    const outputs = [file("a1", { taskId: "t1" }), file("a2", { taskId: "t2" })]
    expect(issueRunProduct(agentRun("t1", "success", "deployment smoke ok"), outputs)).toEqual({
      files: [outputs[0]],
      text: { source: "reply", value: "deployment smoke ok" },
    })
    expect(issueRunProduct(agentRun("t2", "running", null), outputs).text).toBeNull()
    expect(issueRunProduct(agentRun("t2", "success", "  "), []).text).toBeNull()
  })

  it("prefers a Workflow's declared result", () => {
    const run = workflowRun("wr1", "succeeded", [step("draft", 0, "a draft")], { approved: true })
    expect(issueRunProduct(run, []).text).toEqual({ source: "workflowResult", value: '{\n  "approved": true\n}' })
  })

  it("falls back to the last step that produced output when no result is declared", () => {
    const run = workflowRun("wr1", "succeeded", [step("review", 1, null), step("draft", 0, "a draft")])
    expect(issueRunProduct(run, []).text).toEqual({ source: "stepOutput", value: "a draft", stepId: "draft" })
  })

  it("counts files a Workflow run's steps published as that run's", () => {
    const outputs = [file("a1", { taskId: "ts", workflowRunId: "wr1" }), file("a2", { taskId: "t1" })]
    expect(issueRunProduct(workflowRun("wr1", "succeeded", []), outputs).files).toEqual([outputs[0]])
    // A step's Task is not an Agent run of the Issue.
    expect(issueRunProduct(agentRun("ts", "success", null), outputs).files).toEqual([])
  })
})

describe("textResults", () => {
  it("lists runs whose only output was text, and every declared result", () => {
    const outputs = [file("a1", { taskId: "t2" })]
    const runs = [
      agentRun("t1", "success", "text only"),
      agentRun("t2", "success", "covered by its file"),
      agentRun("t3", "running", null),
      workflowRun("wr1", "succeeded", [], "declared"),
    ]
    expect(textResults(runs, outputs).map((entry) => entry.text.value)).toEqual(["text only", "declared"])
  })
})

describe("issueRunInFlight", () => {
  it("is true while any run can still change, of either kind", () => {
    expect(issueRunInFlight([agentRun("t1", "pending", null)])).toBe(true)
    expect(issueRunInFlight([agentRun("t1", "success", "ok"), workflowRun("wr1", "canceling", [])])).toBe(true)
  })

  it("is false once every run has finished, or when nothing has run", () => {
    expect(issueRunInFlight([])).toBe(false)
    expect(issueRunInFlight([agentRun("t1", "failed", null), workflowRun("wr1", "succeeded", [])])).toBe(false)
  })
})
