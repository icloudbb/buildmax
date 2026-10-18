import type { Translate } from "@buildmax/gui"
import type { ApiTaskOrigin } from "../../lib/api/types"
import type { Route, Task } from "../../lib/types"
import type { MessageKey } from "../../i18n"

/** How a task reads in a heading or a list row. */
export interface TaskLabel {
  /** The name a person knows the task by. */
  primary: string
  /** The task's own generated title, when its origin names it instead. */
  secondary: string | null
}

/** The kind of thing an origin is, as a word: "Issue", "Workflow". */
export function originKindLabel(origin: ApiTaskOrigin, t: Translate<MessageKey>): string {
  return t(`tasks.origin.kind.${origin.kind}`)
}

/** An origin's own name, or its kind when it has none. */
export function originName(origin: ApiTaskOrigin, t: Translate<MessageKey>): string {
  return origin.name?.trim() || originKindLabel(origin, t)
}

/** An origin as a reader names it: a Workflow step reads "QA review · Step 2". */
export function originLabel(origin: ApiTaskOrigin, t: Translate<MessageKey>): string {
  const name = originName(origin, t)
  return origin.kind === "workflow" && origin.step ? t("tasks.origin.workflowStep", { workflow: name, step: origin.step }) : name
}

/** Where a reader goes to see an origin. */
export function originRoute(origin: ApiTaskOrigin, spaceId: string): Route {
  switch (origin.kind) {
    case "workflow":
      return { name: "workflowRun", spaceId, workflowRunId: origin.id }
    case "issue":
      return { name: "issue", spaceId, issueId: origin.id }
    case "conversation":
      return { name: "chat", spaceId, conversationId: origin.id }
    case "schedule":
      return { name: "schedules", spaceId }
  }
}

/**
 * The label a task is listed and headed by, from its authoritative origin
 * (docs/design/portal-work-and-execution-experience.md#execution-provenance).
 *
 * An Issue, a Workflow step, or a Schedule is the work a person started, so it
 * names the task, and every run of it reads the same however it went —
 * including one that failed before it started. A Conversation asks for many
 * different tasks, so a task it started keeps its own title, as does one with
 * no recorded origin. The generated title is then only secondary.
 */
export function taskLabel(task: Pick<Task, "title" | "origin">, t: Translate<MessageKey>): TaskLabel {
  const own = task.title.trim() || t("tasks.untitled")
  const origin = task.origin
  if (!origin || origin.kind === "conversation") return { primary: own, secondary: null }
  const primary = originLabel(origin, t)
  return { primary, secondary: own === primary ? null : own }
}
