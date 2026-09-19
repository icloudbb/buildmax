import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useMemo, useState } from "react"
import type { ApiAuditEvent } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { exportAuditEvents, getAuditEvents } from "./api"
import { actorLabel, describeEvent, formatEventTime } from "./describe"
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
  const described = describeEvent(event)
  return (
    <li className={described.denied ? "audit-row audit-row--denied" : "audit-row"}>
      <div className="audit-row__main">
        <span className="audit-row__actor">{actorLabel(event, currentUserId)}</span>
        <span className="audit-row__summary">{described.summary}</span>
      </div>
      <div className="audit-row__meta">
        {described.target ? <span className="audit-row__target">{described.target}</span> : null}
        {event.task_run_id ? (
          <span className="audit-row__target" title="The task run this action was taken on behalf of">
            run {event.task_run_id}
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
        .catch((err) => setListError(classifyError(err, "Failed to load the audit trail")))
        .finally(() => setLoading(false))
    },
    [spaceId, token]
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
        .catch((err) => setExportError(getErrorMessage(err, "Failed to export the audit trail")))
        .finally(() => setExporting(false))
    },
    [spaceId, token, exporting]
  )

  if (!currentUserIsOwner) {
    return (
      <section className="settings-section">
        <h2 className="settings-section__title">Audit trail</h2>
        <p className="settings-section__hint">
          {ownerState === "unknown"
            ? "Checking whether you can read the audit trail…"
            : ownerState === "failed"
              ? "Couldn't verify your role in this space, so the audit trail stays unavailable. Refresh to try again."
              : "Only a space owner can read the audit trail. It records who was refused a request, which is not something the rest of a space needs to see."}
        </p>
      </section>
    )
  }

  return (
    <section className="settings-section">
      <h2 className="settings-section__title">Audit trail</h2>
      <p className="settings-section__hint">
        Sign-ins, membership changes, model changes, and refused requests. It records that an
        action happened and who performed it — never prompts, generated content, or credentials.
      </p>

      {/* Exporting is recorded in the trail it exports. Reading the whole
          record is an action on it, and an export that left no trace would be
          the one way to consult the trail without appearing in it. */}
      <div className="audit-export">
        <Button
          variant="secondary" busy={exporting}
          onClick={() => exportTrail("csv")}
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
        <span className="settings-section__hint">
          The whole trail, not the page below. Exports are themselves recorded.
        </span>
      </div>

      {(eventsState.kind === "error" ||
        eventsState.kind === "forbidden" ||
        eventsState.kind === "notFound" ||
        eventsState.kind === "stale") && (
        <Alert
          tone={eventsState.kind === "stale" ? "stale" : eventsState.kind}
          message={eventsState.error.message}
          retry={{ label: "Retry", onClick: () => load(0) }}
        />
      )}
      {exportError ? (
        <p className="settings-section__error" role="alert">
          {exportError}
        </p>
      ) : null}

      {eventsState.kind === "readyEmpty" ? <EmptyState message="Nothing recorded yet." /> : null}

      {eventsState.kind !== "forbidden" && eventsState.kind !== "notFound" && events.length > 0 ? (
        <ul className="audit-list">
          {events.map((event) => (
            <AuditRow key={event.id} event={event} currentUserId={currentUserId} />
          ))}
        </ul>
      ) : null}

      {loading ? <p className="page-activity__empty">Loading…</p> : null}

      {eventsState.kind !== "forbidden" && eventsState.kind !== "notFound" && events.length < total ? (
        <Button
          variant="secondary"
          onClick={() => load(events.length)}
          disabled={loading}
        >
          Show older ({total - events.length} more)
        </Button>
      ) : null}
    </section>
  )
}
