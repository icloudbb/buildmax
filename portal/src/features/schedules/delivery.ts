import type { Translate } from "@buildmax/gui"
import type { ApiAssistant, ApiScheduleDelivery } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

/** Why a delivery was skipped, in the words the run history shows. Matches the
 *  reasons in internal/core/schedule. */
const SKIP_REASONS: Record<string, MessageKey> = {
  no_target: "schedules.skip.no_target",
  run_not_succeeded: "schedules.skip.run_not_succeeded",
  assistant_unavailable: "schedules.skip.assistant_unavailable",
  no_bot: "schedules.skip.no_bot",
  requester_not_in_audience: "schedules.skip.requester_not_in_audience",
  link_inactive: "schedules.skip.link_inactive",
  no_conversation: "schedules.skip.no_conversation",
  not_on_roster: "schedules.skip.not_on_roster",
  nothing_releasable: "schedules.skip.nothing_releasable",
}

/** One delivery as a short status line for the run history. */
export function describeDelivery(d: ApiScheduleDelivery | undefined, t: Translate<MessageKey>): string {
  if (!d) return "—"
  switch (d.status) {
    case "pending":
      return t("schedules.delivery.pending")
    case "delivered":
      return t("schedules.delivery.delivered")
    case "failed":
      return t("schedules.delivery.failed")
    case "skipped": {
      const known = d.reason ? SKIP_REASONS[d.reason] : undefined
      const reason = known ? t(known) : d.reason || t("schedules.delivery.unknownReason")
      return t("schedules.delivery.skipped", { reason })
    }
    default:
      return d.status
  }
}

/** The Assistants that can carry this executor's result: their roster has it. */
export function deliveringAssistants(assistants: ApiAssistant[], kind: string, id: string): ApiAssistant[] {
  return assistants.filter((a) => a.roster.some((entry) => entry.kind === kind && entry.id === id))
}
