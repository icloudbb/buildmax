import type { ApiAssistantAvailability } from "../../lib/api/types"
import { useT } from "../../i18n"
import { describeAvailability } from "./model"

/** The availability pill. Callers show the reason as text where it matters. */
export function AvailabilityBadge({ availability }: { availability: ApiAssistantAvailability }) {
  const t = useT()
  const view = describeAvailability(availability, t)
  return (
    <span className={`sec-status sec-status--${view.tone} asst-status`} data-testid="assistant-availability">
      <span className="sec-status__dot" aria-hidden />
      {view.label}
    </span>
  )
}
