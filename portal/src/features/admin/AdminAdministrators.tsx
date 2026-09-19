import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import type { ApiSystemGrant } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { createAdminGrant, listAdminGrants, listAdminUsers, revokeAdminGrant } from "./api"

function whenever(rfc3339?: string): string {
  return rfc3339 ? new Date(rfc3339).toLocaleString() : "—"
}

// The grant's granting actor is a user id, or the operator sentinel when the
// command line made it. Naming the shell keeps a bootstrap grant from reading as
// one an unknown account handed out.
function grantedByLabel(grantedBy: string): string {
  return grantedBy === "buildmax-server" ? "operator command" : grantedBy
}

/**
 * AdminAdministrators is who can operate this deployment.
 *
 * It is a separate axis from a space's owner or admin: a grant here authorizes
 * the deployment, never a space's contents. Granting names an existing account
 * by email; it does not create one. Revoking the last effective administrator is
 * refused here — that is a deliberate, database-authorized act done with
 * `buildmax-server admin revoke`, and the refusal says so.
 */
export function AdminAdministrators({ token }: { token: string | null }) {
  const [grants, setGrants] = useState<ApiSystemGrant[]>([])
  const [includeRevoked, setIncludeRevoked] = useState(false)
  const [email, setEmail] = useState("")
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const load = useCallback(
    (withRevoked: boolean) => {
      if (!token) return
      setLoading(true)
      setError(null)
      listAdminGrants(token, withRevoked)
        .then((res) => setGrants(res.grants))
        .catch((err) => setError(getErrorMessage(err, "Failed to load administrators")))
        .finally(() => setLoading(false))
    },
    [token],
  )

  useEffect(() => {
    load(includeRevoked)
  }, [load, includeRevoked])

  async function act<T>(run: () => Promise<T>, done: string): Promise<void> {
    if (!token) return
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      await run()
      setNotice(done)
      load(includeRevoked)
    } catch (err) {
      setError(getErrorMessage(err, "The action did not complete"))
    } finally {
      setBusy(false)
    }
  }

  async function grant(e: React.FormEvent) {
    e.preventDefault()
    if (!token) return
    const wanted = email.trim()
    if (wanted === "") return
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      // The grant route takes an id, and an operator thinks in emails. Resolve
      // it here to exactly one account rather than acting on a substring guess.
      const found = await listAdminUsers(token, { q: wanted })
      const matches = found.users.filter((u) => u.email.toLowerCase() === wanted.toLowerCase())
      if (matches.length === 0) {
        setError(`No account has the email ${wanted}. Create it under Accounts first.`)
        return
      }
      if (matches.length > 1) {
        setError(`More than one account matches ${wanted}.`)
        return
      }
      await createAdminGrant(token, matches[0].id)
      setEmail("")
      setNotice(`${matches[0].email} can now administer this deployment.`)
      load(includeRevoked)
    } catch (err) {
      setError(getErrorMessage(err, "The grant did not complete"))
    } finally {
      setBusy(false)
    }
  }

  function confirmRevoke(g: ApiSystemGrant): boolean {
    return window.confirm(
      `Revoke ${g.email || g.user_id}'s ${g.role}?\n\n` +
        "This removes their access to the administration area on their next " +
        "request. Their login sessions are left intact — this is not a disable.\n\n" +
        "Revoking the deployment's last administrator is refused here.",
    )
  }

  const active = grants.filter((g) => !g.revoked_at)

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Administrators</h2>
            <p className="settings-page__section-copy">
              {active.length} account{active.length === 1 ? "" : "s"} can operate this
              deployment. This is separate from any space role.
            </p>
          </div>
        </div>

        <form className="admin-toolbar" onSubmit={grant}>
          <input
            className="admin-input"
            type="email"
            value={email}
            placeholder="account email to grant"
            aria-label="Account email to grant administrator authority"
            onChange={(e) => setEmail(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={busy}>
            Grant
          </Button>
        </form>

        <label className="admin-check">
          <input
            type="checkbox"
            checked={includeRevoked}
            onChange={(e) => setIncludeRevoked(e.target.checked)}
          />
          Show revoked history
        </label>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}
        {notice ? <p className="admin-notice">{notice}</p> : null}

        {loading ? (
          <p className="admin-empty">Loading administrators…</p>
        ) : grants.length === 0 ? (
          <p className="admin-empty">
            No administrators. The first one is created with `buildmax-server admin grant`.
          </p>
        ) : (
          <ul className="admin-list">
            {grants.map((g) => (
              <li key={g.id} className="admin-list__row">
                <span className="admin-list__main">{g.email || g.user_id}</span>
                <span className={g.revoked_at ? "admin-pill admin-pill--bad" : "admin-pill admin-pill--ok"}>
                  {g.revoked_at ? "revoked" : "active"}
                </span>
                <span className="admin-list__meta">
                  {g.role} · granted by {grantedByLabel(g.granted_by)} · {whenever(g.granted_at)}
                  {g.revoked_at ? ` · revoked ${whenever(g.revoked_at)}` : ""}
                </span>
                {!g.revoked_at ? (
                  <Button
                    variant="danger" size="compact"
                    disabled={busy}
                    onClick={() => {
                      if (confirmRevoke(g)) {
                        void act(
                          () => revokeAdminGrant(token!, g.user_id),
                          `Revoked ${g.email || g.user_id}'s ${g.role}.`,
                        )
                      }
                    }}
                  >
                    Revoke
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
