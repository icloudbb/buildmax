import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { getSpaces } from "../features/spaces/api"
import { getSpaceMembers } from "../features/spaces/api"
import type { ApiSpaceMember } from "../lib/api/types"
import {
  clearStoredCurrentSpaceId,
  getStoredCurrentSpaceId,
  setStoredCurrentSpaceId,
} from "../lib/storage/currentSpaceStorage"
import { classifyError, deriveResourceState, type RequestError, type ResourceState } from "../state/resourceState"
import { derivePermissionState, type PermissionState } from "../state/permissionState"
import { useAuth } from "./AuthContext"

export interface SpaceSummary {
  id: string
  name: string
  personalForUserId?: string | null
}

interface SpaceContextValue {
  spaces: SpaceSummary[]
  /** Resolved state of the spaces list itself — see docs/design/portal-state-and-permission-feedback.md. */
  spacesState: ResourceState<SpaceSummary[]>
  currentSpaceId: string | null
  currentSpace: SpaceSummary | null
  currentSpaceMembers: ApiSpaceMember[]
  currentUserRole: string | null
  /** Whether the current Space's membership (and so currentUserRole) is still being resolved. */
  roleLoading: boolean
  /** Set when the membership/role lookup itself failed — distinct from a resolved non-member. */
  roleError: RequestError | null
  loading: boolean
  setCurrentSpaceId: (spaceId: string) => void
  refetchSpaces: (preferredSpaceId?: string | null) => Promise<void>
}

const SpaceContext = createContext<SpaceContextValue | null>(null)

// Stable identity so `spaces` doesn't churn every render while spacesData is
// still null (before the first successful fetch).
const EMPTY_SPACES: SpaceSummary[] = []

function chooseCurrentSpace(spaces: SpaceSummary[]): string | null {
  if (spaces.length === 0) return null
  const stored = getStoredCurrentSpaceId()
  if (stored && spaces.some((space) => space.id === stored)) return stored
  return spaces[0].id
}

function normalizeSpaceName(space: { name: string; personal_for_user_id?: string | null }): string {
  if (space.personal_for_user_id && space.name.trim() === "My Space") {
    return "My Space"
  }
  return space.name
}

export function SpaceProvider({ children }: { children: ReactNode }) {
  const { token, user, status } = useAuth()
  // null means "not yet successfully fetched", distinct from [] meaning the
  // account genuinely has no Spaces. See deriveResourceState.
  const [spacesData, setSpacesData] = useState<SpaceSummary[] | null>(null)
  const [currentSpaceId, setCurrentSpaceIdState] = useState<string | null>(getStoredCurrentSpaceId)
  const [currentSpaceMembers, setCurrentSpaceMembers] = useState<ApiSpaceMember[]>([])
  const [loading, setLoading] = useState(false)
  const [spacesError, setSpacesError] = useState<RequestError | null>(null)
  const [roleLoading, setRoleLoading] = useState(false)
  const [roleError, setRoleError] = useState<RequestError | null>(null)

  const refetchSpaces = useCallback(async (preferredSpaceId?: string | null) => {
    if (!token) {
      setSpacesData(null)
      setSpacesError(null)
      setCurrentSpaceIdState(null)
      setCurrentSpaceMembers([])
      clearStoredCurrentSpaceId()
      return
    }

    setLoading(true)
    setSpacesError(null)
    try {
      const nextSpaces = await getSpaces(token)
      const mapped = nextSpaces.map((space) => ({
        id: space.id,
        name: normalizeSpaceName(space),
        personalForUserId: space.personal_for_user_id ?? null,
      }))
      setSpacesData(mapped)
      const nextCurrentSpaceId =
        preferredSpaceId && mapped.some((space) => space.id === preferredSpaceId)
          ? preferredSpaceId
          : chooseCurrentSpace(mapped)
      setCurrentSpaceIdState(nextCurrentSpaceId)
      setStoredCurrentSpaceId(nextCurrentSpaceId)
    } catch (err) {
      // Prior spacesData (if any) is intentionally left in place: a failed
      // refresh of an already-loaded list is Stale, not Error.
      setSpacesError(classifyError(err, "Failed to load spaces"))
    } finally {
      setLoading(false)
    }
  }, [token])

  useEffect(() => {
    // Wait for the session restore to settle before fetching or clearing. On a
    // reload the token is briefly null while it is exchanged from the refresh
    // cookie; acting on that transient anonymous window would clear the stored
    // Space selection, and then a Space list that also fails to load would have
    // no id left to fall back on — leaving a whole-page error where the shell
    // should still render the space with an "unavailable" label.
    if (status !== "ready") return
    void refetchSpaces()
  }, [refetchSpaces, status])

  useEffect(() => {
    if (!token || !currentSpaceId) {
      setCurrentSpaceMembers([])
      setRoleError(null)
      setRoleLoading(false)
      return
    }
    let cancelled = false
    setRoleLoading(true)
    setRoleError(null)
    getSpaceMembers(currentSpaceId, token)
      .then((members) => {
        if (!cancelled) setCurrentSpaceMembers(members)
      })
      .catch((err) => {
        // Members (and so currentUserRole) are left as-is on failure: a lookup
        // failure must not read the same as "confirmed not a member".
        if (!cancelled) setRoleError(classifyError(err, "Failed to load membership"))
      })
      .finally(() => {
        if (!cancelled) setRoleLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [token, currentSpaceId])

  const spaces = spacesData ?? EMPTY_SPACES

  const setCurrentSpaceId = useCallback(
    (spaceId: string) => {
      if (!spaces.some((space) => space.id === spaceId)) return
      setCurrentSpaceIdState(spaceId)
      setStoredCurrentSpaceId(spaceId)
    },
    [spaces]
  )

  const currentSpace = useMemo(
    () => spaces.find((space) => space.id === currentSpaceId) ?? null,
    [spaces, currentSpaceId]
  )

  const currentUserRole = useMemo(
    () => currentSpaceMembers.find((member) => member.user_id === user?.id)?.role ?? null,
    [currentSpaceMembers, user?.id],
  )

  const spacesState = useMemo(
    () =>
      deriveResourceState({
        loading,
        data: spacesData,
        error: spacesError,
        isEmpty: (data) => data.length === 0,
      }),
    [loading, spacesData, spacesError]
  )

  const value: SpaceContextValue = {
    spaces,
    spacesState,
    currentSpaceId,
    currentSpace,
    currentSpaceMembers,
    currentUserRole,
    roleLoading,
    roleError,
    loading,
    setCurrentSpaceId,
    refetchSpaces,
  }

  return (
    <SpaceContext.Provider value={value}>
      {children}
    </SpaceContext.Provider>
  )
}

export function useSpace(): SpaceContextValue {
  const ctx = useContext(SpaceContext)
  if (!ctx) throw new Error("useSpace must be used within SpaceProvider")
  return ctx
}

/**
 * Derives a PermissionState for one capability from the current Space's
 * shared role-lookup primitives — see
 * docs/design/portal-state-and-permission-feedback.md#permission-model.
 * `allowed` is the caller's own check against `currentUserRole` (e.g.
 * `currentUserRole === "owner" || currentUserRole === "admin"`); this hook
 * only combines it with whether that role is still loading or failed to
 * load, so "checking" and "lookup failed" are never read as "denied".
 */
export function useSpaceCapability(allowed: boolean): PermissionState {
  const { roleLoading, roleError } = useSpace()
  return useMemo(
    () => derivePermissionState({ loading: roleLoading, lookupFailed: roleError !== null, allowed }),
    [roleLoading, roleError, allowed]
  )
}
