import { useCallback, useEffect, useState } from "react"
import { Button } from "@buildmax/gui"
import { useAuth } from "../../contexts/AuthContext"
import { Alert } from "../../components/state/Alert"
import { listAccountSessions, revokeAccountSession, type AccountSession } from "../../features/sessions/api"
import { lastSeenAt, orderSessions, sessionMethodKey, sessionPlatformKey } from "../../features/sessions/model"
import { getErrorMessage } from "../../lib/errorMessage"
import { useTimestamp } from "../../lib/dateFormat"
import { useStableT, useT } from "../../i18n"

/**
 * Every session signed in to this account, each revocable without an operator.
 * Native sign-in lets a person create sessions for other machines, so ending
 * one confirmed by mistake has to be theirs to do. See
 * docs/design/enterprise-identity-and-access.md section 14.5.
 */
export function AccountSessionsSection() {
  const t = useT()
  const stableT = useStableT()
  const formatTimestamp = useTimestamp()
  const { token, logout } = useAuth()
  const [sessions, setSessions] = useState<AccountSession[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [revokingId, setRevokingId] = useState<string | null>(null)
  const [revokeError, setRevokeError] = useState<string | null>(null)
  const [revokedName, setRevokedName] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!token) return
    setLoadError(null)
    listAccountSessions(token)
      .then((list) => setSessions(orderSessions(list)))
      .catch((err) => setLoadError(getErrorMessage(err, stableT("account.sessions.loadError"))))
  }, [token, stableT])

  useEffect(() => {
    load()
  }, [load])

  function nameOf(session: AccountSession): string {
    return session.client_label?.trim() || t(sessionPlatformKey(session.platform))
  }

  function handleRevoke(session: AccountSession) {
    if (!token) return
    if (session.current) {
      // This browser's own session is ended by the ordinary sign-out, which
      // revokes it and clears the cookie in one action and one audit event.
      if (window.confirm(t("account.sessions.signOutConfirm"))) logout()
      return
    }
    if (!window.confirm(t("account.sessions.revokeConfirm", { name: nameOf(session) }))) return
    setRevokeError(null)
    setRevokedName(null)
    setRevokingId(session.session_id)
    revokeAccountSession(token, session.session_id)
      .then(() => {
        setRevokedName(nameOf(session))
        load()
      })
      .catch((err) => setRevokeError(getErrorMessage(err, stableT("account.sessions.revokeError"))))
      .finally(() => setRevokingId(null))
  }

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.sessions.title")}</h2>
          <p className="settings-page__section-copy">{t("account.sessions.copy")}</p>
        </div>
      </div>
      {loadError ? (
        <Alert tone="error" message={loadError} retry={{ label: t("shell.retry"), onClick: load }} />
      ) : null}
      {revokeError ? (
        <p className="settings-section__error" role="alert">
          {revokeError}
        </p>
      ) : null}
      {revokedName ? (
        <p className="settings-section__muted" role="status">
          {t("account.sessions.revoked", { name: revokedName })}
        </p>
      ) : null}
      {sessions === null ? (
        loadError ? null : <p className="page-activity__empty">{t("account.sessions.loading")}</p>
      ) : sessions.length === 0 ? (
        <p className="page-activity__empty">{t("account.sessions.empty")}</p>
      ) : (
        <ul className="space-settings-page__member-list" aria-label={t("account.sessions.listLabel")}>
          {sessions.map((session) => {
            const label = session.client_label?.trim()
            const methodKey = sessionMethodKey(session.auth_method)
            const seen = lastSeenAt(session)
            return (
              <li key={session.session_id} className="space-settings-page__member">
                <div className="space-settings-page__member-main">
                  <span className="space-settings-page__member-name account-sessions__name">
                    {t(sessionPlatformKey(session.platform))}
                    {session.current ? (
                      <span className="space-settings-page__badge">{t("account.sessions.current")}</span>
                    ) : null}
                  </span>
                  {label ? (
                    <span className="session-label">
                      {label}
                      <span className="session-label__unverified">{t("account.sessions.unverified")}</span>
                    </span>
                  ) : null}
                  <span className="space-settings-page__member-meta account-sessions__meta">
                    {[
                      methodKey ? t(methodKey) : null,
                      t("account.sessions.created", { date: formatTimestamp(session.created_at) }),
                      seen
                        ? t("account.sessions.lastSeen", { date: formatTimestamp(seen) })
                        : t("account.sessions.notSeen"),
                      t("account.sessions.expires", { date: formatTimestamp(session.expires_at) }),
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </span>
                </div>
                <div className="space-settings-page__member-actions">
                  <Button
                    variant={session.current ? "secondary" : "danger"}
                    size="compact"
                    busy={revokingId === session.session_id}
                    disabled={revokingId !== null}
                    aria-label={
                      session.current
                        ? t("account.sessions.signOut")
                        : t("account.sessions.revokeFor", { name: nameOf(session) })
                    }
                    onClick={() => handleRevoke(session)}
                  >
                    {session.current ? t("account.sessions.signOut") : t("account.sessions.revoke")}
                  </Button>
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
