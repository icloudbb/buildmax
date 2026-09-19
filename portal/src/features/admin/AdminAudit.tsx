import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import type { ApiAuditEvent } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { actorLabel, describeEvent, formatEventTime } from "../audit/describe"
import { exportAdminAuditEvents, searchAdminAuditEvents } from "./api"

const PAGE_SIZE = 50

/** Filters the deployment-wide trail supports. Empty strings mean no bound. */
interface AuditFilters {
  spaceId: string
  actorId: string
  action: string
}

/**
 * AdminAudit searches the trail across every space.
 *
 * It is the only place the events with no space can be read at all — logins,
 * administrator grants, account actions. The space-scoped trail could never
 * return them, which is why "Deployment only" is a filter rather than an
 * absence of one: an empty space filter already means "any space".
 */
export function AdminAudit({ token, currentUserId }: { token: string | null; currentUserId?: string }) {
  const [events, setEvents] = useState<ApiAuditEvent[]>([])
  const [total, setTotal] = useState(0)
  const [filters, setFilters] = useState<AuditFilters>({ spaceId: "", actorId: "", action: "" })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [exporting, setExporting] = useState(false)

  const load = useCallback(
    (active: AuditFilters, offset: number) => {
      if (!token) return
      setLoading(true)
      setError(null)
      searchAdminAuditEvents(token, {
        space_id: active.spaceId || undefined,
        actor_id: active.actorId || undefined,
        action: active.action || undefined,
        limit: PAGE_SIZE,
        offset,
      })
        .then((res) => {
          setEvents((prev) => (offset === 0 ? res.events : [...prev, ...res.events]))
          setTotal(res.total)
        })
        .catch((err) => setError(getErrorMessage(err, "Failed to load the audit trail")))
        .finally(() => setLoading(false))
    },
    [token],
  )

  useEffect(() => {
    load({ spaceId: "", actorId: "", action: "" }, 0)
  }, [load])

  function apply(next: AuditFilters) {
    setFilters(next)
    load(next, 0)
  }

  // The export takes the filters currently in the form, not the ones the last
  // search ran under. Anything else would hand back a file that does not match
  // what the operator is looking at.
  function exportTrail(format: "csv" | "jsonl") {
    if (!token || exporting) return
    setExporting(true)
    setError(null)
    exportAdminAuditEvents(token, format, {
      space_id: filters.spaceId || undefined,
      actor_id: filters.actorId || undefined,
      action: filters.action || undefined,
    })
      .catch((err) => setError(getErrorMessage(err, "Failed to export the audit trail")))
      .finally(() => setExporting(false))
  }

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Audit trail</h2>
            <p className="settings-page__section-copy">
              Who did what to which object, across every space. It carries no prompts, no
              generated content, and no credentials.
            </p>
          </div>
        </div>

        <form
          className="admin-toolbar"
          onSubmit={(e) => {
            e.preventDefault()
            apply(filters)
          }}
        >
          <input
            className="admin-input"
            value={filters.spaceId}
            placeholder="Space id"
            aria-label="Filter by space id"
            onChange={(e) => setFilters({ ...filters, spaceId: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.actorId}
            placeholder="Actor id"
            aria-label="Filter by actor id"
            onChange={(e) => setFilters({ ...filters, actorId: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.action}
            placeholder="Action, e.g. user.login"
            aria-label="Filter by action"
            onChange={(e) => setFilters({ ...filters, action: e.target.value })}
          />
          <Button type="submit" variant="secondary" disabled={loading}>
            Search
          </Button>
          <Button
            variant="secondary"
            onClick={() => apply({ spaceId: "none", actorId: filters.actorId, action: filters.action })}
            title="Logins, grants, and account actions — the events no space-scoped reader can see"
          >
            Deployment only
          </Button>
          <Button
            variant="tertiary"
            onClick={() => apply({ spaceId: "", actorId: "", action: "" })}
          >
            Clear
          </Button>
          {/* Exports are recorded in the trail, against the administrator who
              took them — and, when narrowed to one space, in that space's own
              trail as well. */}
          <Button
            variant="secondary"
            onClick={() => exportTrail("csv")}
            busy={exporting}
            title="Download every event matching these filters"
          >
            Export CSV
          </Button>
          <Button
            variant="secondary"
            onClick={() => exportTrail("jsonl")}
            disabled={exporting}
          >
            Export JSONL
          </Button>
        </form>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {events.length === 0 && !loading ? (
          <p className="admin-empty">No events match.</p>
        ) : (
          <ul className="audit-list">
            {events.map((event) => {
              const described = describeEvent(event)
              return (
                <li
                  key={event.id}
                  className={described.denied ? "audit-row audit-row--denied" : "audit-row"}
                >
                  <div className="audit-row__main">
                    <span className="audit-row__actor">{actorLabel(event, currentUserId)}</span>
                    <span className="audit-row__summary">{described.summary}</span>
                  </div>
                  <div className="audit-row__meta">
                    {event.space_id ? (
                      <span className="audit-row__target">{event.space_id}</span>
                    ) : (
                      <span className="admin-pill">deployment</span>
                    )}
                    {described.target ? (
                      <span className="audit-row__target">{described.target}</span>
                    ) : null}
                    <time className="audit-row__time">{formatEventTime(event.created_at)}</time>
                  </div>
                </li>
              )
            })}
          </ul>
        )}

        {events.length < total ? (
          <Button
            variant="secondary"
            disabled={loading}
            onClick={() => load(filters, events.length)}
          >
            Load more ({events.length} of {total})
          </Button>
        ) : null}
      </section>
    </div>
  )
}
