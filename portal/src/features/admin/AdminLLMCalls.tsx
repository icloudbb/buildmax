import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import type { ApiAdminLLMCall, ApiAdminLLMCallCost } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { formatEventTime } from "../audit/describe"
import { searchAdminLLMCalls } from "./api"

const PAGE_SIZE = 50

/** Filters the ledger supports. Empty strings mean no bound. */
interface CallFilters {
  userId: string
  model: string
  status: string
  surface: string
}

const EMPTY: CallFilters = { userId: "", model: "", status: "", surface: "" }

/**
 * A call's spend, rendered from its own rate snapshot. Nano-units divide down to
 * the currency; four places keeps a sub-cent call from reading as free.
 */
function formatCost(cost?: ApiAdminLLMCallCost): string | null {
  if (!cost) return null
  return `${(cost.total / 1e9).toFixed(4)} ${cost.currency}`
}

/** total_tokens, falling back to the parts when a provider reported no total. */
function formatTokens(call: ApiAdminLLMCall): string | null {
  if (call.total_tokens != null) return `${call.total_tokens.toLocaleString()} tok`
  const prompt = call.prompt_tokens ?? 0
  const completion = call.completion_tokens ?? 0
  if (prompt === 0 && completion === 0) return null
  return `${(prompt + completion).toLocaleString()} tok`
}

/**
 * AdminLLMCalls reads the managed call ledger across every user and space.
 *
 * It is the deployment-wide read the space-scoped route cannot do: that one
 * answers one run whose space the caller belongs to. This answers what the
 * deployment spent, and on which approved model. It carries no prompts and no
 * generated content — the ledger was built as an accounting record, not a
 * transcript, and reading it across spaces must not become a way to read across
 * them.
 */
export function AdminLLMCalls({ token }: { token: string | null }) {
  const [calls, setCalls] = useState<ApiAdminLLMCall[]>([])
  const [total, setTotal] = useState(0)
  const [filters, setFilters] = useState<CallFilters>(EMPTY)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    (active: CallFilters, offset: number) => {
      if (!token) return
      setLoading(true)
      setError(null)
      searchAdminLLMCalls(token, {
        user_id: active.userId || undefined,
        model: active.model || undefined,
        status: active.status || undefined,
        surface: active.surface || undefined,
        limit: PAGE_SIZE,
        offset,
      })
        .then((res) => {
          setCalls((prev) => (offset === 0 ? res.calls : [...prev, ...res.calls]))
          setTotal(res.total)
        })
        .catch((err) => setError(getErrorMessage(err, "Failed to load the call ledger")))
        .finally(() => setLoading(false))
    },
    [token],
  )

  useEffect(() => {
    load(EMPTY, 0)
  }, [load])

  function apply(next: CallFilters) {
    setFilters(next)
    load(next, 0)
  }

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">LLM calls</h2>
            <p className="settings-page__section-copy">
              What this deployment spent on managed inference, across every space, and on
              which model. It carries no prompts and no generated content.
            </p>
          </div>
        </div>

        <form
          className="admin-toolbar"
          onSubmit={(e) => {
            e.preventDefault()
            apply(filters)
          }}
        >
          <input
            className="admin-input"
            value={filters.userId}
            placeholder="User id"
            aria-label="Filter by user id"
            onChange={(e) => setFilters({ ...filters, userId: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.model}
            placeholder="Model, e.g. gpt-luna"
            aria-label="Filter by model"
            onChange={(e) => setFilters({ ...filters, model: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.status}
            placeholder="Status, e.g. SUCCEEDED"
            aria-label="Filter by status"
            onChange={(e) => setFilters({ ...filters, status: e.target.value })}
          />
          <input
            className="admin-input"
            value={filters.surface}
            placeholder="Surface, e.g. worker"
            aria-label="Filter by surface"
            onChange={(e) => setFilters({ ...filters, surface: e.target.value })}
          />
          <Button type="submit" variant="primary" disabled={loading}>
            Search
          </Button>
          <Button variant="tertiary" onClick={() => apply(EMPTY)}>
            Clear
          </Button>
        </form>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {calls.length === 0 && !loading ? (
          <p className="admin-empty">No calls match.</p>
        ) : (
          <ul className="admin-list">
            {calls.map((call) => {
              const tokens = formatTokens(call)
              const cost = formatCost(call.cost)
              const failed = call.status !== "SUCCEEDED"
              return (
                <li key={call.id} className="admin-list__row">
                  <span className="admin-list__main">
                    {call.model || call.upstream_model || "unknown model"}
                    {failed ? <span className="admin-pill">{call.status}</span> : null}
                  </span>
                  <span className="admin-list__meta">
                    {call.user_id ? `${call.user_id} · ` : ""}
                    {call.surface || "server"}
                    {call.provider_type ? ` · ${call.provider_type}` : ""}
                    {tokens ? ` · ${tokens}` : ""}
                    {cost ? ` · ${cost}` : ""}
                    {call.error_class ? ` · ${call.error_class}` : ""}
                    {" · "}
                    {formatEventTime(call.accepted_at)}
                  </span>
                </li>
              )
            })}
          </ul>
        )}

        {calls.length < total ? (
          <Button
            variant="secondary"
            disabled={loading}
            onClick={() => load(filters, calls.length)}
          >
            Load more ({calls.length} of {total})
          </Button>
        ) : null}
      </section>
    </div>
  )
}
