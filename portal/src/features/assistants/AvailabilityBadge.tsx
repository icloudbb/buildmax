import type { ApiAssistantAvailability } from "../../lib/api/types"
import { describeAvailability } from "./model"

/** The availability pill. Callers show the reason as text where it matters. */
export function AvailabilityBadge({ availability }: { availability: ApiAssistantAvailability }) {
  const view = describeAvailability(availability)
  return (
    <span className={`sec-status sec-status--${view.tone} asst-status`} data-testid="assistant-availability">
      <span className="sec-status__dot" aria-hidden />
      {view.label}
    </span>
  )
}
