import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useRef, useState } from "react"
import type { ApiAdminSpace, ApiAdminSpaceDetail, ApiAdminSpaceMember } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { pageWindow } from "./pagination"
import { getAdminSpace, listAdminSpaces, recoverSpaceOwner } from "./api"

const PAGE_SIZE = 50

/**
 * AdminSpaces shows the deployment's team spaces as metadata.
 *
 * Personal spaces are left out: every account has exactly one, so listing them
 * would double the rows with nothing an administrator governs. There is also
 * deliberately nothing here to click through into a space's contents. An
 * administrator learns that a space exists, how large it is, and what it is
 * using; reaching what is in it still requires membership. A link that 403s
 * would read as a bug rather than as a boundary, so there is no link.
 */
export function AdminSpaces({ token }: { token: string | null }) {
  const [spaces, setSpaces] = useState<ApiAdminSpace[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<ApiAdminSpaceDetail | null>(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const detailRef = useRef<HTMLElement | null>(null)

  // Recover a shared space whose owners are all disabled by promoting an enabled
  // member. The server enforces the "every owner disabled" precondition and
  // refuses otherwise, so a mistaken click on a healthy space is a stated
  // refusal, not a silent transfer.
  async function makeOwner(space: ApiAdminSpaceDetail, member: ApiAdminSpaceMember): Promise<void> {
    if (!token) return
    const who = member.email || member.user_id
    if (
      !window.confirm(
        `Make ${who} the owner of ${space.name}?\n\n` +
          "Ownership recovery is allowed only when every current owner is disabled. " +
          "The disabled owner is demoted to admin. No membership is created, and you gain " +
          "no access to the space's contents.",
      )
    ) {
      return
    }
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      await recoverSpaceOwner(token, space.id, member.user_id)
      setNotice(`${who} is now the owner of ${space.name}.`)
      setSelected(await getAdminSpace(token, space.id))
    } catch (err) {
      setError(getErrorMessage(err, "Ownership recovery did not complete"))
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
        .catch((err) => setError(getErrorMessage(err, "Failed to load spaces")))
        .finally(() => setLoading(false))
    },
    [token],
  )

  useEffect(() => {
    load("", 0)
  }, [load])

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Spaces</h2>
            <p className="settings-page__section-copy">
              {total} team space{total === 1 ? "" : "s"}. Personal spaces are omitted.
              Metadata only — never their contents.
            </p>
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
            placeholder="Search by name"
            aria-label="Search spaces by name"
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={loading}>
            Search
          </Button>
        </form>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {loading ? (
          <p className="admin-empty">Loading…</p>
        ) : spaces.length === 0 ? (
          <p className="admin-empty">No spaces match.</p>
        ) : (
          <table className="admin-table">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Members</th>
                <th scope="col">Tier</th>
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
                          .catch((err) => setError(getErrorMessage(err, "Failed to load the space")))
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
                    Previous
                  </Button>
                  <span className="admin-pager__status">
                    {page.from}&ndash;{page.to} of {total}
                  </span>
                  <Button
                    variant="secondary" size="compact"
                    disabled={loading || !page.hasNext}
                    onClick={() => load(query, page.nextOffset)}
                  >
                    Next
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
              Close
            </Button>
          </div>

          {selected.usage ? (
            <div className="admin-facts">
              <div className="admin-fact">
                <span className="admin-fact__label">Tier</span>
                <span className="admin-fact__value">{selected.usage.tier || "unknown"}</span>
              </div>
              <div className="admin-fact">
                <span className="admin-fact__label">Runs this period</span>
                <span className="admin-fact__value">
                  {selected.usage.run_count}
                  {selected.usage.max_runs_per_period !== undefined
                    ? ` / ${selected.usage.max_runs_per_period}`
                    : ""}
                </span>
              </div>
              <div className="admin-fact">
                <span className="admin-fact__label">Tokens this period</span>
                <span className="admin-fact__value">
                  {selected.usage.total_tokens}
                  {selected.usage.max_tokens_per_period !== undefined
                    ? ` / ${selected.usage.max_tokens_per_period}`
                    : ""}
                </span>
              </div>
              <div className="admin-fact">
                <span className="admin-fact__label">Period</span>
                <span className="admin-fact__value">{selected.usage.period_days} days</span>
              </div>
            </div>
          ) : (
            <p className="admin-empty">This deployment reports no quota.</p>
          )}

          {notice ? <p className="admin-notice">{notice}</p> : null}
          <ul className="admin-list">
            {selected.members.map((member) => (
              <li key={member.user_id} className="admin-list__row">
                <span className="admin-list__main">{member.email || member.user_id}</span>
                <span className="admin-pill">{member.role}</span>
                {!selected.personal && member.role !== "owner" ? (
                  <Button
                    variant="secondary" size="compact"
                    disabled={busy}
                    onClick={() => void makeOwner(selected, member)}
                  >
                    Make owner
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
          <p className="admin-scope-note">
            Members and capacity, not work. Issues, conversations, files, artifacts, and
            run traces stay behind membership. "Make owner" recovers a space whose owners are
            all disabled; the server refuses it while any owner can still sign in.
          </p>
        </section>
      ) : null}
    </div>
  )
}
