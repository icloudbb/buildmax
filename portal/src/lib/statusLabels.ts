/** Human-facing labels for persisted status values. Keep technical values in APIs. */
export function statusLabel(value: string): string {
  const labels: Record<string, string> = {
    todo: "To do",
    in_progress: "In progress",
    done: "Done",
    pending: "Pending",
    running: "Running",
    success: "Succeeded",
    succeeded: "Succeeded",
    failed: "Failed",
    canceled: "Canceled",
    blocked: "Blocked",
    draft: "Draft",
    published: "Published",
    archived: "Archived",
    no_runs: "No runs yet",
  }
  return labels[value] ?? value.replace(/_/g, " ").replace(/^./, (first: string) => first.toUpperCase())
}
