import { useCallback, useEffect, useMemo, useState, type ComponentType } from "react"
import type { ApiInvitation, ApiSpaceMember, ApiUsage } from "../../lib/api/types"
import type { LoginUser } from "../../lib/api"
import { useAuth } from "../../contexts/AuthContext"
import { useSpace } from "../../contexts/SpaceContext"
import { describeQuotaPressure, getUsage } from "../../features/usage"
import { formatSize } from "../../features/artifacts"
import {
  acceptInvitation,
  getMyInvitations,
  getSpaceInvitations,
  getSpaceMembers,
  getSpaceUsage,
  inviteMember,
  issueMemberLoginCode,
  removeSpaceMember,
  revokeInvitation,
  setMemberRole,
} from "../../features/spaces/api"
import { setPassword } from "../../features/auth"
import { getErrorMessage } from "../../lib/errorMessage"
import { navigate } from "../../router"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError, type ResourceState } from "../../state/resourceState"
import { derivePermissionState } from "../../state/permissionState"
import { UserAvatar } from "../../components/UserAvatar"
import { WebhookKeysSection } from "../../components/WebhookKeysSection"
import SettingsIcon from "../../icons/settings.svg?react"
import UsageIcon from "../../icons/usage.svg?react"
import ToolboxIcon from "../../icons/toolbox.svg?react"
import AgentsIcon from "../../icons/agents.svg?react"
import IssueIcon from "../../icons/issue.svg?react"
import ShieldIcon from "../../icons/shield.svg?react"
import { BaseModal } from "@buildmax/gui"

export type AccountSection = "general" | "usage" | "webhook" | "invitations"
export type SpaceSection =
  | "overview"
  | "members"
  | "plugins"
  | "security"
  | "secrets"
  | "audit"
  | "memberNew"

interface SettingsNavItem<T extends string> {
  id: T
  label: string
  icon: ComponentType<{ className?: string }>
}

export const ACCOUNT_NAV: SettingsNavItem<Exclude<AccountSection, never>>[] = [
  { id: "general", label: "General", icon: SettingsIcon },
  { id: "usage", label: "Usage", icon: UsageIcon },
  { id: "webhook", label: "Webhook", icon: ToolboxIcon },
  // Not space-scoped: what is pending for this account, across every space it
  // was invited to. See docs/design/space-membership-lifecycle.md §5.1, §9.
  { id: "invitations", label: "Invitations", icon: AgentsIcon },
]

export const SPACE_NAV: SettingsNavItem<Exclude<SpaceSection, "memberNew">>[] = [
  { id: "overview", label: "Overview", icon: IssueIcon },
  { id: "members", label: "Members", icon: AgentsIcon },
  // What this space's background runs may use. Readable by any member, because
  // "why did this run have this plugin" is a question anyone debugging asks.
  { id: "plugins", label: "Plugins", icon: ToolboxIcon },
  // The sandbox tiers a background run inherits. Separate from Plugins because
  // "what may run" and "how confined it runs" are different decisions a reader
  // should not have to disentangle from one list.
  { id: "security", label: "Security", icon: ShieldIcon },
  // Owner-only content, but the tab stays visible for everyone, the same as
  // Audit: the section itself explains why a member cannot manage it.
  { id: "secrets", label: "Secrets", icon: ToolboxIcon },
  // Owner-only content, but the tab stays visible for everyone: the section
  // explains why a member cannot read it, which is more useful than a tab that
  // silently exists for some people and not others.
  { id: "audit", label: "Audit", icon: UsageIcon },
]

// Stable identity so a `?? []` derived list doesn't churn every render while
// its backing state is still null (before the first successful fetch).
const EMPTY_MEMBERS: ApiSpaceMember[] = []
const EMPTY_INVITATIONS: ApiInvitation[] = []

function memberDisplayName(member: ApiSpaceMember, currentUserId?: string): string {
  if (member.user_id === currentUserId) return "Me"
  if (member.user_name && member.user_name.trim() !== "") return member.user_name
  if (member.user_email && member.user_email.trim() !== "") return member.user_email
  return member.user_id
}

export function SettingsGeneralSection({ user }: { user: LoginUser | null }) {
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">General</h2>
          <p className="settings-page__section-copy">
            Account details for the currently signed-in user.
          </p>
        </div>
      </div>
      {user ? (
        <div className="settings-general">
          <div className="settings-general__avatar-row">
            <UserAvatar user={user} size="md" />
          </div>
          <dl className="settings-general__fields">
            <div className="settings-general__field">
              <dt className="settings-general__label">Name</dt>
              <dd className="settings-general__value">
                {user.name?.trim() || (user.email ? user.email.split("@")[0] : "—")}
              </dd>
            </div>
            <div className="settings-general__field">
              <dt className="settings-general__label">Email</dt>
              <dd className="settings-general__value">{user.email}</dd>
            </div>
          </dl>
        </div>
      ) : (
        <p className="settings-section__muted">Not signed in.</p>
      )}
    </section>
  )
}

/**
 * Set or change the password.
 *
 * The current password is asked for only when there is one. Someone who just
 * signed in with a login code has none — that is the recovery flow finishing —
 * and demanding a value they cannot have would strand them.
 */
export function SettingsPasswordSection({ token }: { token: string | null }) {
  const [currentPassword, setCurrentPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [status, setStatus] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!token) return
    setError(null)
    setStatus(null)
    if (newPassword !== confirmPassword) {
      setError("The two passwords do not match.")
      return
    }
    setSaving(true)
    try {
      await setPassword(token, newPassword, currentPassword || undefined)
      setStatus("Password updated. Sessions already signed in are unaffected.")
      setCurrentPassword("")
      setNewPassword("")
      setConfirmPassword("")
    } catch (err) {
      setError(getErrorMessage(err, "Could not update the password"))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">Password</h2>
          <p className="settings-page__section-copy">
            Set a password, or change the one you have. Leave the current
            password blank if you signed in with a login code and have not set
            one yet.
          </p>
        </div>
      </div>
      <form className="settings-general" onSubmit={handleSubmit}>
        <label className="settings-general__label" htmlFor="current-password">
          Current password
        </label>
        <input
          id="current-password"
          type="password"
          className="login-page__input"
          autoComplete="current-password"
          value={currentPassword}
          onChange={(e) => setCurrentPassword(e.target.value)}
          disabled={saving}
        />
        <label className="settings-general__label" htmlFor="new-password">
          New password
        </label>
        <input
          id="new-password"
          type="password"
          className="login-page__input"
          autoComplete="new-password"
          value={newPassword}
          onChange={(e) => setNewPassword(e.target.value)}
          required
          disabled={saving}
        />
        <label className="settings-general__label" htmlFor="confirm-password">
          Confirm new password
        </label>
        <input
          id="confirm-password"
          type="password"
          className="login-page__input"
          autoComplete="new-password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          required
          disabled={saving}
        />
        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}
        {status ? <p className="settings-section__muted">{status}</p> : null}
        <button
          type="submit"
          className="login-page__submit"
          disabled={saving || !token || newPassword === ""}
        >
          {saving ? "Saving…" : "Save password"}
        </button>
      </form>
    </section>
  )
}

/**
 * Says when a space is near or past its quota.
 *
 * The server records the same crossings in the audit trail, so this is the
 * fast answer and the trail is the durable one; neither replaces the other.
 */
function QuotaPressureNote({ usage }: { usage: ApiUsage | null }) {
  const pressure = describeQuotaPressure(usage)
  if (!pressure) return null
  return (
    <p
      className={`settings-usage__pressure settings-usage__pressure--${pressure.tone}`}
      role={pressure.tone === "reached" ? "alert" : "status"}
    >
      {pressure.text}
    </p>
  )
}

export function SettingsUsageSection({
  loading,
  error,
  usage,
}: {
  loading: boolean
  error: string | null
  usage: ApiUsage | null
}) {
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">Usage</h2>
          <p className="settings-page__section-copy">
            Personal usage and plan limits for your account.
          </p>
        </div>
      </div>
      {loading && <p className="settings-section__muted">Loading usage…</p>}
      {error ? (
        <p className="settings-section__error" role="alert">
          {error === "usage not available" ? "Usage not available." : error}
        </p>
      ) : null}
      {/* Stated above the numbers, because a reader who has to divide two
          figures in their head to notice they are out of quota will not. */}
      {!loading && !error ? <QuotaPressureNote usage={usage} /> : null}
      {!loading && !error && usage ? (
        <div className="settings-usage">
          {usage.tier ? (
            <p className="settings-usage__row">
              <span className="settings-usage__label">Tier</span>
              <span>{usage.tier}</span>
            </p>
          ) : null}
          <p className="settings-usage__row">
            <span className="settings-usage__label">Runs</span>
            <span>
              {usage.run_count}
              {usage.max_runs_per_period != null ? ` / ${usage.max_runs_per_period}` : ""}
            </span>
          </p>
          <p className="settings-usage__row">
            <span className="settings-usage__label">Tokens</span>
            <span>
              {usage.total_tokens.toLocaleString()}
              {usage.max_tokens_per_period != null
                ? ` / ${usage.max_tokens_per_period.toLocaleString()}`
                : ""}
            </span>
          </p>
          {/* Reported apart from the rates above, and without the period line,
              because it is not measured over one: it is what the space holds
              until somebody deletes an artifact. */}
          {usage.storage_bytes != null ? (
            <p className="settings-usage__row">
              <span className="settings-usage__label">Artifact storage</span>
              <span>
                {formatSize(usage.storage_bytes)}
                {usage.max_storage_bytes != null && usage.max_storage_bytes > 0
                  ? ` / ${formatSize(usage.max_storage_bytes)}`
                  : ""}
              </span>
            </p>
          ) : null}
          {usage.period_days > 0 ? (
            <p className="settings-usage__row settings-usage__period">
              Rolling {usage.period_days} days — runs and tokens only
            </p>
          ) : null}
        </div>
      ) : null}
    </section>
  )
}

export function AccountWebhookSection({ token }: { token: string | null }) {
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">Webhook</h2>
          <p className="settings-page__section-copy">
            Manage API keys for incoming automation triggers.
          </p>
        </div>
      </div>
      <WebhookKeysSection token={token} />
    </section>
  )
}

export function SpaceOverviewSection({
  currentSpaceName,
  isPersonalSpace,
  loadingMembers,
  loadingUsage,
  members,
  usage,
  currentUserRole,
}: {
  currentSpaceName: string
  isPersonalSpace: boolean
  loadingMembers: boolean
  loadingUsage: boolean
  members: ApiSpaceMember[]
  usage: ApiUsage | null
  currentUserRole: string | null
}) {
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">Space</h2>
          <p className="settings-page__section-copy">
            Overview and quota details for the current shared workspace.
          </p>
        </div>
        <span className="space-settings-page__badge">{isPersonalSpace ? "Personal" : "Space"}</span>
      </div>
      <dl className="space-settings-page__summary">
        <div>
          <dt>Space name</dt>
          <dd>{currentSpaceName}</dd>
        </div>
        <div>
          <dt>Members</dt>
          <dd>{loadingMembers ? "Loading..." : members.length}</dd>
        </div>
        <div>
          <dt>Your role</dt>
          <dd>{currentUserRole ?? "member"}</dd>
        </div>
        <div>
          <dt>Quota tier</dt>
          <dd>{loadingUsage ? "Loading..." : usage?.tier ?? "Unavailable"}</dd>
        </div>
        <div>
          <dt>Runs this period</dt>
          <dd>
            {loadingUsage
              ? "Loading..."
              : usage?.max_runs_per_period != null
                ? `${usage.run_count} / ${usage.max_runs_per_period}`
                : (usage?.run_count ?? "Unavailable")}
          </dd>
        </div>
        <div>
          <dt>Tokens this period</dt>
          <dd>
            {loadingUsage
              ? "Loading..."
              : usage?.max_tokens_per_period != null
                ? `${usage.total_tokens.toLocaleString()} / ${usage.max_tokens_per_period.toLocaleString()}`
                : (usage?.total_tokens != null ? usage.total_tokens.toLocaleString() : "Unavailable")}
          </dd>
        </div>
      </dl>
      {usage ? (
        <p className="space-settings-page__muted">
          Current usage window: last {usage.period_days} days.
        </p>
      ) : null}
    </section>
  )
}

function memberDisplayLabel(invitation: ApiInvitation): string {
  // The list this backs (GET .../invitations) resolves no email or name --
  // only the two things a pending offer is defined by. Resolving one would
  // mean a second round trip this section has no other reason to make.
  return invitation.user_id
}

export function SpaceMembersSection({
  spaceId,
  currentSpaceName,
  currentUserIsOwner,
  currentUserRole,
  membersState,
  onRetryMembers,
  members,
  userId,
  removingUserId,
  removeError,
  onRemoveMember,
  invitationsState,
  onRetryInvitations,
  invitations,
  revokingInvitationId,
  revokeError,
  onRevokeInvitation,
  changingRoleUserId,
  roleError,
  onChangeRole,
  onTransferOwnership,
  issuingLoginCodeUserId,
  issuedLoginCode,
  loginCodeError,
  onIssueLoginCode,
}: {
  spaceId: string
  currentSpaceName: string
  currentUserIsOwner: boolean
  currentUserRole: string | null
  membersState: ResourceState<ApiSpaceMember[]>
  onRetryMembers: () => void
  members: ApiSpaceMember[]
  userId?: string
  removingUserId: string | null
  removeError: { userId: string; message: string } | null
  onRemoveMember: (memberUserId: string) => Promise<void>
  invitationsState: ResourceState<ApiInvitation[]>
  onRetryInvitations: () => void
  invitations: ApiInvitation[]
  revokingInvitationId: string | null
  revokeError: { invitationId: string; message: string } | null
  onRevokeInvitation: (invitationId: string) => Promise<void>
  changingRoleUserId: string | null
  roleError: { userId: string; message: string } | null
  onChangeRole: (memberUserId: string, role: string) => Promise<void>
  onTransferOwnership: (memberUserId: string) => Promise<void>
  issuingLoginCodeUserId: string | null
  issuedLoginCode: { userId: string; code: string; expiresAt: string } | null
  loginCodeError: { userId: string; message: string } | null
  onIssueLoginCode: (memberUserId: string) => Promise<void>
}) {
  const canInvite = currentUserIsOwner || currentUserRole === "admin"

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">Members</h2>
          <p className="settings-page__section-copy">
            Owners and admins can invite spacemates who already have a BuildMax
            account. Only owners manage roles and access to {currentSpaceName}.
          </p>
        </div>
        <div className="space-settings-page__member-head-actions">
          <span className="page-activity__meta">
            {members.length} member{members.length === 1 ? "" : "s"}
          </span>
          {canInvite ? (
            <button
              type="button"
              className="page-activity__action-btn"
              onClick={() => navigate({ name: "space", spaceId, section: "memberNew" })}
            >
              Invite
            </button>
          ) : null}
        </div>
      </div>

      {(membersState.kind === "error" ||
        membersState.kind === "forbidden" ||
        membersState.kind === "notFound" ||
        membersState.kind === "stale") && (
        <Alert
          tone={membersState.kind === "stale" ? "stale" : membersState.kind}
          message={membersState.error.message}
          retry={{ label: "Retry", onClick: onRetryMembers }}
        />
      )}

      {membersState.kind === "loading" ? (
        <p className="page-activity__empty">Loading members...</p>
      ) : membersState.kind === "readyEmpty" ? (
        <p className="page-activity__empty">No members yet.</p>
      ) : membersState.kind === "error" || membersState.kind === "forbidden" || membersState.kind === "notFound" ? null : (
        <ul className="space-settings-page__member-list">
          {members.map((member) => {
            const isSelf = member.user_id === userId
            // A member's own row never carries a role editor, a remove
            // button, or a login-code action -- changing your own role
            // (including demoting the sole owner) goes through transfer,
            // not this list. See docs/design/space-membership-lifecycle.md
            // §5.2-§5.3.
            const canManageThisRow = currentUserIsOwner && !isSelf
            return (
              <li key={member.user_id} className="space-settings-page__member">
                <div className="space-settings-page__member-main">
                  <span className="space-settings-page__member-name">
                    {memberDisplayName(member, userId)}
                  </span>
                  <span className="space-settings-page__member-meta">
                    {member.user_email ?? member.user_id}
                  </span>
                </div>
                <div className="space-settings-page__member-actions">
                  {canManageThisRow ? (
                    <select
                      className="space-settings-page__role-select"
                      value={member.role === "owner" ? "owner" : member.role}
                      disabled={changingRoleUserId === member.user_id}
                      onChange={(e) => void onChangeRole(member.user_id, e.target.value)}
                      aria-label={`Role for ${memberDisplayName(member, userId)}`}
                    >
                      <option value="member">Member</option>
                      <option value="admin">Admin</option>
                    </select>
                  ) : (
                    <span className="space-settings-page__role">{member.role}</span>
                  )}
                  {canManageThisRow && member.role !== "owner" ? (
                    <button
                      type="button"
                      className="space-settings-page__secondary-btn space-settings-page__transfer-btn"
                      disabled={changingRoleUserId === member.user_id}
                      onClick={() => void onTransferOwnership(member.user_id)}
                    >
                      Make owner
                    </button>
                  ) : null}
                  {canManageThisRow ? (
                    <button
                      type="button"
                      className="space-settings-page__secondary-btn"
                      disabled={issuingLoginCodeUserId === member.user_id}
                      onClick={() => void onIssueLoginCode(member.user_id)}
                    >
                      {issuingLoginCodeUserId === member.user_id ? "Issuing..." : "Login code"}
                    </button>
                  ) : null}
                  {canManageThisRow ? (
                    <button
                      type="button"
                      className="space-settings-page__remove-btn"
                      disabled={removingUserId === member.user_id}
                      onClick={() => void onRemoveMember(member.user_id)}
                    >
                      {removingUserId === member.user_id ? "Removing..." : "Remove"}
                    </button>
                  ) : null}
                </div>
                {issuedLoginCode && issuedLoginCode.userId === member.user_id ? (
                  <div className="admin-code" role="status">
                    <p className="admin-code__label">
                      Shown once, for {memberDisplayName(member, userId)}. It is stored
                      nowhere it can be read back, so a lost code means issuing another.
                    </p>
                    <code className="admin-code__value">{issuedLoginCode.code}</code>
                  </div>
                ) : null}
                {roleError?.userId === member.user_id ||
                loginCodeError?.userId === member.user_id ||
                removeError?.userId === member.user_id ? (
                  <p className="settings-section__error" role="alert">
                    {roleError?.userId === member.user_id
                      ? roleError.message
                      : loginCodeError?.userId === member.user_id
                        ? loginCodeError.message
                        : removeError?.message}
                  </p>
                ) : null}
              </li>
            )
          })}
        </ul>
      )}

      {canInvite ? (
        <div className="space-settings-page__invitations">
          <h3 className="space-settings-page__subheading">Pending invitations</h3>
          {(invitationsState.kind === "error" ||
            invitationsState.kind === "forbidden" ||
            invitationsState.kind === "notFound" ||
            invitationsState.kind === "stale") && (
            <Alert
              tone={invitationsState.kind === "stale" ? "stale" : invitationsState.kind}
              message={invitationsState.error.message}
              retry={{ label: "Retry", onClick: onRetryInvitations }}
            />
          )}
          {invitationsState.kind === "loading" ? (
            <p className="page-activity__empty">Loading invitations...</p>
          ) : invitationsState.kind === "readyEmpty" ? (
            <p className="page-activity__empty">No pending invitations.</p>
          ) : invitationsState.kind === "error" ||
            invitationsState.kind === "forbidden" ||
            invitationsState.kind === "notFound" ? null : (
            <ul className="space-settings-page__member-list">
              {invitations.map((invitation) => (
                <li key={invitation.id} className="space-settings-page__member">
                  <div className="space-settings-page__member-main">
                    <span className="space-settings-page__member-name">
                      {memberDisplayLabel(invitation)}
                    </span>
                    <span className="space-settings-page__member-meta">
                      Invited as {invitation.role}, expires{" "}
                      {new Date(invitation.expires_at).toLocaleString()}
                    </span>
                  </div>
                  <div className="space-settings-page__member-actions">
                    <button
                      type="button"
                      className="space-settings-page__remove-btn"
                      disabled={revokingInvitationId === invitation.id}
                      onClick={() => void onRevokeInvitation(invitation.id)}
                    >
                      {revokingInvitationId === invitation.id ? "Revoking..." : "Revoke"}
                    </button>
                  </div>
                  {revokeError?.invitationId === invitation.id ? (
                    <p className="settings-section__error" role="alert">
                      {revokeError.message}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </div>
      ) : null}
    </section>
  )
}

export function SpaceInviteMemberDialog({
  open,
  onClose,
  currentSpaceName,
  currentUserRole,
  saving,
  email,
  role,
  error,
  onEmailChange,
  onRoleChange,
  onSubmit,
}: {
  open: boolean
  onClose: () => void
  currentSpaceName: string
  currentUserRole: string | null
  saving: boolean
  email: string
  role: string
  error: string | null
  onEmailChange: (value: string) => void
  onRoleChange: (value: string) => void
  onSubmit: () => Promise<void>
}) {
  const canInviteAsAdmin = currentUserRole === "owner"
  if (currentUserRole !== "owner" && currentUserRole !== "admin") {
    return null
  }

  return (
    <BaseModal
      open={open}
      title="Invite"
      titleId="space-invite-member-dialog-title"
      onClose={() => {
        if (saving) return
        onClose()
      }}
    >
      <div className="modal__body">
        <div className="space-settings-page__dialog">
          <p className="space-settings-page__muted">
            Invite a spacemate to {currentSpaceName} by email. The address must already
            have a BuildMax account — a system administrator creates one when it
            does not exist yet.
          </p>
          <label className="settings-page__field-label" htmlFor="settings-member-email">
            Spacemate email
          </label>
          <input
            id="settings-member-email"
            className="issues-page__input"
            type="email"
            value={email}
            onChange={(e) => onEmailChange(e.target.value)}
            placeholder="spacemate@example.com"
            autoFocus
          />
          {canInviteAsAdmin ? (
            <>
              <label className="settings-page__field-label" htmlFor="settings-member-role">
                Role
              </label>
              <select
                id="settings-member-role"
                className="issues-page__input"
                value={role}
                onChange={(e) => onRoleChange(e.target.value)}
              >
                <option value="member">Member</option>
                <option value="admin">Admin</option>
              </select>
            </>
          ) : null}
          {error ? (
            <p className="modal__error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="space-settings-page__dialog-actions">
            <button
              type="button"
              className="space-settings-page__secondary-btn"
              disabled={saving}
              onClick={onClose}
            >
              Cancel
            </button>
            <button
              type="button"
              className="page-activity__action-btn"
              disabled={saving || !email.trim()}
              onClick={() => void onSubmit()}
            >
              {saving ? "Inviting..." : "Send Invite"}
            </button>
          </div>
        </div>
      </div>
    </BaseModal>
  )
}

export function AccountInvitationsSection({
  invitationsState,
  invitations,
  onRetry,
  acceptingInvitationId,
  acceptError,
  onAccept,
}: {
  invitationsState: ResourceState<ApiInvitation[]>
  invitations: ApiInvitation[]
  onRetry: () => void
  acceptingInvitationId: string | null
  acceptError: string | null
  onAccept: (invitationId: string) => Promise<void>
}) {
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">Invitations</h2>
          <p className="settings-page__section-copy">
            Spaces that have invited you. Accepting joins the space immediately; a
            pending invitation you ignore simply expires.
          </p>
        </div>
      </div>
      {(invitationsState.kind === "error" ||
        invitationsState.kind === "forbidden" ||
        invitationsState.kind === "notFound" ||
        invitationsState.kind === "stale") && (
        <Alert
          tone={invitationsState.kind === "stale" ? "stale" : invitationsState.kind}
          message={invitationsState.error.message}
          retry={{ label: "Retry", onClick: onRetry }}
        />
      )}
      {acceptError ? (
        <p className="settings-section__error" role="alert">
          {acceptError}
        </p>
      ) : null}
      {invitationsState.kind === "loading" ? (
        <p className="page-activity__empty">Loading invitations...</p>
      ) : invitationsState.kind === "readyEmpty" ? (
        <p className="page-activity__empty">No pending invitations.</p>
      ) : invitationsState.kind === "error" ||
        invitationsState.kind === "forbidden" ||
        invitationsState.kind === "notFound" ? null : (
        <ul className="space-settings-page__member-list">
          {invitations.map((invitation) => (
            <li key={invitation.id} className="space-settings-page__member">
              <div className="space-settings-page__member-main">
                <span className="space-settings-page__member-name">
                  Invited as {invitation.role}
                </span>
                <span className="space-settings-page__member-meta">
                  Expires {new Date(invitation.expires_at).toLocaleString()}
                </span>
              </div>
              <div className="space-settings-page__member-actions">
                <button
                  type="button"
                  className="page-activity__action-btn"
                  disabled={acceptingInvitationId === invitation.id}
                  onClick={() => void onAccept(invitation.id)}
                >
                  {acceptingInvitationId === invitation.id ? "Accepting..." : "Accept"}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/**
 * `spaceId` pins the space-scoped half of this data to the route's own Space
 * rather than whichever one is currently selected -- pass it from a
 * Space-prefixed page (Space settings). Omit it from a global page (Account
 * settings), where there is no route Space to defer to and the currently
 * selected one is the only sensible source.
 */
export function useSettingsData(spaceId?: string) {
  const { token, user } = useAuth()
  const { spaces, currentSpace: contextSpace, currentSpaceId: contextSpaceId, refetchSpaces } = useSpace()
  const currentSpaceId = spaceId ?? contextSpaceId
  // Look the summary up by the resolved id rather than trusting the context's
  // own `currentSpace`: right after a Space switch, context updates before
  // this page's route does (that reconciliation is centralized in a later
  // slice), and showing one Space's name over another's fetched data would be
  // a real, silent correctness bug, not just a cosmetic lag.
  const currentSpace = spaceId ? spaces.find((s) => s.id === spaceId) ?? null : contextSpace
  const [usage, setUsage] = useState<ApiUsage | null>(null)
  const [spaceUsage, setSpaceUsage] = useState<ApiUsage | null>(null)
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no other members. See deriveResourceState.
  const [membersData, setMembersData] = useState<ApiSpaceMember[] | null>(null)
  const [usageLoading, setUsageLoading] = useState(false)
  const [spaceUsageLoading, setSpaceUsageLoading] = useState(false)
  const [membersLoading, setMembersLoading] = useState(false)
  const [membersError, setMembersError] = useState<RequestError | null>(null)
  const [pageError, setPageError] = useState<string | null>(null)
  const [email, setEmail] = useState("")
  const [inviteRole, setInviteRole] = useState("member")
  const [inviteError, setInviteError] = useState<string | null>(null)
  const [savingInvite, setSavingInvite] = useState(false)
  const [removingUserId, setRemovingUserId] = useState<string | null>(null)
  // Remove's own error, tagged with which member it was.
  const [removeError, setRemoveError] = useState<{ userId: string; message: string } | null>(null)

  const [invitationsData, setInvitationsData] = useState<ApiInvitation[] | null>(null)
  const [invitationsLoading, setInvitationsLoading] = useState(false)
  const [invitationsError, setInvitationsError] = useState<RequestError | null>(null)
  const [revokingInvitationId, setRevokingInvitationId] = useState<string | null>(null)
  // Revoke's own error, tagged with which invitation it was.
  const [revokeError, setRevokeError] = useState<{ invitationId: string; message: string } | null>(null)

  const [changingRoleUserId, setChangingRoleUserId] = useState<string | null>(null)
  const [roleError, setRoleError] = useState<{ userId: string; message: string } | null>(null)

  const [issuingLoginCodeUserId, setIssuingLoginCodeUserId] = useState<string | null>(null)
  const [issuedLoginCode, setIssuedLoginCode] = useState<
    { userId: string; code: string; expiresAt: string } | null
  >(null)
  const [loginCodeError, setLoginCodeError] = useState<{ userId: string; message: string } | null>(null)

  const [myInvitationsData, setMyInvitationsData] = useState<ApiInvitation[] | null>(null)
  const [myInvitationsLoading, setMyInvitationsLoading] = useState(false)
  const [myInvitationsError, setMyInvitationsError] = useState<RequestError | null>(null)
  const [acceptingInvitationId, setAcceptingInvitationId] = useState<string | null>(null)
  // Distinct from myInvitationsError: the accept-invitation mutation's own error.
  const [acceptInvitationError, setAcceptInvitationError] = useState<string | null>(null)

  const loadMembers = useCallback(async () => {
    if (!token || !currentSpaceId) {
      setMembersData(null)
      return
    }
    setMembersLoading(true)
    setMembersError(null)
    try {
      setMembersData(await getSpaceMembers(currentSpaceId, token))
    } catch (err) {
      // membersData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping the roster.
      setMembersError(classifyError(err, "Failed to load space members"))
    } finally {
      setMembersLoading(false)
    }
  }, [token, currentSpaceId])

  const loadSpaceUsage = useCallback(async () => {
    if (!token || !currentSpaceId) {
      setSpaceUsage(null)
      return
    }
    setSpaceUsageLoading(true)
    setPageError(null)
    try {
      setSpaceUsage(await getSpaceUsage(currentSpaceId, token))
    } catch (err) {
      setPageError(getErrorMessage(err, "Failed to load space usage"))
    } finally {
      setSpaceUsageLoading(false)
    }
  }, [token, currentSpaceId])

  useEffect(() => {
    if (!token) {
      setUsage(null)
      return
    }
    setUsageLoading(true)
    getUsage(token)
      .then((data) => {
        setUsage(data)
      })
      .catch((err) => {
        setPageError(getErrorMessage(err, "Failed to load usage"))
      })
      .finally(() => {
        setUsageLoading(false)
      })
  }, [token])

  useEffect(() => {
    void loadMembers()
  }, [loadMembers])

  useEffect(() => {
    void loadSpaceUsage()
  }, [loadSpaceUsage])

  const members = membersData ?? EMPTY_MEMBERS
  const membersState = useMemo(
    () =>
      deriveResourceState({
        loading: membersLoading,
        data: membersData,
        error: membersError,
        isEmpty: (data) => data.length === 0,
      }),
    [membersLoading, membersData, membersError]
  )

  const currentUserMember = useMemo(
    () => members.find((member) => member.user_id === user?.id) ?? null,
    [members, user?.id],
  )
  const currentUserIsOwner = currentUserMember?.role === "owner"
  const currentUserRole = currentUserMember?.role ?? null
  const canInvite = currentUserRole === "owner" || currentUserRole === "admin"
  const isPersonalSpace = Boolean(currentSpace?.personalForUserId)
  const currentSpaceName = currentSpace?.name ?? "Current Space"

  // Owner-only and owner-or-admin capability states, distinguishing "still
  // resolving membership" and "the lookup failed" from a confirmed denial --
  // see docs/design/portal-state-and-permission-feedback.md#permission-model.
  const currentUserIsOwnerState = useMemo(
    () => derivePermissionState({ loading: membersLoading, lookupFailed: membersError !== null, allowed: currentUserIsOwner }),
    [membersLoading, membersError, currentUserIsOwner]
  )
  const canManageSpaceState = useMemo(
    () =>
      derivePermissionState({
        loading: membersLoading,
        lookupFailed: membersError !== null,
        allowed: currentUserRole === "owner" || currentUserRole === "admin",
      }),
    [membersLoading, membersError, currentUserRole]
  )

  // Reading who has been invited is the same authority as sending or
  // revoking an invitation -- owner or admin. A member simply sees none,
  // rather than the page treating a 403 here as a page-level error.
  const loadInvitations = useCallback(async () => {
    if (!token || !currentSpaceId || !canInvite) {
      setInvitationsData(null)
      setInvitationsError(null)
      return
    }
    setInvitationsLoading(true)
    setInvitationsError(null)
    try {
      setInvitationsData(await getSpaceInvitations(currentSpaceId, token))
    } catch (err) {
      // A member without invite authority never reaches this branch (the
      // guard above returns first). This is a real fetch failure for someone
      // who can invite, and must not read the same as "none pending".
      setInvitationsError(classifyError(err, "Failed to load invitations"))
    } finally {
      setInvitationsLoading(false)
    }
  }, [token, currentSpaceId, canInvite])

  useEffect(() => {
    void loadInvitations()
  }, [loadInvitations])

  const invitationsState = useMemo(
    () =>
      deriveResourceState({
        loading: invitationsLoading,
        data: invitationsData,
        error: invitationsError,
        isEmpty: (data) => data.length === 0,
      }),
    [invitationsLoading, invitationsData, invitationsError]
  )
  const invitations = invitationsData ?? EMPTY_INVITATIONS

  const loadMyInvitations = useCallback(async () => {
    if (!token) {
      setMyInvitationsData(null)
      return
    }
    setMyInvitationsLoading(true)
    setMyInvitationsError(null)
    try {
      setMyInvitationsData(await getMyInvitations(token))
    } catch (err) {
      setMyInvitationsError(classifyError(err, "Failed to load invitations"))
    } finally {
      setMyInvitationsLoading(false)
    }
  }, [token])

  useEffect(() => {
    void loadMyInvitations()
  }, [loadMyInvitations])

  const myInvitationsState = useMemo(
    () =>
      deriveResourceState({
        loading: myInvitationsLoading,
        data: myInvitationsData,
        error: myInvitationsError,
        isEmpty: (data) => data.length === 0,
      }),
    [myInvitationsLoading, myInvitationsData, myInvitationsError]
  )
  const myInvitations = myInvitationsData ?? EMPTY_INVITATIONS

  async function handleInviteMember(): Promise<boolean> {
    if (!token || !currentSpaceId || !email.trim() || savingInvite) return false
    setSavingInvite(true)
    setInviteError(null)
    try {
      await inviteMember(currentSpaceId, { email: email.trim(), role: inviteRole }, token)
      setEmail("")
      setInviteRole("member")
      // Not yet a member: the invitation is pending, not active, so the
      // roster does not change -- only the pending list does.
      await loadInvitations()
      navigate({ name: "space", spaceId: currentSpaceId, section: "members" })
      return true
    } catch (err) {
      setInviteError(getErrorMessage(err, "Failed to invite"))
      return false
    } finally {
      setSavingInvite(false)
    }
  }

  async function handleRevokeInvitation(invitationId: string) {
    if (!token || !currentSpaceId || revokingInvitationId) return
    setRevokingInvitationId(invitationId)
    setRevokeError(null)
    try {
      await revokeInvitation(currentSpaceId, invitationId, token)
      await loadInvitations()
    } catch (err) {
      setRevokeError({ invitationId, message: getErrorMessage(err, "Failed to revoke the invitation") })
    } finally {
      setRevokingInvitationId(null)
    }
  }

  async function handleRemoveMember(memberUserId: string) {
    if (!token || !currentSpaceId || removingUserId) return
    setRemovingUserId(memberUserId)
    setRemoveError(null)
    try {
      await removeSpaceMember(currentSpaceId, memberUserId, token)
      await loadMembers()
    } catch (err) {
      setRemoveError({ userId: memberUserId, message: getErrorMessage(err, "Failed to remove member") })
    } finally {
      setRemovingUserId(null)
    }
  }

  async function changeRole(memberUserId: string, role: string) {
    if (!token || !currentSpaceId || changingRoleUserId) return
    setChangingRoleUserId(memberUserId)
    setRoleError(null)
    try {
      await setMemberRole(currentSpaceId, memberUserId, { role }, token)
      await loadMembers()
    } catch (err) {
      setRoleError({ userId: memberUserId, message: getErrorMessage(err, "Failed to change the role") })
    } finally {
      setChangingRoleUserId(null)
    }
  }

  async function handleChangeRole(memberUserId: string, role: string) {
    await changeRole(memberUserId, role)
  }

  /**
   * Transfer ownership. Unilateral and immediate on the backend, not subject
   * to the target's acceptance -- see
   * docs/design/space-membership-lifecycle.md §5.2-§5.3. The confirmation
   * here is deliberately distinct from the ordinary role dropdown: that
   * backend irreversibility-by-immediate-effect is a reason for more UI
   * friction on this one action, not less.
   */
  async function handleTransferOwnership(memberUserId: string) {
    const target = members.find((m) => m.user_id === memberUserId)
    const label = target ? memberDisplayName(target, user?.id) : memberUserId
    if (
      !window.confirm(
        `Make ${label} the owner of ${currentSpaceName}?\n\n` +
          "This takes effect immediately, without their confirmation. You become " +
          "an admin. You can transfer ownership back the same way.",
      )
    ) {
      return
    }
    await changeRole(memberUserId, "owner")
  }

  async function handleIssueLoginCode(memberUserId: string) {
    if (!token || !currentSpaceId || issuingLoginCodeUserId) return
    setIssuingLoginCodeUserId(memberUserId)
    setLoginCodeError(null)
    setIssuedLoginCode(null)
    try {
      const res = await issueMemberLoginCode(currentSpaceId, memberUserId, token)
      setIssuedLoginCode({ userId: memberUserId, code: res.code, expiresAt: res.expires_at })
    } catch (err) {
      setLoginCodeError({ userId: memberUserId, message: getErrorMessage(err, "Failed to issue a login code") })
    } finally {
      setIssuingLoginCodeUserId(null)
    }
  }

  async function handleAcceptInvitation(invitationId: string) {
    if (!token || acceptingInvitationId) return
    setAcceptingInvitationId(invitationId)
    setAcceptInvitationError(null)
    try {
      await acceptInvitation(invitationId, token)
      await loadMyInvitations()
      // The accepted space is now in the switcher's list, keeping the
      // current selection where it was.
      await refetchSpaces(currentSpaceId)
    } catch (err) {
      setAcceptInvitationError(getErrorMessage(err, "Failed to accept the invitation"))
    } finally {
      setAcceptingInvitationId(null)
    }
  }

  return {
    token,
    user,
    usage,
    spaceUsage,
    members,
    membersState,
    loadMembers,
    usageLoading,
    spaceUsageLoading,
    membersLoading,
    pageError,
    email,
    inviteRole,
    inviteError,
    savingInvite,
    removingUserId,
    removeError,
    invitations,
    invitationsState,
    loadInvitations,
    invitationsLoading,
    revokingInvitationId,
    revokeError,
    changingRoleUserId,
    roleError,
    issuingLoginCodeUserId,
    issuedLoginCode,
    loginCodeError,
    myInvitations,
    myInvitationsState,
    loadMyInvitations,
    myInvitationsLoading,
    myInvitationsError,
    acceptInvitationError,
    acceptingInvitationId,
    currentUserMember,
    currentUserIsOwner,
    currentUserIsOwnerState,
    canManageSpaceState,
    currentUserRole,
    isPersonalSpace,
    currentSpaceName,
    setEmail,
    setInviteRole,
    handleInviteMember,
    handleRevokeInvitation,
    handleRemoveMember,
    handleChangeRole,
    handleTransferOwnership,
    handleIssueLoginCode,
    handleAcceptInvitation,
  }
}
