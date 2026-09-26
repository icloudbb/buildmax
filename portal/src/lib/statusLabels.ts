/** Human-facing labels for persisted status values. Keep technical values in APIs. */
export function statusLabel(value: string): string {
  const labels: Record<string, string> = {
    todo: "To do",
    in_progress: "In progress",
    done: "Done",
    pending: "Pending",
    running: "Running",
    failing: "Stopping after failure",
    canceling: "Canceling",
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

/**
 * Where a conversation came from, or null for Portal chat itself. Only the
 * channels that arrive from elsewhere (a chat app such as Telegram, a webhook)
 * are worth marking in a list of mostly Portal conversations.
 */
export function conversationSourceLabel(channel: string): string | null {
  if (!channel || channel === "portal") return null
  return channel.replace(/^./, (first: string) => first.toUpperCase())
}
