import type { Translate } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { useT, type MessageKey } from "../../i18n"
import { navigate } from "../../router"
import { listRemoteSessions, type RemoteSession } from "../../features/remoteControl/api"

interface RemoteControlListProps {
  token: string | null
}

const POLL_INTERVAL_MS = 4000

/** Best-effort "moments ago" without pulling in a date library. */
function relativeSeen(t: Translate<MessageKey>, rfc3339?: string): string {
  if (!rfc3339) return t("remote.seen.never")
  const then = new Date(rfc3339).getTime()
  if (Number.isNaN(then)) return t("remote.seen.unknown")
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000))
  if (secs < 45) return t("remote.seen.justNow")
  const mins = Math.round(secs / 60)
  if (mins < 60) return t("remote.seen.minutes", { count: mins })
  const hours = Math.round(mins / 60)
  if (hours < 24) return t("remote.seen.hours", { count: hours })
  return t("remote.seen.days", { count: Math.round(hours / 24) })
}

/**
 * The account-scoped list of the user's live Remote Control sessions. It polls
 * for presence — the same posture the Task page uses for lifecycle — so the
 * online dots stay current without a dedicated push channel.
 */
export function RemoteControlList({ token }: RemoteControlListProps) {
  const t = useT()
  const [sessions, setSessions] = useState<RemoteSession[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback(async () => {
    if (!token) return
    try {
      setSessions(await listRemoteSessions(token))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoaded(true)
    }
  }, [token])

  useEffect(() => {
    void load()
    const id = window.setInterval(() => void load(), POLL_INTERVAL_MS)
    return () => window.clearInterval(id)
  }, [load])

  return (
    <div className="rc-page">
      <header className="rc-page__header">
        <h1 className="rc-page__title">{t("remote.title")}</h1>
        <p className="rc-page__subtitle">
          {t("remote.subtitleBefore")}
          <code>buildmax --remote-control</code>
          {t("remote.subtitleAfter")}
        </p>
      </header>

      {error ? <div className="rc-alert">{error}</div> : null}

      {loaded && sessions.length === 0 && !error ? (
        <div className="rc-empty">
          <p>{t("remote.empty")}</p>
          <p className="rc-empty__hint">
            {t("remote.emptyHintBefore")}
            <code>buildmax --remote-control</code>
            {t("remote.emptyHintAfter")}
          </p>
        </div>
      ) : null}

      {sessions.length > 0 ? (
        <ul className="rc-list">
          {sessions.map((s) => (
            <li key={s.id}>
              <button
                type="button"
                className="rc-list__row"
                onClick={() => navigate({ name: "remoteControlSession", sessionId: s.id })}
              >
                <span
                  className={`rc-status-dot rc-status-dot--${s.status}`}
                  aria-label={s.status === "online" ? t("remote.online") : t("remote.offline")}
                  title={s.status === "online" ? t("remote.online") : t("remote.offline")}
                />
                <span className="rc-list__main">
                  <span className="rc-list__name">{s.display_name || s.host || s.id}</span>
                  <span className="rc-list__meta">
                    {[s.platform, s.host].filter(Boolean).join(" · ") || "—"}
                  </span>
                </span>
                <span className="rc-list__seen">
                  {s.status === "online"
                    ? t("remote.onlineLower")
                    : t("remote.lastSeen", { when: relativeSeen(t, s.last_seen_at) })}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
