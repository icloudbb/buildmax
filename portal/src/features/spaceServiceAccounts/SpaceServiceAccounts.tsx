import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, getInitials } from "@buildmax/gui"
import type { ApiServiceAccount, ApiSpaceMember } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import {
  createServiceAccount,
  getServiceAccounts,
  setServiceAccountState,
  updateServiceAccount,
} from "../spaces/api"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed, type PermissionState } from "../../state/permissionState"

/**
 * SpaceServiceAccounts lists and manages the Space's service accounts: the
 * non-human principals work runs as, each accountable through a human sponsor
 * who is an owner or admin here. They have no email, password, or sign-in.
 * See docs/design/space-assistants.md §6.
 */
export function SpaceServiceAccounts({
  token,
  spaceId,
  isPersonalSpace,
  members,
  currentUserId,
  manageState,
}: {
  token: string | null
  spaceId: string
  isPersonalSpace: boolean
  members: ApiSpaceMember[]
  currentUserId?: string
  /** Owner-or-admin capability state. */
  manageState: PermissionState
}) {
  const canManage = isAllowed(manageState) && !isPersonalSpace
  const [data, setData] = useState<ApiServiceAccount[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<RequestError | null>(null)
  const [creating, setCreating] = useState(false)
  const [actionError, setActionError] = useState<{ id: string; message: string } | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!token) return
    setLoading(true)
    setLoadError(null)
    try {
      setData(await getServiceAccounts(spaceId, token))
    } catch (err) {
      setLoadError(classifyError(err, "Failed to load this space's service accounts"))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId])

  useEffect(() => {
    void load()
  }, [load])

  const state = useMemo(
    () => deriveResourceState({ loading, data, error: loadError, isEmpty: (d) => d.length === 0 }),
    [loading, data, loadError]
  )
  const accounts = data ?? []

  const nameOf = useCallback(
    (userId?: string) => {
      if (!userId) return "nobody"
      if (userId === currentUserId) return "you"
      const member = members.find((m) => m.user_id === userId)
      return member?.user_name || member?.user_email || userId
    },
    [members, currentUserId]
  )

  async function act(id: string, run: () => Promise<unknown>, fallback: string) {
    setBusyId(id)
    setActionError(null)
    try {
      await run()
      await load()
    } catch (err) {
      setActionError({ id, message: getErrorMessage(err, fallback) })
    } finally {
      setBusyId(null)
    }
  }

  return (
    <section className="sec" aria-labelledby="service-accounts-title">
      <div className="sec__head">
        <div>
          <h2 className="sec__title" id="service-accounts-title">
            Service accounts
          </h2>
          <p className="sec__copy">
            Space-owned identities that automation runs as, so work does not carry
            one person&apos;s authority. A service account is a member of this space
            only, cannot sign in, and has a sponsor: an owner or admin accountable
            for it.
          </p>
        </div>
        {canManage && !creating ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            New service account
          </Button>
        ) : null}
      </div>

      {isPersonalSpace ? (
        <p className="page-activity__empty">A personal space cannot have service accounts.</p>
      ) : null}

      {(state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" || state.kind === "stale") && (
        <Alert
          tone={state.kind === "stale" ? "stale" : state.kind}
          message={state.error.message}
          retry={{ label: "Retry", onClick: () => void load() }}
        />
      )}

      {creating && token ? (
        <CreateServiceAccountForm
          onCancel={() => setCreating(false)}
          onCreate={async (name) => {
            await createServiceAccount(spaceId, { name }, token)
            setCreating(false)
            await load()
          }}
        />
      ) : null}

      {state.kind === "loading" ? (
        <p className="page-activity__empty">Loading service accounts...</p>
      ) : state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" ? null : accounts.length === 0 ? (
        isPersonalSpace ? null : <p className="page-activity__empty">No service accounts yet.</p>
      ) : (
        <ul className="sec-list" aria-label="Service accounts">
          {accounts.map((account) => (
            <li key={account.id}>
              <ServiceAccountCard
                account={account}
                sponsorName={nameOf(account.sponsor_user_id)}
                canManage={canManage}
                isSponsor={account.sponsor_user_id === currentUserId}
                busy={busyId === account.id}
                error={actionError?.id === account.id ? actionError.message : null}
                onRename={(name) =>
                  act(account.id, () => updateServiceAccount(spaceId, account.id, { name }, token ?? ""), "Failed to rename")
                }
                onTakeSponsorship={() =>
                  act(
                    account.id,
                    () => updateServiceAccount(spaceId, account.id, { sponsor_user_id: currentUserId }, token ?? ""),
                    "Failed to change the sponsor"
                  )
                }
                onSetDisabled={(disabled) =>
                  act(
                    account.id,
                    () => setServiceAccountState(spaceId, account.id, disabled, token ?? ""),
                    disabled ? "Failed to disable" : "Failed to enable"
                  )
                }
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function CreateServiceAccountForm({
  onCancel,
  onCreate,
}: {
  onCancel: () => void
  onCreate: (name: string) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setError(null)
    if (!name.trim()) {
      setError("A service account needs a name.")
      return
    }
    setBusy(true)
    try {
      await onCreate(name.trim())
    } catch (err) {
      setError(getErrorMessage(err, "Failed to create the service account"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="sec-form">
      <div className="sec-form__head">
        <h3 className="sec-form__title">New service account</h3>
      </div>
      <div className="sec-field">
        <label className="modal__label" htmlFor="service-account-name">
          Name
        </label>
        <input
          id="service-account-name"
          className="modal__input"
          value={name}
          placeholder="HR operations"
          onChange={(e) => setName(e.target.value)}
        />
      </div>
      <p className="sec__copy">You become its sponsor. It joins this space as a member.</p>
      {error ? (
        <p className="sec__error" role="alert">
          {error}
        </p>
      ) : null}
      <div className="sec-form__actions">
        <Button variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button variant="primary" busy={busy} onClick={() => void submit()}>
          Create service account
        </Button>
      </div>
    </div>
  )
}

function ServiceAccountCard({
  account,
  sponsorName,
  canManage,
  isSponsor,
  busy,
  error,
  onRename,
  onTakeSponsorship,
  onSetDisabled,
}: {
  account: ApiServiceAccount
  sponsorName: string
  canManage: boolean
  isSponsor: boolean
  busy: boolean
  error: string | null
  onRename: (name: string) => Promise<void>
  onTakeSponsorship: () => Promise<void>
  onSetDisabled: (disabled: boolean) => Promise<void>
}) {
  const disabled = Boolean(account.disabled_at)
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(account.name)

  return (
    <div className={`sec-card ${disabled ? "sec-card--dead" : ""}`} data-testid="service-account">
      <div className="sec-card__head">
        <span className="sec-card__logo" aria-hidden>
          {getInitials(account.name)}
        </span>
        <div className="sec-card__ident">
          <span className="sec-card__name">{account.name}</span>
          <span className="sec-card__desc">
            {account.needs_sponsor ? "Needs a sponsor" : `Sponsored by ${sponsorName}`}
          </span>
        </div>
        <span className={`sec-status sec-status--${disabled ? "blocked" : account.needs_sponsor ? "suspended" : "active"}`}>
          <span className="sec-status__dot" aria-hidden />
          {disabled ? "disabled" : account.needs_sponsor ? "needs sponsor" : "active"}
        </span>
      </div>

      {renaming ? (
        <div className="sec-field">
          <label className="modal__label" htmlFor={`rename-${account.id}`}>
            New name
          </label>
          <input
            id={`rename-${account.id}`}
            className="modal__input"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <div className="sec-form__actions">
            <Button variant="secondary" size="compact" onClick={() => setRenaming(false)} disabled={busy}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="compact"
              busy={busy}
              onClick={() => void onRename(name.trim()).then(() => setRenaming(false))}
            >
              Save name
            </Button>
          </div>
        </div>
      ) : null}

      {canManage && !renaming ? (
        <div className="sec-card__actions">
          <Button variant="secondary" size="compact" onClick={() => setRenaming(true)} disabled={busy}>
            Rename
          </Button>
          {!isSponsor || account.needs_sponsor ? (
            <Button variant="secondary" size="compact" busy={busy} onClick={() => void onTakeSponsorship()}>
              Take sponsorship
            </Button>
          ) : null}
          <Button variant="tertiary" size="compact" busy={busy} onClick={() => void onSetDisabled(!disabled)}>
            {disabled ? "Enable" : "Disable"}
          </Button>
        </div>
      ) : null}

      {error ? (
        <p className="sec__error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}
