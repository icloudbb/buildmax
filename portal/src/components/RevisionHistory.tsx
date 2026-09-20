import { Button } from "@buildmax/gui"
import { Alert } from "./state/Alert"
import { EmptyState } from "./state/EmptyState"
import type { ResourceState } from "../state/resourceState"

interface RevisionEntry {
  id: string
  revision: number
  createdBy: string
  createdLabel: string
  summary?: string | null
}

interface RevisionHistoryProps {
  title: string
  state: ResourceState<RevisionEntry[]>
  onRetry: () => void
  currentRevision: number
  canRestore: boolean
  restoringRevision: number | null
  /** Restore's own error, tagged with which revision it was, so it renders
   * next to that row instead of a page-level banner nothing points back to. */
  restoreError: { revision: number; message: string } | null
  onRestore: (revision: number) => void
}

/**
 * Shared history list for agents and workflows. Restoring writes an older
 * version's content back as a new version, so the button says Restore rather
 * than anything that suggests the versions since are discarded.
 */
export function RevisionHistory({
  title,
  state,
  onRetry,
  currentRevision,
  canRestore,
  restoringRevision,
  restoreError,
  onRestore,
}: RevisionHistoryProps) {
  return (
    <section className="revision-history">
      <div className="revision-history__head">
        <h3 className="revision-history__title">{title}</h3>
        {currentRevision > 0 ? (
          <span className="page-activity__meta">Current: v{currentRevision}</span>
        ) : null}
      </div>
      {(state.kind === "error" ||
        state.kind === "forbidden" ||
        state.kind === "notFound" ||
        state.kind === "stale") && (
        <Alert
          tone={state.kind === "stale" ? "stale" : state.kind}
          message={state.error.message}
          retry={{ label: "Retry", onClick: onRetry }}
        />
      )}
      {state.kind === "loading" ? (
        <p className="page-activity__empty">Loading history…</p>
      ) : state.kind === "readyEmpty" ? (
        <EmptyState message="No history recorded yet." />
      ) : state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" ? null : (
        <ol className="revision-history__list">
          {state.data.map((entry) => (
            <li key={entry.id} className="revision-history__item">
              <div className="revision-history__item-head">
                <strong>v{entry.revision}</strong>
                <span className="page-activity__meta">
                  {entry.createdBy} · {entry.createdLabel}
                </span>
                {canRestore && entry.revision !== currentRevision ? (
                  <Button
                    variant="secondary" size="compact" busy={restoringRevision === entry.revision}
                    disabled={restoringRevision !== null}
                    onClick={() => onRestore(entry.revision)}
                  >
                    Restore
                  </Button>
                ) : null}
              </div>
              {entry.summary ? (
                <pre className="revision-history__summary">{entry.summary}</pre>
              ) : null}
              {restoreError?.revision === entry.revision ? (
                <p className="modal__error">{restoreError.message}</p>
              ) : null}
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
