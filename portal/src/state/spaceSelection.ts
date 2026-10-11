import type { RequestError } from "./resourceState"

/**
 * What a request to make `spaceId` the current Space should do.
 *
 * - `select`: the Space is in the list; adopt it.
 * - `relist`: the list predates the Space -- an invitation accepted or a Space
 *   created since it loaded -- so adopt it and list again to learn its name.
 * - `ignore`: the list has not loaded yet (its own load decides), or this
 *   Space was already listed again and is still absent, so the caller cannot
 *   reach it and asking once more would loop.
 */
export type SpaceSelection = "select" | "relist" | "ignore"

export function planSpaceSelection(
  knownSpaceIds: readonly string[] | null,
  spaceId: string,
  relistedFor: string | null,
): SpaceSelection {
  if (knownSpaceIds === null) return "ignore"
  if (knownSpaceIds.includes(spaceId)) return "select"
  if (relistedFor === spaceId) return "ignore"
  return "relist"
}

/** The member list fetched for one Space, tagged with that Space. */
export interface MembershipLookup<M> {
  spaceId: string
  members: M[] | null
  loading: boolean
  error: RequestError | null
}

export interface CurrentMembership<M> {
  members: M[]
  loading: boolean
  error: RequestError | null
}

const NO_MEMBERS: never[] = []

/**
 * The current Space's membership, from a lookup that may belong to another
 * Space. A lookup for any other Space reads as still loading, never as this
 * Space's roster: a role is only ever read from the Space it was granted in.
 */
export function currentMembership<M>(
  lookup: MembershipLookup<M> | null,
  currentSpaceId: string | null,
): CurrentMembership<M> {
  if (!currentSpaceId) return { members: NO_MEMBERS, loading: false, error: null }
  if (!lookup || lookup.spaceId !== currentSpaceId) return { members: NO_MEMBERS, loading: true, error: null }
  return { members: lookup.members ?? NO_MEMBERS, loading: lookup.loading, error: lookup.error }
}
