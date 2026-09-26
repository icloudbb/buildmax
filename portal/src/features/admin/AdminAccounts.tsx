import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useRef, useState } from "react"
import type { ApiAdminSession, ApiAdminUser, ApiAdminUserDetail } from "../../lib/api/types"
import { DeactivationImpactModal } from "./DeactivationImpactModal"
import { describeDisableOutcome } from "./disableOutcome"
import { getErrorMessage } from "../../lib/errorMessage"
import { navigate } from "../../router"
import { pageWindow } from "./pagination"
import {
  createAdminUser,
  getAdminUser,
  issueAdminLoginCode,
  listAdminUserSessions,
  listAdminUsers,
  revokeAdminUserSession,
  revokeAdminUserSessions,
  setAdminUserDisabled,
} from "./api"

const PAGE_SIZE = 50

interface AccountFilters {
  status: string
  hasPassword: string
  systemRole: string
  platform: string
  // Whole days in the operator's zone, as the date input yields them
  // (YYYY-MM-DD). Converted to instants only when the request is built.
  lastLoginAfter: string
  lastLoginBefore: string
}

const emptyFilters: AccountFilters = {
  status: "",
  hasPassword: "",
  systemRole: "",
  platform: "",
  lastLoginAfter: "",
  lastLoginBefore: "",
}

// The date inputs are whole days in the operator's zone. "after" is that day's
// start; "before" is the start of the day after the chosen one, so the chosen
// day falls inside the range rather than being excluded at its own midnight.
function dayStartISO(date: string): string | undefined {
  if (!date) return undefined
  const d = new Date(`${date}T00:00:00`)
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString()
}

function nextDayStartISO(date: string): string | undefined {
  if (!date) return undefined
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime())) return undefined
  d.setDate(d.getDate() + 1)
  return d.toISOString()
}

function accountState(user: ApiAdminUser): { label: string; disabled: boolean } {
  if (user.disabled_at) return { label: "Disabled", disabled: true }
  if (!user.has_password) return { label: "No password yet", disabled: false }
  return { label: "Active", disabled: false }
}

function whenever(rfc3339?: string): string {
  return rfc3339 ? new Date(rfc3339).toLocaleString() : "never"
}

/**
 * AdminAccounts is the page an operator opens on a joiner or a leaver day.
 *
 * The destructive actions state what they will do before they do it. Disabling
 * revokes sessions and stops queued work; a login code is shown once and is
 * recoverable nowhere. Both are said in the confirm, not discovered afterwards.
 */
export function AdminAccounts({
  token,
  selectedUserId,
}: {
  token: string | null
  selectedUserId?: string
}) {
  const [users, setUsers] = useState<ApiAdminUser[]>([])
  const [total, setTotal] = useState(0)
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [selected, setSelected] = useState<ApiAdminUserDetail | null>(null)
  const [sessions, setSessions] = useState<ApiAdminSession[]>([])
  const detailRef = useRef<HTMLElement | null>(null)

  // The detail panel renders below the list, so on a long list it opens off
  // screen and the click reads as having done nothing.
  useEffect(() => {
    if (selected) detailRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [selected])
  const [newEmail, setNewEmail] = useState("")
  const [busy, setBusy] = useState(false)
  const [disableTarget, setDisableTarget] = useState<ApiAdminUser | null>(null)
  const [cleanupRetry, setCleanupRetry] = useState<{
    user: ApiAdminUser
    retireWebhookKeys: boolean
  } | null>(null)
  const [loginCode, setLoginCode] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)
  const [filters, setFilters] = useState<AccountFilters>(emptyFilters)

  const load = useCallback(
    (q: string, off: number, f: AccountFilters) => {
      if (!token) return
      setLoading(true)
      setError(null)
      listAdminUsers(token, {
        q,
        limit: PAGE_SIZE,
        offset: off,
        status: f.status || undefined,
        has_password: f.hasPassword || undefined,
        system_role: f.systemRole || undefined,
        platform: f.platform || undefined,
        last_login_after: dayStartISO(f.lastLoginAfter),
        last_login_before: nextDayStartISO(f.lastLoginBefore),
      })
        .then((res) => {
          setUsers(res.users)
          setTotal(res.total)
          setOffset(off)
        })
        .catch((err) => setError(getErrorMessage(err, "Failed to load accounts")))
        .finally(() => setLoading(false))
    },
    [token],
  )

  useEffect(() => {
    load("", 0, emptyFilters)
  }, [load])

  const openDetail = useCallback(
    (userId: string) => {
      if (!token) return
      setLoginCode(null)
      getAdminUser(token, userId)
        .then(setSelected)
        .catch((err) => setError(getErrorMessage(err, "Failed to load the account")))
      listAdminUserSessions(token, userId)
        .then((res) => setSessions(res.sessions))
        .catch(() => setSessions([]))
    },
    [token],
  )

  // The URL owns which account is open, so the detail panel survives a reload
  // and can be linked. Selecting a row navigates; this reflects the result.
  useEffect(() => {
    if (!selectedUserId) {
      setSelected(null)
      setSessions([])
      setLoginCode(null)
      return
    }
    openDetail(selectedUserId)
  }, [selectedUserId, openDetail])

  async function act<T>(run: () => Promise<T>, done: (result: T) => string): Promise<void> {
    if (!token) return
    setBusy(true)
    setError(null)
    setNotice(null)
    setCleanupRetry(null)
    try {
      const result = await run()
      setNotice(done(result))
      load(query, offset, filters)
      if (selected) openDetail(selected.id)
    } catch (err) {
      setError(getErrorMessage(err, "The action did not complete"))
    } finally {
      setBusy(false)
    }
  }

  // The joiner flow: create and issue-a-code stay separate calls with separate
  // audit events, but the operator is walked from one to the next. Creating an
  // account lands on its detail — where the login-code button is — with the
  // reason it still cannot sign in stated, rather than leaving the operator to
  // find the account again for step two.
  async function createJoiner(email: string): Promise<void> {
    if (!token) return
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      const user = await createAdminUser(token, email)
      setNewEmail("")
      load(query, 0, filters)
      navigate({ name: "admin", section: "accounts", userId: user.id })
      setNotice(
        `Created ${user.email}. It cannot sign in yet — issue a login code below ` +
          "and deliver it over a channel you trust.",
      )
    } catch (err) {
      setError(getErrorMessage(err, "The account was not created"))
    } finally {
      setBusy(false)
    }
  }

  // A filter change always returns to the first page: the offset it was on may
  // not exist in the narrower result.
  function applyFilter(patch: Partial<AccountFilters>) {
    const next = { ...filters, ...patch }
    setFilters(next)
    load(query, 0, next)
  }

  // Disabling is a guided, orchestrated step: the operator previews the impact
  // and chooses suspension vs leaver in the modal, then this runs the disable and
  // reports what its cleanup did. A partly failed cleanup leaves the account
  // disabled, and the account then offers Enable rather than Disable, so the
  // retry is offered here with the same choice.
  function runDisable(user: ApiAdminUser, retireWebhookKeys: boolean): void {
    act(
      () => setAdminUserDisabled(token!, user.id, true, { retireWebhookKeys }),
      (after) => {
        const outcome = describeDisableOutcome(after)
        setCleanupRetry(outcome.incomplete ? { user, retireWebhookKeys } : null)
        return outcome.message
      },
    )
    setDisableTarget(null)
  }

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Accounts</h2>
            <p className="settings-page__section-copy">
              {total} account{total === 1 ? "" : "s"} in this deployment.
            </p>
          </div>
        </div>

        <form
          className="admin-toolbar"
          onSubmit={(e) => {
            e.preventDefault()
            load(query, 0, filters)
          }}
        >
          <input
            className="admin-input"
            type="search"
            value={query}
            placeholder="Search by email"
            aria-label="Search accounts by email"
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={loading}>
            Search
          </Button>
        </form>

        <div className="admin-toolbar" role="group" aria-label="Filter accounts">
          <select
            className="admin-input"
            aria-label="Filter by status"
            value={filters.status}
            onChange={(e) => applyFilter({ status: e.target.value })}
          >
            <option value="">Any status</option>
            <option value="enabled">Enabled</option>
            <option value="disabled">Disabled</option>
          </select>
          <select
            className="admin-input"
            aria-label="Filter by password state"
            value={filters.hasPassword}
            onChange={(e) => applyFilter({ hasPassword: e.target.value })}
          >
            <option value="">Any password</option>
            <option value="true">Has a password</option>
            <option value="false">No password yet</option>
          </select>
          <select
            className="admin-input"
            aria-label="Filter by system role"
            value={filters.systemRole}
            onChange={(e) => applyFilter({ systemRole: e.target.value })}
          >
            <option value="">Any role</option>
            <option value="system_admin">Administrators</option>
          </select>
          <select
            className="admin-input"
            aria-label="Filter by last-login platform"
            value={filters.platform}
            onChange={(e) => applyFilter({ platform: e.target.value })}
          >
            <option value="">Any platform</option>
            <option value="portal">Portal</option>
            <option value="cli">CLI</option>
            <option value="desktop">Desktop</option>
          </select>
          <label className="admin-field">
            <span className="admin-field__label">Signed in after</span>
            <input
              className="admin-input"
              type="date"
              value={filters.lastLoginAfter}
              max={filters.lastLoginBefore || undefined}
              onChange={(e) => applyFilter({ lastLoginAfter: e.target.value })}
            />
          </label>
          <label className="admin-field">
            <span className="admin-field__label">Signed in before</span>
            <input
              className="admin-input"
              type="date"
              value={filters.lastLoginBefore}
              min={filters.lastLoginAfter || undefined}
              onChange={(e) => applyFilter({ lastLoginBefore: e.target.value })}
            />
          </label>
        </div>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}
        {notice ? (
          <p className="admin-notice" role={cleanupRetry ? "alert" : undefined}>
            {notice}{" "}
            {cleanupRetry ? (
              <Button
                variant="secondary"
                disabled={busy}
                onClick={() => runDisable(cleanupRetry.user, cleanupRetry.retireWebhookKeys)}
              >
                Retry cleanup
              </Button>
            ) : null}
          </p>
        ) : null}

        {loading ? (
          <p className="admin-empty">Loading…</p>
        ) : users.length === 0 ? (
          <p className="admin-empty">No accounts match.</p>
        ) : (
          <ul className="admin-list">
            {users.map((user) => {
              const state = accountState(user)
              return (
                <li key={user.id} className="admin-list__row">
                  <button
                    type="button"
                    className="admin-list__main admin-list__main--action"
                    onClick={() => navigate({ name: "admin", section: "accounts", userId: user.id })}
                  >
                    {user.email}
                  </button>
                  <span
                    className={
                      state.disabled ? "admin-pill admin-pill--bad" : "admin-pill"
                    }
                  >
                    {state.label}
                  </span>
                  <span className="admin-list__meta">
                    last signed in {whenever(user.last_login_at)}
                  </span>
                </li>
              )
            })}
          </ul>
        )}

        {total > PAGE_SIZE
          ? (() => {
              const page = pageWindow(offset, PAGE_SIZE, total)
              return (
                <div className="admin-pager">
                  <Button
                    variant="secondary" size="compact"
                    disabled={loading || !page.hasPrev}
                    onClick={() => load(query, page.prevOffset, filters)}
                  >
                    Previous
                  </Button>
                  <span className="admin-pager__status">
                    {page.from}&ndash;{page.to} of {total}
                  </span>
                  <Button
                    variant="secondary" size="compact"
                    disabled={loading || !page.hasNext}
                    onClick={() => load(query, page.nextOffset, filters)}
                  >
                    Next
                  </Button>
                </div>
              )
            })()
          : null}
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Create an account</h2>
            <p className="settings-page__section-copy">
              Creating an account gives nobody access. Issue a login code afterwards and
              deliver it over a channel you trust — BuildMax has no mail channel.
            </p>
          </div>
        </div>
        <form
          className="admin-toolbar"
          onSubmit={(e) => {
            e.preventDefault()
            const email = newEmail.trim()
            if (!email) return
            createJoiner(email)
          }}
        >
          <input
            className="admin-input"
            type="email"
            value={newEmail}
            placeholder="name@example.com"
            aria-label="Email for the new account"
            onChange={(e) => setNewEmail(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={busy || !newEmail.trim()}>
            Create
          </Button>
        </form>
      </section>

      {selected ? (
        <section className="settings-page__section" ref={detailRef}>
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">{selected.email}</h2>
              <p className="settings-page__section-copy">
                {selected.id} · created {whenever(selected.created_at)} ·{" "}
                {selected.session_count} live session
                {selected.session_count === 1 ? "" : "s"}
              </p>
            </div>
            <Button
              variant="tertiary"
              onClick={() => navigate({ name: "admin", section: "accounts" })}
            >
              Close
            </Button>
          </div>

          {selected.system_roles.length > 0 ? (
            <p className="admin-notice">
              Holds {selected.system_roles.join(", ")} — this account can operate the
              deployment.
            </p>
          ) : null}

          <div className="admin-facts">
            <div className="admin-fact">
              <span className="admin-fact__label">Spaces</span>
              <span className="admin-fact__value">
                {selected.spaces.length === 0
                  ? "none"
                  : selected.spaces.map((space) => `${space.name} (${space.role})`).join(", ")}
              </span>
            </div>
          </div>
          <p className="admin-scope-note">
            Spaces are listed by name and role only. Reaching what is in one still
            requires membership.
          </p>

          <h3 className="settings-page__section-title">Sessions</h3>
          {sessions.length === 0 ? (
            <p className="admin-empty">No live sessions.</p>
          ) : (
            <ul className="admin-list">
              {sessions.map((session) => (
                <li key={session.session_id} className="admin-list__row">
                  <span className="admin-list__main">
                    {session.platform || "unknown platform"}
                    <span className="admin-list__id"> · {session.session_id}</span>
                  </span>
                  <span className="admin-list__meta">
                    signed in {whenever(session.created_at)} · last active{" "}
                    {whenever(session.last_rotated_at)} · expires {whenever(session.expires_at)}
                  </span>
                  <Button
                    variant="danger" size="compact"
                    disabled={busy}
                    onClick={() => {
                      if (
                        !window.confirm(
                          `Sign this ${session.platform || ""} session out?\n\n` +
                            "Only this device is revoked; the account's other sessions stay " +
                            "signed in. An access token it already holds keeps working until it " +
                            "expires.",
                        )
                      )
                        return
                      act(
                        () => revokeAdminUserSession(token!, selected.id, session.session_id),
                        () => "Session revoked.",
                      )
                    }}
                  >
                    Revoke
                  </Button>
                </li>
              ))}
            </ul>
          )}

          {loginCode ? (
            <div className="admin-code" role="status">
              <p className="admin-code__label">
                Shown once. It is stored nowhere it can be read back, so a lost code means
                issuing another.
              </p>
              <code className="admin-code__value">{loginCode}</code>
            </div>
          ) : null}

          <div className="admin-actions">
            <Button
              variant="secondary"
              disabled={busy || Boolean(selected.disabled_at)}
              onClick={() => {
                if (
                  !window.confirm(
                    `Issue a single-use login code for ${selected.email}?\n\n` +
                      "It is shown once and recoverable nowhere. Deliver it over a channel " +
                      "you trust.",
                  )
                )
                  return
                act(
                  () => issueAdminLoginCode(token!, selected.id),
                  (res) => {
                    setLoginCode(res.code)
                    return `Code issued, valid until ${whenever(res.expires_at)}.`
                  },
                )
              }}
            >
              Issue a login code
            </Button>

            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                if (
                  !window.confirm(
                    `Sign ${selected.email} out of every device?\n\n` +
                      "Their stored sessions are revoked. An access token they already " +
                      "hold keeps working until it expires.",
                  )
                )
                  return
                act(
                  () => revokeAdminUserSessions(token!, selected.id),
                  (res) => `Revoked ${res.revoked} session token${res.revoked === 1 ? "" : "s"}.`,
                )
              }}
            >
              Revoke sessions
            </Button>

            {selected.disabled_at ? (
              <Button
                variant="primary"
                disabled={busy}
                onClick={() =>
                  act(
                    () => setAdminUserDisabled(token!, selected.id, false),
                    (user) => `${user.email} can sign in again.`,
                  )
                }
              >
                Enable
              </Button>
            ) : (
              <Button
                variant="danger"
                disabled={busy}
                onClick={() => setDisableTarget(selected)}
              >
                Disable
              </Button>
            )}
          </div>
        </section>
      ) : null}

      <DeactivationImpactModal
        open={disableTarget !== null}
        user={disableTarget}
        token={token ?? ""}
        busy={busy}
        onCancel={() => setDisableTarget(null)}
        onConfirm={(retireWebhookKeys) => {
          if (disableTarget) runDisable(disableTarget, retireWebhookKeys)
        }}
      />
    </div>
  )
}
