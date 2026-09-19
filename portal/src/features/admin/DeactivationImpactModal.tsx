import { useEffect, useState } from "react"
import { Button, BaseModal } from "@buildmax/gui"
import type { ApiAdminUser, ApiDeactivationImpact } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { getDeactivationImpact } from "./api"

interface Props {
  open: boolean
  user: ApiAdminUser | null
  token: string
  /** True while the parent's disable call is in flight. */
  busy: boolean
  onCancel: () => void
  onConfirm: (retireWebhookKeys: boolean) => void
}

/**
 * The guided-disable step: before the account gate commits, show what the
 * disable will stop — sessions, machine credentials, schedules, in-flight runs —
 * the Spaces it would leave without an owner, and how long already-running work
 * may take to stop. The operator chooses whether this is a suspension that keeps
 * the account's webhook keys or a leaver whose keys are retired. Counts and ids
 * only; it never shows a Space's contents.
 */
export function DeactivationImpactModal({ open, user, token, busy, onCancel, onConfirm }: Props) {
  const [impact, setImpact] = useState<ApiDeactivationImpact | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [retireKeys, setRetireKeys] = useState(false)

  useEffect(() => {
    if (!open || !user) return
    let cancelled = false
    setImpact(null)
    setError(null)
    setRetireKeys(false)
    setLoading(true)
    getDeactivationImpact(token, user.id)
      .then((res) => {
        if (!cancelled) setImpact(res)
      })
      .catch((err) => {
        if (!cancelled) setError(getErrorMessage(err, "Could not load the impact"))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, user, token])

  if (!user) return null

  const activeRuns = impact
    ? Object.values(impact.active_runs_by_status).reduce((a, b) => a + b, 0)
    : 0

  return (
    <BaseModal
      open={open}
      title={`Disable ${user.email}?`}
      titleId="deactivation-impact-title"
      onClose={() => {
        if (busy) return
        onCancel()
      }}
    >
      <div className="modal__body">
        <p className="admin-detail__muted">
          Every credential this account holds stops working immediately. Live sessions are
          revoked, its schedules pause, and its in-flight runs are canceled. This is not
          deletion — enabling reverses the gate and nothing else.
        </p>

        {loading ? <p className="admin-detail__muted">Loading impact…</p> : null}
        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {impact ? (
          <>
            <ul className="admin-impact">
              <li>
                <span>Live sessions</span>
                <strong>{impact.live_sessions}</strong>
              </li>
              <li>
                <span>Webhook keys</span>
                <strong>{impact.webhook_keys}</strong>
              </li>
              <li>
                <span>Memberships</span>
                <strong>{impact.memberships.length}</strong>
              </li>
              <li>
                <span>Enabled schedules</span>
                <strong>{impact.enabled_schedules}</strong>
              </li>
              <li>
                <span>Active runs</span>
                <strong>{activeRuns}</strong>
              </li>
            </ul>

            {impact.sole_owned_space_ids.length > 0 ? (
              <p className="admin-impact__warn" role="alert">
                {impact.sole_owned_space_ids.length} shared space
                {impact.sole_owned_space_ids.length === 1 ? "" : "s"} would be left with no
                enabled owner. Transfer ownership first, or recover it afterward from Spaces.
              </p>
            ) : null}

            <p className="admin-detail__muted">
              Already-running work stops within about {impact.cancellation_bound} once its
              worker next checks in; a tool call already in progress may finish.
            </p>

            {impact.webhook_keys > 0 ? (
              <label className="admin-impact__choice">
                <input
                  type="checkbox"
                  checked={retireKeys}
                  onChange={(e) => setRetireKeys(e.target.checked)}
                />
                <span>
                  Retire this account's webhook keys permanently (a leaver, not a temporary
                  suspension). Leave unchecked to keep them for a deliberate return.
                </span>
              </label>
            ) : null}
          </>
        ) : null}

        <div className="admin-detail__actions">
          <Button variant="secondary" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="danger"
            busy={busy}
            disabled={loading}
            onClick={() => onConfirm(retireKeys)}
          >
            Disable account
          </Button>
        </div>
      </div>
    </BaseModal>
  )
}
