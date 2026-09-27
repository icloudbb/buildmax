import type { ApiConversationMessage, ApiTask } from "../../lib/api/types"

export type ThreadEntry =
  | { kind: "message"; at: string; message: ApiConversationMessage }
  | { kind: "task"; at: string; task: ApiTask }

/**
 * Whether a task's run is over, in the vocabulary the server actually stores.
 *
 * `lib/taskStatus` speaks the lowercase words the Issue pages use; a task read
 * straight from the conversation route carries the run status, so this reads
 * both rather than making the caller guess which one it has.
 */
export function taskRunFinished(status: string): boolean {
  const normalized = status.toUpperCase()
  return normalized === "SUCCEEDED" || normalized === "SUCCESS" || normalized === "FAILED" || normalized === "CANCELED"
}

export function taskRunFailed(status: string): boolean {
  const normalized = status.toUpperCase()
  return normalized === "FAILED" || normalized === "CANCELED"
}

/** A run's coarse tone for status chips: still going, finished ok, or failed. */
export function runStatusTone(status: string): "running" | "done" | "failed" {
  if (!taskRunFinished(status)) return "running"
  return taskRunFailed(status) ? "failed" : "done"
}

/** The human label for a run status, in the server's own vocabulary. */
export function runStatusLabel(status: string): string {
  switch (status.toUpperCase()) {
    case "PENDING":
      return "Queued"
    case "SCHEDULED":
      return "Starting"
    case "RUNNING":
      return "Running"
    case "SUCCEEDED":
    case "SUCCESS":
      return "Done"
    case "FAILED":
      return "Failed"
    case "CANCELED":
      return "Stopped"
    default:
      return status
  }
}

/**
 * A task's status label. A task whose latest run ended on AskUser questions is
 * finished as a run but waiting on the user, and says so rather than "Done".
 */
export function taskStatusLabel(task: { status: string; awaiting_answer?: boolean }): string {
  return task.awaiting_answer ? "Needs your answer" : runStatusLabel(task.status)
}

/**
 * Order the transcript and the conversation's task cards into one list.
 *
 * A message and a task are separate records with separate lifecycles, so the
 * only thing that orders them against each other is when each was created.
 * Timestamps are whole seconds, which makes ties normal rather than rare: a
 * task placed after the message in the same second reflects the causality —
 * the turn read the message and then started the task.
 *
 * A task card is not a message. It carries execution state that keeps changing
 * after it appears, and it is deliberately not folded into an assistant reply.
 */
export function buildConversationThread(
  messages: ApiConversationMessage[],
  tasks: ApiTask[]
): ThreadEntry[] {
  const entries: ThreadEntry[] = [
    ...messages.map((message): ThreadEntry => ({ kind: "message", at: message.created_at, message })),
    ...tasks.map((task): ThreadEntry => ({ kind: "task", at: task.created_at, task })),
  ]
  return entries
    .map((entry, index) => ({ entry, index }))
    .sort((a, b) => {
      if (a.entry.at !== b.entry.at) return a.entry.at < b.entry.at ? -1 : 1
      if (a.entry.kind !== b.entry.kind) return a.entry.kind === "message" ? -1 : 1
      return a.index - b.index
    })
    .map(({ entry }) => entry)
}
