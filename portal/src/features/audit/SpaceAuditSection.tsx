import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useMemo, useState } from "react"
import type { ApiAuditEvent } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT } from "../../i18n"
import { exportAuditEvents, getAuditEvents } from "./api"
import { actorLabel, describeEvent } from "./describe"
import { useTimestamp } from "../../lib/dateFormat"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed, type PermissionState } from "../../state/permissionState"

const PAGE_SIZE = 50

interface SpaceAuditSectionProps {
  spaceId: string | null
  token: string | null
  /** Owner-only capability state — see docs/design/portal-state-and-permission-feedback.md#permission-model. */
  ownerState: PermissionState
  currentUserId?: string
}

function AuditRow({ event, currentUserId }: { event: ApiAuditEvent; currentUserId?: string }) {
  const t = useT()
  const formatEventTime = useTimestamp()
  const described = describeEvent(event, t)
  return (
    <li className={described.denied ? "audit-row audit-row--denied" : "audit-row"}>
      <div className="audit-row__main">
        <span className="audit-row__actor">{actorLabel(event, t, currentUserId)}</span>
        <span className="audit-row__summary">{described.summary}</span>
      </div>
      <div className="audit-row__meta">
        {described.target ? <span className="audit-row__target">{described.target}</span> : null}
        {event.task_run_id ? (
          <span className="audit-row__target" title={t("audit.runTitle")}>
            {t("audit.run", { id: event.task_run_id })}
          </span>
        ) : null}
        <time className="audit-row__time">{formatEventTime(event.created_at)}</time>
      </div>
    </li>
  )
}

/**
 * SpaceAuditSection lists a space's audit trail.
 *
 * Owner only, matching the server. The trail names who was refused, which is
 * administrative rather than collaborative information.
 */
export function SpaceAuditSection({
  spaceId,
  token,
  ownerState,
  currentUserId,
}: SpaceAuditSectionProps) {
  const t = useT()
  const stableT = useStableT()
  const currentUserIsOwner = isAllowed(ownerState)
  // null means "not yet successfully fetched", distinct from [] meaning the
  // trail genuinely has no events. See deriveResourceState.
  const [eventsData, setEventsData] = useState<ApiAuditEvent[] | null>(null)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [exporting, setExporting] = useState(false)
  // Distinct from listError: the export action's own error.
  const [exportError, setExportError] = useState<string | null>(null)

  const load = useCallback(
    (offset: number) => {
      if (!spaceId || !token) return
      setLoading(true)
      setListError(null)
      getAuditEvents(spaceId, token, { limit: PAGE_SIZE, offset })
        .then((res) => {
          setEventsData((prev) => (offset === 0 ? res.events : [...(prev ?? []), ...res.events]))
          setTotal(res.total)
        })
        // eventsData from a prior successful fetch (if any) is left in place,
        // so a failed refresh reads as Stale rather than wiping the trail.
        .catch((err) => setListError(classifyError(err, stableT("audit.error.load"))))
        .finally(() => setLoading(false))
    },
    [spaceId, token, stableT]
  )

  useEffect(() => {
    if (!currentUserIsOwner) return
    load(0)
  }, [currentUserIsOwner, load])

  const eventsState = useMemo(
    () => deriveResourceState({ loading, data: eventsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, eventsData, listError]
  )
  const events = eventsData ?? []

  const exportTrail = useCallback(
    (format: "csv" | "jsonl") => {
      if (!spaceId || !token || exporting) return
      setExporting(true)
      setExportError(null)
      exportAuditEvents(spaceId, token, format)
        .catch((err) => setExportError(getErrorMessage(err, stableT("audit.error.export"))))
        .finally(() => setExporting(false))
    },
    [spaceId, token, exporting, stableT]
  )

  if (!currentUserIsOwner) {
    return (
      <section className="settings-section">
        <h2 className="settings-section__title">{t("audit.title")}</h2>
        <p className="settings-section__hint">
          {ownerState === "unknown"
            ? t("audit.checking")
            : ownerState === "failed"
              ? t("audit.unverified")
              : t("audit.ownerOnly")}
        </p>
      </section>
    )
  }

  return (
    <section className="settings-section">
      <h2 className="settings-section__title">{t("audit.title")}</h2>
      <p className="settings-section__hint">{t("audit.hint")}</p>

      {/* Exporting is recorded in the trail it exports. Reading the whole
          record is an action on it, and an export that left no trace would be
          the one way to consult the trail without appearing in it. */}
      <div className="audit-export">
        <Button
          variant="secondary" busy={exporting}
          onClick={() => exportTrail("csv")}
        >
          {t("audit.exportCsv")}
        </Button>
        <Button
          variant="secondary"
          onClick={() => exportTrail("jsonl")}
          disabled={exporting}
        >
          {t("audit.exportJsonl")}
        </Button>
        <span className="settings-section__hint">
          {t("audit.exportHint")}
        </span>
      </div>

      {(eventsState.kind === "error" ||
        eventsState.kind === "forbidden" ||
        eventsState.kind === "notFound" ||
        eventsState.kind === "stale") && (
        <Alert
          tone={eventsState.kind === "stale" ? "stale" : eventsState.kind}
          message={eventsState.error.message}
          retry={{ label: t("shell.retry"), onClick: () => load(0) }}
        />
      )}
      {exportError ? (
        <p className="settings-section__error" role="alert">
          {exportError}
        </p>
      ) : null}

      {eventsState.kind === "readyEmpty" ? <EmptyState message={t("audit.empty")} /> : null}

      {eventsState.kind !== "forbidden" && eventsState.kind !== "notFound" && events.length > 0 ? (
        <ul className="audit-list">
          {events.map((event) => (
            <AuditRow key={event.id} event={event} currentUserId={currentUserId} />
          ))}
        </ul>
      ) : null}

      {loading ? <p className="page-activity__empty">{t("shell.loading")}</p> : null}

      {eventsState.kind !== "forbidden" && eventsState.kind !== "notFound" && events.length < total ? (
        <Button
          variant="secondary"
          onClick={() => load(events.length)}
          disabled={loading}
        >
          {t("audit.showOlder", { count: total - events.length })}
        </Button>
      ) : null}
    </section>
  )
}
