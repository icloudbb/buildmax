import { useCallback, useEffect, useRef, useState } from "react"
import { classifyError, type RequestError } from "../state/resourceState"
import { useStableT } from "../i18n"

export interface UseAsyncListOptions {
  /** Optional callback when loading state changes (e.g. for parent state). */
  setLoading?: (loading: boolean) => void
  /**
   * Fallback message when the caught error carries none. Default: "Request
   * failed". Read when a fetch fails, so a translated message changing with the
   * language never refetches.
   */
  fallbackMessage?: string
}

/**
 * Fetches a list when deps are valid (enabled). Clears when disabled. refetch() re-runs the fetch.
 *
 * `data` is null until the first successful fetch, then never null again — a
 * failed refetch leaves the previous list in place rather than wiping it, so
 * callers can distinguish "not loaded yet" / Stale / Error via
 * deriveResourceState instead of reading a failure as an empty list.
 */
export function useAsyncList<T, U>(
  fetchFn: () => Promise<T[]>,
  map: (raw: T[]) => U[],
  deps: unknown[],
  enabled: boolean,
  options?: UseAsyncListOptions
): { data: U[] | null; loading: boolean; error: RequestError | null; refetch: () => Promise<void> } {
  const setExternalLoading = options?.setLoading
  const stableT = useStableT()
  const fallbackMessageRef = useRef(options?.fallbackMessage)
  fallbackMessageRef.current = options?.fallbackMessage
  const [data, setData] = useState<U[] | null>(null)
  const [loading, setLoadingState] = useState(false)
  const [error, setError] = useState<RequestError | null>(null)
  const fetchFnRef = useRef(fetchFn)
  const mapRef = useRef(map)
  fetchFnRef.current = fetchFn
  mapRef.current = map

  const runFetch = useCallback((): Promise<void> => {
    setLoadingState(true)
    setError(null)
    setExternalLoading?.(true)
    return fetchFnRef
      .current()
      .then((raw) => {
        setData(mapRef.current(raw))
        setError(null)
      })
      .catch((err) => {
        // Prior data (if any) is left in place: a failed refresh is Stale, not Error.
        setError(classifyError(err, fallbackMessageRef.current ?? stableT("common.requestFailed")))
      })
      .finally(() => {
        setLoadingState(false)
        setExternalLoading?.(false)
      })
  }, [setExternalLoading, stableT])

  useEffect(() => {
    if (!enabled) {
      setData(null)
      setError(null)
      setLoadingState(false)
      setExternalLoading?.(false)
      return
    }
    runFetch()
    // eslint-disable-next-line react-hooks/exhaustive-deps -- deps are intentional
  }, [enabled, runFetch, setExternalLoading, ...deps])

  const refetch = useCallback((): Promise<void> => {
    if (!enabled) return Promise.resolve()
    return runFetch()
  }, [enabled, runFetch])

  return { data, loading, error, refetch }
}
