import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useRef, useState } from "react"
import { useStableT, useT } from "../../i18n"
import type {
  ApiAdminSpace,
  ApiAdminSpaceDetail,
  ApiAdminSpaceMember,
  ApiQuotaTier,
} from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { pageWindow } from "./pagination"
import { describeTierLimits } from "./quotaTier"
import { roleLabel } from "./roles"
import {
  getAdminSpace,
  listAdminSpaces,
  listQuotaTiers,
  recoverSpaceOwner,
  setSpaceQuotaTier,
} from "./api"

const PAGE_SIZE = 50

/**
 * AdminSpaces shows the deployment's team spaces as metadata.
 *
 * selectedSpaceId opens one Space's detail from a link — Overview's Spaces
 * needing attention — including a personal Space the list omits.
 *
 * Personal spaces are left out: every account has exactly one, so listing them
 * would double the rows with nothing an administrator governs. There is also
 * deliberately nothing here to click through into a space's contents. An
 * administrator learns that a space exists, how large it is, and what it is
 * using; reaching what is in it still requires membership. A link that 403s
 * would read as a bug rather than as a boundary, so there is no link.
 *
 * The detail can move a Space, personal or shared, onto another seeded quota
 * tier. That changes capacity, not access, and applies from the next quota
 * check; tier definitions themselves are not editable anywhere.
 */
export function AdminSpaces({
  token,
  selectedSpaceId,
}: {
  token: string | null
  selectedSpaceId?: string
}) {
  const t = useT()
  const stableT = useStableT()
  const [spaces, setSpaces] = useState<ApiAdminSpace[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<ApiAdminSpaceDetail | null>(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [tiers, setTiers] = useState<ApiQuotaTier[]>([])
  const [tierChoice, setTierChoice] = useState("")
  const detailRef = useRef<HTMLElement | null>(null)

  // The tier the space runs under: its own, or the deployment default the
  // usage figures were computed against when it records none.
  const currentTier = selected ? selected.quota_tier || selected.usage?.tier || "" : ""

  useEffect(() => {
    setTierChoice(currentTier)
  }, [currentTier])

  // A deployment without quota answers 503 here; the detail already says it
  // reports no quota, so the control is simply absent rather than an error.
  useEffect(() => {
    if (!token) return
    listQuotaTiers(token)
      .then((res) => setTiers(res.tiers))
      .catch(() => setTiers([]))
  }, [token])

  // Recover a shared space whose owners are all disabled by promoting an enabled
  // member. The server enforces the "every owner disabled" precondition and
  // refuses otherwise, so a mistaken click on a healthy space is a stated
  // refusal, not a silent transfer.
  async function makeOwner(space: ApiAdminSpaceDetail, member: ApiAdminSpaceMember): Promise<void> {
    if (!token) return
    const who = member.email || member.user_id
    if (!window.confirm(t("admin.spaces.makeOwnerConfirm", { who, space: space.name }))) {
      return
    }
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      await recoverSpaceOwner(token, space.id, member.user_id)
      setNotice(stableT("admin.spaces.nowOwner", { who, space: space.name }))
      setSelected(await getAdminSpace(token, space.id))
    } catch (err) {
      setError(getErrorMessage(err, stableT("admin.spaces.recoveryFailed")))
    } finally {
      setBusy(false)
    }
  }

  // The detail panel renders below the list, so on a long list it opens off
  // screen and the click reads as having done nothing.
  useEffect(() => {
    if (selected) detailRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [selected])

  const load = useCallback(
    (q: string, off: number) => {
      if (!token) return
      setLoading(true)
      setError(null)
      listAdminSpaces(token, { q, limit: PAGE_SIZE, offset: off })
        .then((res) => {
          setSpaces(res.spaces)
          setTotal(res.total)
          setOffset(off)
        })
        .catch((err) => setError(getErrorMessage(err, stableT("admin.spaces.loadError"))))
        .finally(() => setLoading(false))
    },
    [token, stableT],
  )

  useEffect(() => {
    load("", 0)
  }, [load])

  async function changeTier(space: ApiAdminSpaceDetail, tier: string): Promise<void> {
    if (!token) return
    if (!window.confirm(t("admin.spaces.tierConfirm", { space: space.name, tier }))) {
      return
    }
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      await setSpaceQuotaTier(token, space.id, tier)
      setNotice(stableT("admin.spaces.tierChanged", { space: space.name, tier }))
      setSelected(await getAdminSpace(token, space.id))
      load(query, offset)
    } catch (err) {
      setError(getErrorMessage(err, stableT("admin.spaces.tierFailed")))
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    if (!token || !selectedSpaceId) return
    getAdminSpace(token, selectedSpaceId)
      .then(setSelected)
      .catch((err) => setError(getErrorMessage(err, stableT("admin.spaces.loadOneError"))))
  }, [token, selectedSpaceId, stableT])

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.spaces.title")}</h2>
            <p className="settings-page__section-copy">{t("admin.spaces.count", { count: total })}</p>
          </div>
        </div>

        <form
          className="admin-toolbar"
          onSubmit={(e) => {
            e.preventDefault()
            load(query, 0)
          }}
        >
          <input
            className="admin-input"
            type="search"
            value={query}
            placeholder={t("admin.spaces.searchPlaceholder")}
            aria-label={t("admin.spaces.searchLabel")}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={loading}>
            {t("admin.search")}
          </Button>
        </form>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {loading ? (
          <p className="admin-empty">{t("shell.loading")}</p>
        ) : spaces.length === 0 ? (
          <p className="admin-empty">{t("admin.spaces.noMatch")}</p>
        ) : (
          <table className="admin-table">
            <thead>
              <tr>
                <th scope="col">{t("admin.spaces.colName")}</th>
                <th scope="col">{t("admin.spaces.colMembers")}</th>
                <th scope="col">{t("admin.spaces.tier")}</th>
              </tr>
            </thead>
            <tbody>
              {spaces.map((space) => (
                <tr key={space.id}>
                  <td>
                    <button
                      type="button"
                      className="admin-list__main--action"
                      onClick={() => {
                        if (!token) return
                        getAdminSpace(token, space.id)
                          .then(setSelected)
                          .catch((err) => setError(getErrorMessage(err, stableT("admin.spaces.loadOneError"))))
                      }}
                    >
                      {space.name}
                    </button>
                  </td>
                  <td>{space.member_count}</td>
                  <td className="admin-table__muted">{space.quota_tier || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {total > PAGE_SIZE
          ? (() => {
              const page = pageWindow(offset, PAGE_SIZE, total)
              return (
                <div className="admin-pager">
                  <Button
                    variant="secondary" size="compact"
                    disabled={loading || !page.hasPrev}
                    onClick={() => load(query, page.prevOffset)}
                  >
                    {t("admin.previous")}
                  </Button>
                  <span className="admin-pager__status">
                    {t("admin.pageStatus", { from: page.from, to: page.to, total })}
                  </span>
                  <Button
                    variant="secondary" size="compact"
                    disabled={loading || !page.hasNext}
                    onClick={() => load(query, page.nextOffset)}
                  >
                    {t("admin.next")}
                  </Button>
                </div>
              )
            })()
          : null}
      </section>

      {selected ? (
        <section className="settings-page__section" ref={detailRef}>
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">{selected.name}</h2>
              <p className="settings-page__section-copy">{selected.id}</p>
            </div>
            <Button variant="tertiary" onClick={() => setSelected(null)}>
              {t("admin.close")}
            </Button>
          </div>

          {selected.usage ? (
            <div className="admin-facts">
              <div className="admin-fact">
                <span className="admin-fact__label">{t("admin.spaces.tier")}</span>
                <span className="admin-fact__value">{selected.usage.tier || t("admin.spaces.unknown")}</span>
              </div>
              <div className="admin-fact">
                <span className="admin-fact__label">{t("admin.spaces.runsThisPeriod")}</span>
                <span className="admin-fact__value">
                  {selected.usage.run_count}
                  {selected.usage.max_runs_per_period !== undefined
                    ? ` / ${selected.usage.max_runs_per_period}`
                    : ""}
                </span>
              </div>
              <div className="admin-fact">
                <span className="admin-fact__label">{t("admin.spaces.tokensThisPeriod")}</span>
                <span className="admin-fact__value">
                  {selected.usage.total_tokens}
                  {selected.usage.max_tokens_per_period !== undefined
                    ? ` / ${selected.usage.max_tokens_per_period}`
                    : ""}
                </span>
              </div>
              <div className="admin-fact">
                <span className="admin-fact__label">{t("admin.spaces.period")}</span>
                <span className="admin-fact__value">
                  {t("admin.spaces.periodDays", { days: selected.usage.period_days })}
                </span>
              </div>
            </div>
          ) : (
            <p className="admin-empty">{t("admin.spaces.noQuota")}</p>
          )}

          {tiers.length > 0 ? (
            <form
              className="admin-toolbar"
              onSubmit={(e) => {
                e.preventDefault()
                void changeTier(selected, tierChoice)
              }}
            >
              <label className="admin-field">
                <span className="admin-field__label">{t("admin.spaces.quotaTier")}</span>
                <select
                  className="admin-input"
                  value={tierChoice}
                  disabled={busy}
                  onChange={(e) => setTierChoice(e.target.value)}
                >
                  {tiers.map((tier) => (
                    <option key={tier.tier_name} value={tier.tier_name}>
                      {tier.tier_name} — {describeTierLimits(tier, t)}
                    </option>
                  ))}
                </select>
              </label>
              <Button
                type="submit"
                variant="secondary"
                disabled={busy || !tierChoice || tierChoice === currentTier}
              >
                {t("admin.spaces.changeTier")}
              </Button>
            </form>
          ) : null}

          {notice ? <p className="admin-notice">{notice}</p> : null}
          <ul className="admin-list">
            {selected.members.map((member) => (
              <li key={member.user_id} className="admin-list__row">
                <span className="admin-list__main">{member.email || member.name || member.user_id}</span>
                {member.kind === "service" ? (
                  <span className="admin-pill">{t("admin.spaces.serviceAccount")}</span>
                ) : null}
                <span className="admin-pill">{roleLabel(member.role, t)}</span>
                {!selected.personal && member.role !== "owner" && member.kind !== "service" ? (
                  <Button
                    variant="secondary" size="compact"
                    disabled={busy}
                    onClick={() => void makeOwner(selected, member)}
                  >
                    {t("admin.spaces.makeOwner")}
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
          <p className="admin-scope-note">{t("admin.spaces.scopeNote")}</p>
        </section>
      ) : null}
    </div>
  )
}
