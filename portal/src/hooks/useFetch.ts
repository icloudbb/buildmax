import { useCallback, useEffect, useRef, useState } from "react"
import { getErrorMessage } from "../lib/errorMessage"
import { classifyError, type RequestErrorKind } from "../state/resourceState"
import { useStableT } from "../i18n"

export interface UseFetchOptions {
  /** When false, no fetch runs and data/error are cleared. Default true. */
  enabled?: boolean
  /** Map caught error to message. Default: err.message or "Request failed". */
  errorMessage?: (err: unknown) => string
}

export interface UseFetchResult<T> {
  data: T | null
  loading: boolean
  error: string | null
  /** classifyError's read of the same failure — forbidden/notFound/error — for callers that branch on it. */
  errorKind: RequestErrorKind | null
  refetch: () => void
}

/**
 * Fetches when deps change (and enabled). Cancels on unmount or when deps change.
 * refetch() re-runs the fetch with the latest fetchFn.
 */
export function useFetch<T>(
  fetchFn: () => Promise<T>,
  deps: unknown[],
  options: UseFetchOptions = {}
): UseFetchResult<T> {
  const stableT = useStableT()
  const { enabled = true, errorMessage = (e) => getErrorMessage(e, stableT("common.requestFailed")) } = options
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [errorKind, setErrorKind] = useState<RequestErrorKind | null>(null)
  const fetchFnRef = useRef(fetchFn)
  fetchFnRef.current = fetchFn
  const errorMessageRef = useRef(errorMessage)
  errorMessageRef.current = errorMessage

  const runFetch = useCallback(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    setErrorKind(null)
    fetchFnRef
      .current()
      .then((value) => {
        if (!cancelled) setData(value)
      })
      .catch((err) => {
        if (!cancelled) {
          setError(errorMessageRef.current(err))
          setErrorKind(classifyError(err).kind)
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!enabled) {
      setData(null)
      setLoading(false)
      setError(null)
      setErrorKind(null)
      return
    }
    return runFetch()
    // eslint-disable-next-line react-hooks/exhaustive-deps -- deps are intentional
  }, [enabled, runFetch, ...deps])

  const refetch = useCallback(() => {
    if (!enabled) return
    runFetch()
  }, [enabled, runFetch])

  return { data, loading, error, errorKind, refetch }
}
