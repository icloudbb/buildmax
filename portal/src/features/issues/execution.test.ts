import { describe, expect, it } from "vitest"
import { executorHint, issueRunAction, issueRunInFlight, type ExecutorChoices } from "./execution"

const empty: ExecutorChoices = { agentCount: 0, publishedWorkflowCount: 0, manage: "allowed" }

describe("executorHint", () => {
  it("names both kinds of executor to someone who may assign either", () => {
    expect(executorHint({ ...empty, agentCount: 1 })).toBe("agentOrWorkflow")
    expect(executorHint({ ...empty, publishedWorkflowCount: 1 })).toBe("agentOrWorkflow")
  })

  it("leads an owner of an empty Space to create an Agent", () => {
    expect(executorHint(empty)).toBe("createAgent")
  })

  it("tells a member who may not create an Agent whom to ask", () => {
    expect(executorHint({ ...empty, manage: "denied" })).toBe("askForAgent")
    // A published Workflow does not help someone who may not assign one.
    expect(executorHint({ ...empty, publishedWorkflowCount: 2, manage: "denied" })).toBe("askForAgent")
    expect(executorHint({ ...empty, agentCount: 1, manage: "denied" })).toBe("agentOnly")
  })

  it("never treats an unknown or failed role lookup as a refusal", () => {
    expect(executorHint({ ...empty, manage: "unknown" })).toBe("nothingYet")
    expect(executorHint({ ...empty, manage: "failed" })).toBe("nothingYet")
  })
})

describe("issueRunAction", () => {
  const none = { executorKind: null, executorId: null }

  it("offers Run for an assigned Agent that still exists", () => {
    const issue = { executorKind: "agent" as const, executorId: "agt_1" }
    expect(issueRunAction({ issue, agentExists: true, choices: empty })).toEqual({ kind: "agent", blocked: null })
    expect(issueRunAction({ issue, agentExists: false, choices: empty })).toEqual({ kind: "agent", blocked: "agentMissing" })
  })

  it("blocks a Workflow until it is known to be published", () => {
    const issue = { executorKind: "workflow" as const, executorId: "wfl_1" }
    expect(issueRunAction({ issue, agentExists: false, workflowStatus: "published", choices: empty })).toEqual({
      kind: "workflow",
      blocked: null,
    })
    expect(issueRunAction({ issue, agentExists: false, workflowStatus: "draft", choices: empty })).toEqual({
      kind: "workflow",
      blocked: "workflowUnpublished",
    })
    expect(issueRunAction({ issue, agentExists: false, choices: empty })).toEqual({
      kind: "workflow",
      blocked: "workflowUnpublished",
    })
  })

  it("names the next step when the Issue has no executor", () => {
    expect(issueRunAction({ issue: none, agentExists: false, choices: { ...empty, agentCount: 1 } })).toEqual({
      kind: "none",
      next: "chooseExecutor",
    })
    expect(issueRunAction({ issue: none, agentExists: false, choices: empty })).toEqual({ kind: "none", next: "createAgent" })
    expect(issueRunAction({ issue: none, agentExists: false, choices: { ...empty, manage: "denied" } })).toEqual({
      kind: "none",
      next: "askForAgent",
    })
  })

  it("treats a kind without an id as no executor", () => {
    expect(issueRunAction({ issue: { executorKind: "agent", executorId: "" }, agentExists: false, choices: empty })).toEqual({
      kind: "none",
      next: "createAgent",
    })
  })
})

describe("issueRunInFlight", () => {
  it("is true while the latest Agent task or Workflow run can still change", () => {
    expect(issueRunInFlight({ status: "pending" }, null)).toBe(true)
    expect(issueRunInFlight({ status: "running" }, null)).toBe(true)
    expect(issueRunInFlight(null, { status: "canceling" })).toBe(true)
  })

  it("is false once both have finished, or when nothing has run", () => {
    expect(issueRunInFlight(null, null)).toBe(false)
    expect(issueRunInFlight({ status: "success" }, { status: "failed" })).toBe(false)
    expect(issueRunInFlight({ status: "canceled" }, { status: "succeeded" })).toBe(false)
  })
})
