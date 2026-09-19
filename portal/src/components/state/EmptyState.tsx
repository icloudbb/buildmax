import type { ReactNode } from "react"
import { Button, type ButtonVariant } from "@buildmax/gui"

export interface EmptyStateAction {
  label: string
  onClick: () => void
  variant?: ButtonVariant
}

export interface EmptyStateProps {
  /** A specific explanation, not a generic "No items." (see the design's Ready empty row). */
  message: string
  /** A valid creation or navigation action, when one exists for this collection. */
  action?: EmptyStateAction
  children?: ReactNode
}

/** Shared presenter for the Ready empty state: a request that succeeded with no objects. */
export function EmptyState({ message, action, children }: EmptyStateProps) {
  return (
    <div className="state-empty">
      <p className="state-empty__message">{message}</p>
      {children}
      {action && (
        <Button variant={action.variant ?? "primary"} onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </div>
  )
}
