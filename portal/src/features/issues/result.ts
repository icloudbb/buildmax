import type { IssueOutput, IssueRun, WorkflowRun } from "../../lib/types"
import { taskIsStoppable } from "../../lib/taskStatus"

/**
 * Where a run's text came from: an Agent run's final reply, a Workflow's
 * declared result, or -- for a Workflow that declares none -- the output of
 * its last step that produced one.
 */
export type RunTextSource = "reply" | "workflowResult" | "stepOutput"

export interface RunText {
  source: RunTextSource
  value: string
  /** The step whose output this is, for "stepOutput". */
  stepId?: string
}

/** What one run produced: the files it published and the text it answered with. */
export interface RunProduct {
  files: IssueOutput[]
  text: RunText | null
}

/** A stable key for a run in a list that mixes both kinds. */
export function issueRunKey(run: IssueRun): string {
  return run.kind === "agent" ? `agent:${run.task.id}` : `workflow:${run.run.id}`
}

function present(value: string | null | undefined): value is string {
  return value != null && value.trim() !== ""
}

/**
 * What a run produced, from the same flow response the Issue renders: every
 * surface that answers "what did it produce" reads this, so the Overview and
 * the Results tab cannot disagree.
 */
export function issueRunProduct(run: IssueRun, outputs: IssueOutput[]): RunProduct {
  if (run.kind === "agent") {
    return {
      files: outputs.filter((output) => output.source.taskId === run.task.id && !output.source.workflowRunId),
      text: present(run.output) ? { source: "reply", value: run.output } : null,
    }
  }
  const files = outputs.filter((output) => output.source.workflowRunId === run.run.id)
  if (run.run.result != null) {
    const value = typeof run.run.result === "string" ? run.run.result : JSON.stringify(run.run.result, null, 2)
    return { files, text: { source: "workflowResult", value } }
  }
  const last = [...run.steps].sort((a, b) => b.nodeIndex - a.nodeIndex).find((step) => present(step.output))
  return { files, text: last?.output ? { source: "stepOutput", value: last.output, stepId: last.nodeId } : null }
}

const LIVE_WORKFLOW_RUN: ReadonlySet<WorkflowRun["status"]> = new Set(["pending", "running", "failing", "canceling"])

/** Whether a run can still change without anyone acting on it. */
export function issueRunLive(run: IssueRun): boolean {
  return run.kind === "agent" ? taskIsStoppable(run.task.status) : LIVE_WORKFLOW_RUN.has(run.run.status)
}

/**
 * Whether any of the Issue's runs is still in flight, so the page keeps
 * refreshing after Run instead of showing a start state that never moves --
 * including an earlier run someone retried.
 */
export function issueRunInFlight(runs: IssueRun[]): boolean {
  return runs.some(issueRunLive)
}

/**
 * The Results tab's text entries: a run whose only output was text, and every
 * declared Workflow result, which is a structured outcome in its own right.
 */
export function textResults(runs: IssueRun[], outputs: IssueOutput[]): { run: IssueRun; text: RunText }[] {
  const out: { run: IssueRun; text: RunText }[] = []
  for (const run of runs) {
    const product = issueRunProduct(run, outputs)
    if (product.text && (product.files.length === 0 || product.text.source === "workflowResult")) {
      out.push({ run, text: product.text })
    }
  }
  return out
}
