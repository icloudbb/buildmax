import type { ReactNode } from "react"
import { Button } from "@buildmax/gui"

export interface AlertAction {
  label: string
  onClick: () => void
}

/**
 * Presentation only: which ResourceState/PermissionState kind this alert
 * speaks for. Callers own deciding when to show one (see deriveResourceState
 * in ../../state/resourceState and derivePermissionState in
 * ../../state/permissionState); this component owns only how it looks.
 */
export type AlertTone = "error" | "forbidden" | "notFound" | "stale"

export interface AlertProps {
  tone: AlertTone
  message: string
  /** Shown for a recoverable failure. Omit when there is nothing to retry (e.g. Forbidden). */
  retry?: AlertAction
  /** Safe navigation away from the failure, e.g. back to the collection. */
  navigate?: AlertAction
  children?: ReactNode
}

const TITLE: Record<AlertTone, string> = {
  error: "Something went wrong",
  forbidden: "Access denied",
  notFound: "Not found",
  stale: "Showing previous data",
}

const TONE_CLASS: Record<AlertTone, string> = {
  error: "error",
  forbidden: "forbidden",
  notFound: "not-found",
  stale: "stale",
}

/**
 * Shared presenter for Error, Forbidden, Not found, and Stale (see the state
 * model in docs/design/portal-state-and-permission-feedback.md). Stale uses
 * `role="status"` since it is a persistent warning over otherwise-current
 * content, not a blocking alert.
 */
export function Alert({ tone, message, retry, navigate, children }: AlertProps) {
  return (
    <div className={`state-alert state-alert--${TONE_CLASS[tone]}`} role={tone === "stale" ? "status" : "alert"}>
      <p className="state-alert__title">{TITLE[tone]}</p>
      <p className="state-alert__message">{message}</p>
      {children}
      {(retry || navigate) && (
        <div className="state-alert__actions">
          {retry && (
            <Button variant="secondary" onClick={retry.onClick}>
              {retry.label}
            </Button>
          )}
          {navigate && (
            <Button variant="tertiary" onClick={navigate.onClick}>
              {navigate.label}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
