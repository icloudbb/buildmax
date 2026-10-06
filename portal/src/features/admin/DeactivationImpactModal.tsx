import { useEffect, useState } from "react"
import { Button, BaseModal } from "@buildmax/gui"
import { useStableT, useT } from "../../i18n"
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
  const t = useT()
  const stableT = useStableT()
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
        if (!cancelled) setError(getErrorMessage(err, stableT("admin.impact.loadError")))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, user, token, stableT])

  if (!user) return null

  const activeRuns = impact
    ? Object.values(impact.active_runs_by_status).reduce((a, b) => a + b, 0)
    : 0

  return (
    <BaseModal
      open={open}
      title={t("admin.impact.title", { email: user.email })}
      titleId="deactivation-impact-title"
      onClose={() => {
        if (busy) return
        onCancel()
      }}
    >
      <div className="modal__body">
        <p className="admin-detail__muted">{t("admin.impact.intro")}</p>

        {loading ? <p className="admin-detail__muted">{t("admin.impact.loading")}</p> : null}
        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {impact ? (
          <>
            <ul className="admin-impact">
              <li>
                <span>{t("admin.impact.liveSessions")}</span>
                <strong>{impact.live_sessions}</strong>
              </li>
              <li>
                <span>{t("admin.impact.webhookKeys")}</span>
                <strong>{impact.webhook_keys}</strong>
              </li>
              <li>
                <span>{t("admin.impact.memberships")}</span>
                <strong>{impact.memberships.length}</strong>
              </li>
              <li>
                <span>{t("admin.impact.enabledSchedules")}</span>
                <strong>{impact.enabled_schedules}</strong>
              </li>
              <li>
                <span>{t("admin.impact.activeRuns")}</span>
                <strong>{activeRuns}</strong>
              </li>
            </ul>

            {impact.sole_owned_space_ids.length > 0 ? (
              <p className="admin-impact__warn" role="alert">
                {t("admin.impact.soleOwned", { count: impact.sole_owned_space_ids.length })}
              </p>
            ) : null}

            <p className="admin-detail__muted">
              {t("admin.impact.cancellationBound", { bound: impact.cancellation_bound })}
            </p>

            {impact.webhook_keys > 0 ? (
              <label className="admin-impact__choice">
                <input
                  type="checkbox"
                  checked={retireKeys}
                  onChange={(e) => setRetireKeys(e.target.checked)}
                />
                <span>{t("admin.impact.retireKeys")}</span>
              </label>
            ) : null}
          </>
        ) : null}

        <div className="admin-detail__actions">
          <Button variant="secondary" disabled={busy} onClick={onCancel}>
            {t("admin.impact.cancel")}
          </Button>
          <Button
            variant="danger"
            busy={busy}
            disabled={loading}
            onClick={() => onConfirm(retireKeys)}
          >
            {t("admin.impact.disableAccount")}
          </Button>
        </div>
      </div>
    </BaseModal>
  )
}
