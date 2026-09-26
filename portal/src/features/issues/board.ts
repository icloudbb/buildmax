import type { Issue, IssueCollectionQuery } from "../../lib/types"
import type { GetIssuesOptions } from "./api"

/**
 * The Board's lanes are the Issue statuses, in domain order. Lane membership
 * is always read from `issue.status`; there is no lane value of its own. See
 * docs/design/portal-work-and-execution-experience.md.
 */
export const ISSUE_LANES = ["todo", "in_progress", "done"] as const
export type IssueLane = (typeof ISSUE_LANES)[number]

/** A Board lane's first page and the step of each "Show more". */
export const LANE_PAGE_SIZE = 20
/** The server's ceiling for one list request (httputil.ListPageMax). */
export const LANE_RELOAD_MAX = 100

/**
 * The server query shared by List and every Board lane, so both views show the
 * same Issue identities under the same filters. Top-level only: a sub-Issue
 * is part of its parent's breakdown, not a peer card.
 */
export function collectionFilter(query: IssueCollectionQuery): GetIssuesOptions {
  const out: GetIssuesOptions = { parentId: "none" }
  if (query.owner) out.owner = query.owner
  if (query.executor) {
    const sep = query.executor.indexOf(":")
    const kind = query.executor.slice(0, sep)
    if (sep > 0 && (kind === "agent" || kind === "workflow")) {
      out.executorKind = kind
      out.executorId = query.executor.slice(sep + 1)
    }
  }
  return out
}

/**
 * Appends a further page, skipping Issues already shown. Offsets shift when an
 * Issue changes lane or is updated between requests, and one Issue appearing
 * twice would read as two pieces of work.
 */
export function appendPage(shown: Issue[], page: Issue[]): Issue[] {
  const seen = new Set(shown.map((issue) => issue.id))
  return [...shown, ...page.filter((issue) => !seen.has(issue.id))]
}

/**
 * How many Issues a lane refetches after a move: as many as the reader had
 * already expanded, so reconciling does not collapse the lane back to its
 * first page.
 */
export function reloadLimit(shown: number): number {
  return Math.min(LANE_RELOAD_MAX, Math.max(LANE_PAGE_SIZE, shown))
}
