import { Button, useLocale, type Locale, type Translate } from "@buildmax/gui"
import { formatTimestamp } from "../../lib/dateFormat"
import { useCallback, useEffect, useRef, useState } from "react"
import { useStableT, useT, type MessageKey } from "../../i18n"
import type { ApiAdminSession, ApiAdminUser, ApiAdminUserDetail } from "../../lib/api/types"
import { DeactivationImpactModal } from "./DeactivationImpactModal"
import { describeDisableOutcome } from "./disableOutcome"
import { getErrorMessage } from "../../lib/errorMessage"
import { navigate } from "../../router"
import { pageWindow } from "./pagination"
import { roleLabel } from "./roles"
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

function accountState(user: ApiAdminUser, t: Translate<MessageKey>): { label: string; disabled: boolean } {
  if (user.disabled_at) return { label: t("admin.accounts.disabled"), disabled: true }
  // A service account never has a password, so "no password yet" would read
  // as something left to do.
  if (user.kind === "service") return { label: t("admin.accounts.active"), disabled: false }
  if (!user.has_password) return { label: t("admin.accounts.noPassword"), disabled: false }
  return { label: t("admin.accounts.active"), disabled: false }
}

function whenever(t: Translate<MessageKey>, locale: Locale, rfc3339?: string): string {
  return rfc3339 ? formatTimestamp(rfc3339, locale) : t("admin.accounts.never")
}

/** A service account has no email; it is shown by name and marked. */
function accountLabel(user: ApiAdminUser): string {
  return user.kind === "service" ? user.name || user.id : user.email
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
  const t = useT()
  const stableT = useStableT()
  const { locale } = useLocale()
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
        .catch((err) => setError(getErrorMessage(err, stableT("admin.accounts.loadError"))))
        .finally(() => setLoading(false))
    },
    [token, stableT],
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
        .catch((err) => setError(getErrorMessage(err, stableT("admin.accounts.loadOneError"))))
      listAdminUserSessions(token, userId)
        .then((res) => setSessions(res.sessions))
        .catch(() => setSessions([]))
    },
    [token, stableT],
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
      setError(getErrorMessage(err, stableT("admin.actionFailed")))
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
      setNotice(stableT("admin.accounts.created", { email: user.email }))
    } catch (err) {
      setError(getErrorMessage(err, stableT("admin.accounts.notCreated")))
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
        const outcome = describeDisableOutcome(after, stableT)
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
            <h2 className="settings-page__section-title">{t("admin.accounts.title")}</h2>
            <p className="settings-page__section-copy">{t("admin.accounts.count", { count: total })}</p>
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
            placeholder={t("admin.accounts.searchPlaceholder")}
            aria-label={t("admin.accounts.searchLabel")}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={loading}>
            {t("admin.search")}
          </Button>
        </form>

        <div className="admin-toolbar" role="group" aria-label={t("admin.accounts.filters")}>
          <select
            className="admin-input"
            aria-label={t("admin.filterByStatus")}
            value={filters.status}
            onChange={(e) => applyFilter({ status: e.target.value })}
          >
            <option value="">{t("admin.accounts.anyStatus")}</option>
            <option value="enabled">{t("admin.accounts.enabled")}</option>
            <option value="disabled">{t("admin.accounts.disabled")}</option>
          </select>
          <select
            className="admin-input"
            aria-label={t("admin.accounts.filterPassword")}
            value={filters.hasPassword}
            onChange={(e) => applyFilter({ hasPassword: e.target.value })}
          >
            <option value="">{t("admin.accounts.anyPassword")}</option>
            <option value="true">{t("admin.accounts.hasPassword")}</option>
            <option value="false">{t("admin.accounts.noPassword")}</option>
          </select>
          <select
            className="admin-input"
            aria-label={t("admin.accounts.filterRole")}
            value={filters.systemRole}
            onChange={(e) => applyFilter({ systemRole: e.target.value })}
          >
            <option value="">{t("admin.accounts.anyRole")}</option>
            <option value="system_admin">{t("admin.accounts.administrators")}</option>
          </select>
          <select
            className="admin-input"
            aria-label={t("admin.accounts.filterPlatform")}
            value={filters.platform}
            onChange={(e) => applyFilter({ platform: e.target.value })}
          >
            <option value="">{t("admin.accounts.anyPlatform")}</option>
            <option value="portal">Portal</option>
            <option value="cli">CLI</option>
            <option value="desktop">Desktop</option>
          </select>
          <label className="admin-field">
            <span className="admin-field__label">{t("admin.accounts.signedInAfter")}</span>
            <input
              className="admin-input"
              type="date"
              value={filters.lastLoginAfter}
              max={filters.lastLoginBefore || undefined}
              onChange={(e) => applyFilter({ lastLoginAfter: e.target.value })}
            />
          </label>
          <label className="admin-field">
            <span className="admin-field__label">{t("admin.accounts.signedInBefore")}</span>
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
                {t("admin.accounts.retryCleanup")}
              </Button>
            ) : null}
          </p>
        ) : null}

        {loading ? (
          <p className="admin-empty">{t("shell.loading")}</p>
        ) : users.length === 0 ? (
          <p className="admin-empty">{t("admin.accounts.noMatch")}</p>
        ) : (
          <ul className="admin-list">
            {users.map((user) => {
              const state = accountState(user, t)
              return (
                <li key={user.id} className="admin-list__row">
                  <button
                    type="button"
                    className="admin-list__main admin-list__main--action"
                    onClick={() => navigate({ name: "admin", section: "accounts", userId: user.id })}
                  >
                    {accountLabel(user)}
                  </button>
                  {user.kind === "service" ? (
                    <span className="admin-pill">{t("admin.accounts.serviceAccount")}</span>
                  ) : null}
                  <span
                    className={
                      state.disabled ? "admin-pill admin-pill--bad" : "admin-pill"
                    }
                  >
                    {state.label}
                  </span>
                  <span className="admin-list__meta">
                    {t("admin.accounts.lastSignedIn", { when: whenever(t, locale, user.last_login_at) })}
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
                    {t("admin.previous")}
                  </Button>
                  <span className="admin-pager__status">
                    {t("admin.pageStatus", { from: page.from, to: page.to, total })}
                  </span>
                  <Button
                    variant="secondary" size="compact"
                    disabled={loading || !page.hasNext}
                    onClick={() => load(query, page.nextOffset, filters)}
                  >
                    {t("admin.next")}
                  </Button>
                </div>
              )
            })()
          : null}
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.accounts.createTitle")}</h2>
            <p className="settings-page__section-copy">{t("admin.accounts.createCopy")}</p>
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
            aria-label={t("admin.accounts.emailLabel")}
            onChange={(e) => setNewEmail(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={busy || !newEmail.trim()}>
            {t("admin.accounts.create")}
          </Button>
        </form>
      </section>

      {selected ? (
        <section className="settings-page__section" ref={detailRef}>
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">{accountLabel(selected)}</h2>
              <p className="settings-page__section-copy">
                {t("admin.accounts.detailMeta", {
                  id: selected.id,
                  when: whenever(t, locale, selected.created_at),
                  count: selected.session_count,
                })}
              </p>
            </div>
            <Button
              variant="tertiary"
              onClick={() => navigate({ name: "admin", section: "accounts" })}
            >
              {t("admin.close")}
            </Button>
          </div>

          {selected.system_roles.length > 0 ? (
            <p className="admin-notice">
              {t("admin.accounts.holdsRoles", { roles: selected.system_roles.join(t("admin.listSeparator")) })}
            </p>
          ) : null}

          <div className="admin-facts">
            <div className="admin-fact">
              <span className="admin-fact__label">{t("admin.accounts.spaces")}</span>
              <span className="admin-fact__value">
                {selected.spaces.length === 0
                  ? t("admin.accounts.none")
                  : selected.spaces
                      .map((space) =>
                        t("admin.accounts.spaceRole", { name: space.name, role: roleLabel(space.role, t) }),
                      )
                      .join(t("admin.listSeparator"))}
              </span>
            </div>
          </div>
          <p className="admin-scope-note">{t("admin.accounts.scopeNote")}</p>

          <h3 className="settings-page__section-title">{t("admin.accounts.sessions")}</h3>
          {sessions.length === 0 ? (
            <p className="admin-empty">{t("admin.accounts.noSessions")}</p>
          ) : (
            <ul className="admin-list">
              {sessions.map((session) => (
                <li key={session.session_id} className="admin-list__row">
                  <span className="admin-list__main">
                    {session.platform || t("admin.accounts.unknownPlatform")}
                    <span className="admin-list__id"> · {session.session_id}</span>
                  </span>
                  <span className="admin-list__meta">
                    {t("admin.accounts.sessionMeta", {
                      created: whenever(t, locale, session.created_at),
                      active: whenever(t, locale, session.last_rotated_at),
                      expires: whenever(t, locale, session.expires_at),
                    })}
                  </span>
                  <Button
                    variant="danger" size="compact"
                    disabled={busy}
                    onClick={() => {
                      if (
                        !window.confirm(
                          t("admin.accounts.revokeSessionConfirm", { platform: session.platform || "" }),
                        )
                      )
                        return
                      act(
                        () => revokeAdminUserSession(token!, selected.id, session.session_id),
                        () => stableT("admin.accounts.sessionRevoked"),
                      )
                    }}
                  >
                    {t("admin.revoke")}
                  </Button>
                </li>
              ))}
            </ul>
          )}

          {loginCode ? (
            <div className="admin-code" role="status">
              <p className="admin-code__label">{t("admin.accounts.codeShownOnce")}</p>
              <code className="admin-code__value">{loginCode}</code>
            </div>
          ) : null}

          <div className="admin-actions">
            <Button
              variant="secondary"
              disabled={busy || Boolean(selected.disabled_at) || selected.kind === "service"}
              onClick={() => {
                if (!window.confirm(t("admin.accounts.issueCodeConfirm", { email: selected.email }))) return
                act(
                  () => issueAdminLoginCode(token!, selected.id),
                  (res) => {
                    setLoginCode(res.code)
                    return stableT("admin.accounts.codeIssued", { when: whenever(stableT, locale, res.expires_at) })
                  },
                )
              }}
            >
              {t("admin.accounts.issueCode")}
            </Button>

            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                if (!window.confirm(t("admin.accounts.revokeAllConfirm", { email: selected.email }))) return
                act(
                  () => revokeAdminUserSessions(token!, selected.id),
                  (res) => stableT("admin.accounts.revokedTokens", { count: res.revoked }),
                )
              }}
            >
              {t("admin.accounts.revokeSessions")}
            </Button>

            {selected.disabled_at ? (
              <Button
                variant="primary"
                disabled={busy}
                onClick={() =>
                  act(
                    () => setAdminUserDisabled(token!, selected.id, false),
                    (user) => stableT("admin.accounts.canSignInAgain", { email: user.email }),
                  )
                }
              >
                {t("admin.enable")}
              </Button>
            ) : (
              <Button
                variant="danger"
                disabled={busy}
                onClick={() => setDisableTarget(selected)}
              >
                {t("admin.accounts.disable")}
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
