import { ApiRequestError } from "../lib/api/client"
import { detectLocale } from "@buildmax/gui"
import { getErrorMessage } from "../lib/errorMessage"
import { translate } from "../i18n"

/**
 * Cause of a failed resource request, derived from the HTTP status when known.
 * "error" covers everything transient: network failure, 5xx, or any status
 * Portal does not give special meaning to.
 */
export type RequestErrorKind = "forbidden" | "notFound" | "error"

export interface RequestError {
  readonly kind: RequestErrorKind
  readonly message: string
}

/** Maps a caught fetch error to a RequestError. 403 -> forbidden, 404 -> notFound, else -> error. */
export function classifyError(
  err: unknown,
  fallback = translate(detectLocale(), "common.requestFailed"),
): RequestError {
  const message = getErrorMessage(err, fallback)
  if (err instanceof ApiRequestError) {
    if (err.status === 403) return { kind: "forbidden", message }
    if (err.status === 404) return { kind: "notFound", message }
  }
  return { kind: "error", message }
}

/**
 * One explicit state for a remote resource boundary, matching the state model in
 * docs/design/portal-state-and-permission-feedback.md. `kind` discriminates the
 * union so a page cannot render two of these at once (e.g. readyEmpty and error).
 */
export type ResourceState<T> =
  | { kind: "loading" }
  | { kind: "ready"; data: T }
  | { kind: "readyEmpty" }
  | { kind: "refreshing"; data: T }
  | { kind: "stale"; data: T; error: RequestError }
  | { kind: "error"; error: RequestError }
  | { kind: "forbidden"; error: RequestError }
  | { kind: "notFound"; error: RequestError }

export interface DeriveResourceStateInput<T> {
  /** A request for this resource key is in flight. */
  loading: boolean
  /** The last successfully loaded data for this resource key, if any. */
  data: T | null
  /** The error from the most recent failed request for this resource key, if any. */
  error: RequestError | null
  /** True when `data` has no content worth presenting as Ready (e.g. an empty list). */
  isEmpty: (data: T) => boolean
}

/**
 * Pure derivation of the ResourceState a page should render from request
 * primitives. Hooks and reducers own tracking loading/data/error per resource
 * key; this function only decides which single state that combination means.
 *
 * Callers must clear `data` when the resource key changes (e.g. a route id)
 * before the next fetch starts, so that a failure here only ever produces
 * Stale for a failed re-fetch of the *same* resource, never a stale object
 * left over from a previous key. A forbidden or not-found result on refresh
 * still overrides Stale: a permission or existence change is never presented
 * as "old data, retry available".
 */
export function deriveResourceState<T>(input: DeriveResourceStateInput<T>): ResourceState<T> {
  const { loading, data, error, isEmpty } = input
  const hasPriorData = data !== null

  if (loading) {
    return hasPriorData ? { kind: "refreshing", data: data as T } : { kind: "loading" }
  }

  if (error) {
    if (error.kind === "forbidden") return { kind: "forbidden", error }
    if (error.kind === "notFound") return { kind: "notFound", error }
    if (hasPriorData) return { kind: "stale", data: data as T, error }
    return { kind: "error", error }
  }

  if (!hasPriorData) return { kind: "readyEmpty" }
  return isEmpty(data as T) ? { kind: "readyEmpty" } : { kind: "ready", data: data as T }
}
