import { useCallback, useEffect, useState } from "react"
import { navigate } from "../../router"
import { listRemoteSessions, type RemoteSession } from "../../features/remoteControl/api"

interface RemoteControlListProps {
  token: string | null
}

const POLL_INTERVAL_MS = 4000

/** Best-effort "moments ago" without pulling in a date library. */
function relativeSeen(rfc3339?: string): string {
  if (!rfc3339) return "never"
  const then = new Date(rfc3339).getTime()
  if (Number.isNaN(then)) return "unknown"
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000))
  if (secs < 45) return "just now"
  const mins = Math.round(secs / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.round(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}

/**
 * The account-scoped list of the user's live Remote Control sessions. It polls
 * for presence — the same posture the Task page uses for lifecycle — so the
 * online dots stay current without a dedicated push channel.
 */
export function RemoteControlList({ token }: RemoteControlListProps) {
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
        <h1 className="rc-page__title">Remote Control</h1>
        <p className="rc-page__subtitle">
          Sessions you started on your machines with <code>buildmax --remote-control</code>. Watch one from here; it
          keeps running on that machine.
        </p>
      </header>

      {error ? <div className="rc-alert">{error}</div> : null}

      {loaded && sessions.length === 0 && !error ? (
        <div className="rc-empty">
          <p>No live sessions.</p>
          <p className="rc-empty__hint">
            Run <code>buildmax --remote-control</code> on a machine, signed in to this server, to make its session
            reachable here.
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
                  aria-label={s.status === "online" ? "Online" : "Offline"}
                  title={s.status === "online" ? "Online" : "Offline"}
                />
                <span className="rc-list__main">
                  <span className="rc-list__name">{s.display_name || s.host || s.id}</span>
                  <span className="rc-list__meta">
                    {[s.platform, s.host].filter(Boolean).join(" · ") || "—"}
                  </span>
                </span>
                <span className="rc-list__seen">
                  {s.status === "online" ? "online" : `last seen ${relativeSeen(s.last_seen_at)}`}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
