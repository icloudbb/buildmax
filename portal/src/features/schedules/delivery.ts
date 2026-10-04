import type { ApiAssistant, ApiScheduleDelivery } from "../../lib/api/types"

/** Why a delivery was skipped, in the words the run history shows. Matches the
 *  reasons in internal/core/schedule. */
const SKIP_REASONS: Record<string, string> = {
  no_target: "the schedule no longer sends results",
  run_not_succeeded: "the run did not succeed",
  assistant_unavailable: "the assistant is paused or deleted",
  no_bot: "the assistant has no bot",
  requester_not_in_audience: "the person is no longer in the assistant's audience",
  link_inactive: "the person's chat link is gone or inactive",
  no_conversation: "the person has no chat with the assistant",
  not_on_roster: "the assistant's roster no longer includes what this schedule runs",
  nothing_releasable: "the result had no field the assistant may share",
}

/** One delivery as a short status line for the run history. */
export function describeDelivery(d: ApiScheduleDelivery | undefined): string {
  if (!d) return "—"
  switch (d.status) {
    case "pending":
      return "Waiting for the run"
    case "delivered":
      return "Sent"
    case "failed":
      return "Could not be sent"
    case "skipped":
      return `Not sent: ${(d.reason && SKIP_REASONS[d.reason]) || d.reason || "unknown reason"}`
    default:
      return d.status
  }
}

/** The Assistants that can carry this executor's result: their roster has it. */
export function deliveringAssistants(assistants: ApiAssistant[], kind: string, id: string): ApiAssistant[] {
  return assistants.filter((a) => a.roster.some((entry) => entry.kind === kind && entry.id === id))
}
