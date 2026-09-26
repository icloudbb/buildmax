import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@buildmax/gui"
import type { Issue } from "../../lib/types"
import { buildHash } from "../../router"
import { ApiRequestError } from "../../lib/api/client"
import { apiIssueToIssue } from "../../lib/api/mappers"
import { getErrorMessage } from "../../lib/errorMessage"
import { statusLabel } from "../../lib/statusLabels"
import {
  ISSUE_LANES,
  LANE_PAGE_SIZE,
  appendPage,
  getIssues,
  reloadLimit,
  updateIssue,
  type GetIssuesOptions,
  type IssueLane,
} from "../../features/issues"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"

interface LaneState {
  /** null until this lane's first successful load, as in deriveResourceState. */
  items: Issue[] | null
  total: number
  loading: boolean
  error: RequestError | null
  loadingMore: boolean
  moreError: string | null
}

const EMPTY_LANE: LaneState = { items: null, total: 0, loading: true, error: null, loadingMore: false, moreError: null }

function initialLanes(): Record<IssueLane, LaneState> {
  return { todo: { ...EMPTY_LANE }, in_progress: { ...EMPTY_LANE }, done: { ...EMPTY_LANE } }
}

interface Announcement {
  tone: "status" | "alert"
  text: string
}

interface IssueBoardProps {
  token: string | null
  spaceId: string
  /** The List's filters; every lane applies them identically. */
  filter: GetIssuesOptions
  ownerLabel: (issue: Issue) => string | null
  executorLabel: (issue: Issue) => string | null
}

/**
 * Space Issues projected into one lane per status. Each lane is its own query
 * with its own total, page, and resource state: grouping one fetched page
 * would make a lane look empty only because its Issues fell off that page.
 * A move is an ordinary versioned status update; nothing here runs work.
 */
export function IssueBoard({ token, spaceId, filter, ownerLabel, executorLabel }: IssueBoardProps) {
  const [lanes, setLanes] = useState<Record<IssueLane, LaneState>>(initialLanes)
  const [moving, setMoving] = useState<Record<string, IssueLane>>({})
  const [announcement, setAnnouncement] = useState<Announcement | null>(null)
  // Where focus goes once the lanes re-render after a move settles: the moved
  // card, or its lane heading if the card is no longer loaded.
  const focusTarget = useRef<{ issueId: string; lane: IssueLane } | null>(null)
  // Bumped per lane on every replacing load, so a slow response for an old
  // filter or an earlier reload cannot overwrite a newer one.
  const laneSeq = useRef<Record<IssueLane, number>>({ todo: 0, in_progress: 0, done: 0 })
  // The caller builds filter fresh each render; its content is its identity.
  const filterKey = JSON.stringify(filter)
  const stableFilter = useMemo(() => JSON.parse(filterKey) as GetIssuesOptions, [filterKey])

  const patchLane = useCallback((lane: IssueLane, patch: Partial<LaneState>) => {
    setLanes((prev) => ({ ...prev, [lane]: { ...prev[lane], ...patch } }))
  }, [])

  const loadLane = useCallback(
    (lane: IssueLane, limit: number) => {
      if (!token || !spaceId) return Promise.resolve()
      const seq = ++laneSeq.current[lane]
      patchLane(lane, { loading: true, error: null, moreError: null })
      return getIssues(spaceId, token, { ...stableFilter, status: lane, limit, offset: 0 })
        .then((res) => {
          if (seq !== laneSeq.current[lane]) return
          patchLane(lane, { items: res.issues.map(apiIssueToIssue), total: res.total, loading: false })
        })
        .catch((err) => {
          if (seq !== laneSeq.current[lane]) return
          patchLane(lane, { error: classifyError(err, `Failed to load ${statusLabel(lane)} issues`), loading: false })
        })
    },
    [token, spaceId, stableFilter, patchLane],
  )

  useEffect(() => {
    setLanes(initialLanes())
    setAnnouncement(null)
    for (const lane of ISSUE_LANES) void loadLane(lane, LANE_PAGE_SIZE)
  }, [loadLane])

  function loadMore(lane: IssueLane) {
    const current = lanes[lane]
    if (!token || !spaceId || !current.items || current.loadingMore) return
    const seq = laneSeq.current[lane]
    patchLane(lane, { loadingMore: true, moreError: null })
    getIssues(spaceId, token, { ...stableFilter, status: lane, limit: LANE_PAGE_SIZE, offset: current.items.length })
      .then((res) => {
        if (seq !== laneSeq.current[lane]) return
        setLanes((prev) => ({
          ...prev,
          [lane]: {
            ...prev[lane],
            items: appendPage(prev[lane].items ?? [], res.issues.map(apiIssueToIssue)),
            total: res.total,
            loadingMore: false,
          },
        }))
      })
      .catch((err) => {
        if (seq !== laneSeq.current[lane]) return
        patchLane(lane, { loadingMore: false, moreError: getErrorMessage(err, "Failed to load more issues") })
      })
  }

  function reload(lane: IssueLane) {
    return loadLane(lane, reloadLimit(lanes[lane].items?.length ?? 0))
  }

  async function move(issue: Issue, to: IssueLane) {
    if (!token || !spaceId || moving[issue.id]) return
    const from = issue.status
    setMoving((prev) => ({ ...prev, [issue.id]: to }))
    setAnnouncement(null)
    try {
      // The version the card was loaded with: a newer edit is refused, never
      // overwritten. Only status is sent, so Owner and Executor are untouched.
      const updated = apiIssueToIssue(await updateIssue(spaceId, issue.id, { version: issue.version, status: to }, token))
      // The accepted response is authoritative, so show it at once; the
      // reloads below reconcile totals and anything else that moved.
      setLanes((prev) => ({
        ...prev,
        [from]: { ...prev[from], items: (prev[from].items ?? []).filter((item) => item.id !== issue.id), total: Math.max(0, prev[from].total - 1) },
        [to]: { ...prev[to], items: [updated, ...(prev[to].items ?? []).filter((item) => item.id !== issue.id)], total: prev[to].total + 1 },
      }))
      focusTarget.current = { issueId: issue.id, lane: to }
      setAnnouncement({ tone: "status", text: `Moved “${issue.title}” to ${statusLabel(to)}.` })
      await Promise.all([reload(from), reload(to)])
    } catch (err) {
      focusTarget.current = { issueId: issue.id, lane: from }
      if (err instanceof ApiRequestError && err.status === 409) {
        // Not retried: the reader decided against state that no longer
        // exists, so they decide again against the reloaded board.
        setAnnouncement({
          tone: "alert",
          text: `“${issue.title}” changed since the board loaded it, so it was not moved. The board has been reloaded — check it and move again if needed.`,
        })
        await Promise.all(ISSUE_LANES.map((lane) => reload(lane)))
      } else {
        setAnnouncement({ tone: "alert", text: `Couldn’t move “${issue.title}”: ${getErrorMessage(err, "the update failed")}` })
      }
    } finally {
      setMoving((prev) => {
        const next = { ...prev }
        delete next[issue.id]
        return next
      })
    }
  }

  useEffect(() => {
    const target = focusTarget.current
    if (!target || moving[target.issueId]) return
    focusTarget.current = null
    const card = document.querySelector<HTMLElement>(`[data-issue-card="${CSS.escape(target.issueId)}"] .issue-board__open`)
    const heading = document.getElementById(laneHeadingId(target.lane))
    ;(card ?? heading)?.focus()
  }, [lanes, moving])

  const failedLanes = ISSUE_LANES.filter((lane) => lanes[lane].items === null && lanes[lane].error)
  const loadedLanes = ISSUE_LANES.filter((lane) => lanes[lane].items !== null)

  return (
    <div className="issue-board">
      {failedLanes.length > 0 && loadedLanes.length > 0 ? (
        <div className="state-alert state-alert--stale" role="status">
          <p className="state-alert__title">Board incomplete</p>
          <p className="state-alert__message">
            {failedLanes.map((lane) => statusLabel(lane)).join(" and ")} couldn’t load, so this board does not show every
            Issue. An empty-looking lane is not proof there is no work in it.
          </p>
        </div>
      ) : null}
      {announcement ? (
        <p className={`issue-board__announcement issue-board__announcement--${announcement.tone}`} role={announcement.tone}>
          {announcement.text}
        </p>
      ) : null}
      <div className="issue-board__lanes">
        {ISSUE_LANES.map((lane) => (
          <BoardLane
            key={lane}
            lane={lane}
            state={lanes[lane]}
            spaceId={spaceId}
            moving={moving}
            ownerLabel={ownerLabel}
            executorLabel={executorLabel}
            onRetry={() => void loadLane(lane, LANE_PAGE_SIZE)}
            onMore={() => loadMore(lane)}
            onMove={(issue, to) => void move(issue, to)}
          />
        ))}
      </div>
    </div>
  )
}

function laneHeadingId(lane: IssueLane): string {
  return `issue-board-lane-${lane}`
}

interface BoardLaneProps {
  lane: IssueLane
  state: LaneState
  spaceId: string
  moving: Record<string, IssueLane>
  ownerLabel: (issue: Issue) => string | null
  executorLabel: (issue: Issue) => string | null
  onRetry: () => void
  onMore: () => void
  onMove: (issue: Issue, to: IssueLane) => void
}

function BoardLane({ lane, state, spaceId, moving, ownerLabel, executorLabel, onRetry, onMore, onMove }: BoardLaneProps) {
  const resource = deriveResourceState({
    loading: state.loading,
    data: state.items,
    error: state.error,
    isEmpty: (items) => items.length === 0,
  })
  const items = state.items ?? []
  const headingId = laneHeadingId(lane)

  return (
    <section className="issue-board__lane" aria-labelledby={headingId} data-lane={lane}>
      <header className="issue-board__lane-head">
        {/* The total is part of the heading, so a lane's name announces how
            much work it holds, not just how much is loaded. */}
        <h2 id={headingId} className="issue-board__lane-title" tabIndex={-1}>
          {statusLabel(lane)}
          {state.items !== null ? <span className="issue-board__count"> {state.total}</span> : null}
        </h2>
      </header>

      {resource.kind === "loading" ? (
        <p className="page-activity__empty">Loading…</p>
      ) : resource.kind === "error" || resource.kind === "forbidden" || resource.kind === "notFound" ? (
        <Alert
          tone={resource.kind}
          message={resource.error.message}
          retry={resource.kind === "forbidden" ? undefined : { label: "Retry", onClick: onRetry }}
        />
      ) : (
        <>
          {resource.kind === "stale" ? (
            <Alert tone="stale" message={resource.error.message} retry={{ label: "Retry", onClick: onRetry }} />
          ) : null}
          {resource.kind === "readyEmpty" ? (
            <p className="page-activity__empty">No issues in {statusLabel(lane)}.</p>
          ) : (
            <ul className="issue-board__cards">
              {items.map((issue) => (
                <li key={issue.id}>
                  <BoardCard
                    issue={issue}
                    spaceId={spaceId}
                    movingTo={moving[issue.id]}
                    owner={ownerLabel(issue)}
                    executor={executorLabel(issue)}
                    onMove={onMove}
                  />
                </li>
              ))}
            </ul>
          )}
          {items.length < state.total ? (
            <div className="issue-board__more">
              <span className="page-activity__meta">
                {items.length} of {state.total}
              </span>
              <Button variant="secondary" size="compact" busy={state.loadingMore} onClick={onMore}>
                Show more
              </Button>
            </div>
          ) : null}
          {state.moreError ? (
            <p className="issue-board__announcement issue-board__announcement--alert" role="alert">
              {state.moreError}
            </p>
          ) : null}
        </>
      )}
    </section>
  )
}

interface BoardCardProps {
  issue: Issue
  spaceId: string
  movingTo: IssueLane | undefined
  owner: string | null
  executor: string | null
  onMove: (issue: Issue, to: IssueLane) => void
}

function BoardCard({ issue, spaceId, movingTo, owner, executor, onMove }: BoardCardProps) {
  return (
    <article className="issue-board__card" data-issue-card={issue.id} aria-busy={movingTo ? true : undefined}>
      <a className="issue-board__open" href={buildHash({ name: "issue", spaceId, issueId: issue.id })}>
        {issue.title}
      </a>
      <dl className="issue-board__facts">
        <div>
          <dt>Owner</dt>
          <dd>{owner ?? "Unowned"}</dd>
        </div>
        <div>
          <dt>Executor</dt>
          <dd>{executor ?? "No executor"}</dd>
        </div>
        {issue.childCount > 0 ? (
          <div>
            <dt>Sub-issues</dt>
            <dd>
              {issue.doneChildCount}/{issue.childCount} done
            </dd>
          </div>
        ) : null}
        <div>
          <dt>Updated</dt>
          <dd>{issue.updatedLabel}</dd>
        </div>
      </dl>
      {/* The named, non-drag path is the move contract: it works the same
          for keyboard, assistive technology, touch, and pointer. */}
      <div className="issue-board__move" role="group" aria-label={`Move “${issue.title}”`}>
        <span className="issue-board__move-label" aria-hidden="true">
          Move to
        </span>
        {ISSUE_LANES.filter((lane) => lane !== issue.status).map((lane) => (
          <Button
            key={lane}
            variant="tertiary"
            size="compact"
            aria-label={`Move to ${statusLabel(lane)}`}
            busy={movingTo === lane}
            disabled={movingTo !== undefined}
            onClick={() => onMove(issue, lane)}
          >
            {statusLabel(lane)}
          </Button>
        ))}
      </div>
    </article>
  )
}
