import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { useStableT, useT } from "../../i18n"
import type { ApiAuditEvent } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { actorLabel, describeEvent } from "../audit/describe"
import { useTimestamp } from "../../lib/dateFormat"
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
  const t = useT()
  const formatEventTime = useTimestamp()
  const stableT = useStableT()
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
        .catch((err) => setError(getErrorMessage(err, stableT("admin.audit.loadError"))))
        .finally(() => setLoading(false))
    },
    [token, stableT],
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
      .catch((err) => setError(getErrorMessage(err, stableT("admin.audit.exportError"))))
      .finally(() => setExporting(false))
  }

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.audit.title")}</h2>
            <p className="settings-page__section-copy">{t("admin.audit.copy")}</p>
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
            placeholder={t("admin.audit.spacePlaceholder")}
            aria-label={t("admin.audit.spaceLabel")}
            onChange={(e) => setFilters({ ...filters, spaceId: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.actorId}
            placeholder={t("admin.audit.actorPlaceholder")}
            aria-label={t("admin.audit.actorLabel")}
            onChange={(e) => setFilters({ ...filters, actorId: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.action}
            placeholder={t("admin.audit.actionPlaceholder")}
            aria-label={t("admin.audit.actionLabel")}
            onChange={(e) => setFilters({ ...filters, action: e.target.value })}
          />
          <Button type="submit" variant="primary" disabled={loading}>
            {t("admin.search")}
          </Button>
          <Button
            variant="secondary"
            onClick={() => apply({ spaceId: "none", actorId: filters.actorId, action: filters.action })}
            title={t("admin.audit.deploymentOnlyTitle")}
          >
            {t("admin.audit.deploymentOnly")}
          </Button>
          <Button
            variant="tertiary"
            onClick={() => apply({ spaceId: "", actorId: "", action: "" })}
          >
            {t("admin.clear")}
          </Button>
          {/* Exports are recorded in the trail, against the administrator who
              took them — and, when narrowed to one space, in that space's own
              trail as well. */}
          <Button
            variant="secondary"
            onClick={() => exportTrail("csv")}
            busy={exporting}
            title={t("admin.audit.exportTitle")}
          >
            {t("admin.audit.exportCsv")}
          </Button>
          <Button
            variant="secondary"
            onClick={() => exportTrail("jsonl")}
            disabled={exporting}
          >
            {t("admin.audit.exportJsonl")}
          </Button>
        </form>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {events.length === 0 && !loading ? (
          <p className="admin-empty">{t("admin.audit.noEvents")}</p>
        ) : (
          <ul className="audit-list">
            {events.map((event) => {
              const described = describeEvent(event, t)
              return (
                <li
                  key={event.id}
                  className={described.denied ? "audit-row audit-row--denied" : "audit-row"}
                >
                  <div className="audit-row__main">
                    <span className="audit-row__actor">{actorLabel(event, t, currentUserId)}</span>
                    <span className="audit-row__summary">{described.summary}</span>
                  </div>
                  <div className="audit-row__meta">
                    {event.space_id ? (
                      <span className="audit-row__target">{event.space_id}</span>
                    ) : (
                      <span className="admin-pill">{t("admin.audit.deployment")}</span>
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
            {t("admin.loadMore", { shown: events.length, total })}
          </Button>
        ) : null}
      </section>
    </div>
  )
}
