import { useCallback, useEffect, useMemo, useState, type ComponentType } from "react"
import type { ApiInvitation, ApiManagedCallTotals, ApiSpaceMember, ApiUsage } from "../../lib/api/types"
import type { LoginUser } from "../../lib/api"
import { useAuth } from "../../contexts/AuthContext"
import { useSpace } from "../../contexts/SpaceContext"
import { describeQuotaPressure, getUsage } from "../../features/usage"
import { formatAmount } from "../../features/runs/spend"
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
import { ChatAccountsSection } from "../../components/ChatAccountsSection"
import SettingsIcon from "../../icons/settings.svg?react"
import UsageIcon from "../../icons/usage.svg?react"
import ToolboxIcon from "../../icons/toolbox.svg?react"
import AgentsIcon from "../../icons/agents.svg?react"
import NewChatIcon from "../../icons/new-chat.svg?react"
import IssueIcon from "../../icons/issue.svg?react"
import ShieldIcon from "../../icons/shield.svg?react"
import { Button, BaseModal, type Translate } from "@buildmax/gui"
import { useStableT, useT, type MessageKey } from "../../i18n"
import { useTimestamp } from "../../lib/dateFormat"

export type AccountSection = "general" | "usage" | "webhook" | "chat" | "invitations"
export type SpaceSection =
  | "overview"
  | "members"
  | "plugins"
  | "security"
  | "secrets"
  | "serviceAccounts"
  | "assistants"
  | "audit"
  | "memberNew"

interface SettingsNavItem<T extends string> {
  id: T
  labelKey: MessageKey
  icon: ComponentType<{ className?: string }>
}

export const ACCOUNT_NAV: SettingsNavItem<Exclude<AccountSection, never>>[] = [
  { id: "general", labelKey: "account.nav.general", icon: SettingsIcon },
  { id: "usage", labelKey: "account.nav.usage", icon: UsageIcon },
  { id: "webhook", labelKey: "account.nav.webhook", icon: ToolboxIcon },
  // Chat-app accounts (Telegram) linked to this account. Account-owned, like
  // webhook keys. See docs/design/instant-messaging-channels.md.
  { id: "chat", labelKey: "account.nav.chat", icon: NewChatIcon },
  // Not space-scoped: what is pending for this account, across every space it
  // was invited to. See docs/design/space-membership-lifecycle.md §5.1, §9.
  { id: "invitations", labelKey: "account.nav.invitations", icon: AgentsIcon },
]

export const SPACE_NAV: SettingsNavItem<Exclude<SpaceSection, "memberNew">>[] = [
  { id: "overview", labelKey: "settings.nav.overview", icon: IssueIcon },
  { id: "members", labelKey: "settings.nav.members", icon: AgentsIcon },
  // What this space's background runs may use. Readable by any member, because
  // "why did this run have this plugin" is a question anyone debugging asks.
  { id: "plugins", labelKey: "settings.nav.plugins", icon: ToolboxIcon },
  // The sandbox tiers a background run inherits. Separate from Plugins because
  // "what may run" and "how confined it runs" are different decisions a reader
  // should not have to disentangle from one list.
  { id: "security", labelKey: "settings.nav.security", icon: ShieldIcon },
  // Owner-only content, but the tab stays visible for everyone, the same as
  // Audit: the section itself explains why a member cannot manage it.
  { id: "secrets", labelKey: "settings.nav.secrets", icon: ToolboxIcon },
  // Space-owned principals work runs as. Any member sees the inventory; owners
  // and admins manage it. See docs/design/space-assistants.md §6.
  { id: "serviceAccounts", labelKey: "settings.nav.serviceAccounts", icon: AgentsIcon },
  // Service front doors the space publishes on its own chat bots. Any member
  // sees them; owners and admins manage and publish them. A personal space
  // cannot have one, which the section itself says. See
  // docs/design/space-assistants.md.
  { id: "assistants", labelKey: "settings.nav.assistants", icon: NewChatIcon },
  // Owner-only content, but the tab stays visible for everyone: the section
  // explains why a member cannot read it, which is more useful than a tab that
  // silently exists for some people and not others.
  { id: "audit", labelKey: "settings.nav.audit", icon: UsageIcon },
]

// Stable identity so a `?? []` derived list doesn't churn every render while
// its backing state is still null (before the first successful fetch).
const EMPTY_MEMBERS: ApiSpaceMember[] = []
const EMPTY_INVITATIONS: ApiInvitation[] = []

function memberDisplayName(
  member: ApiSpaceMember,
  t: Translate<MessageKey>,
  currentUserId?: string,
): string {
  if (member.user_id === currentUserId) return t("settings.members.me")
  if (member.user_name && member.user_name.trim() !== "") return member.user_name
  if (member.user_email && member.user_email.trim() !== "") return member.user_email
  return member.user_id
}

const ROLE_KEYS: Record<string, MessageKey> = {
  owner: "settings.role.owner",
  admin: "settings.role.admin",
  member: "settings.role.member",
}

/** A Space role in the interface language; an unknown role stays its raw value. */
function roleLabel(role: string, t: Translate<MessageKey>): string {
  const key = ROLE_KEYS[role]
  return key ? t(key) : role
}

export function SettingsGeneralSection({ user }: { user: LoginUser | null }) {
  const t = useT()
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.general.title")}</h2>
          <p className="settings-page__section-copy">{t("account.general.copy")}</p>
        </div>
      </div>
      {user ? (
        <div className="settings-general">
          <div className="settings-general__avatar-row">
            <UserAvatar user={user} size="md" />
          </div>
          <dl className="settings-general__fields">
            <div className="settings-general__field">
              <dt className="settings-general__label">{t("account.general.name")}</dt>
              <dd className="settings-general__value">
                {user.name?.trim() || (user.email ? user.email.split("@")[0] : "—")}
              </dd>
            </div>
            <div className="settings-general__field">
              <dt className="settings-general__label">{t("account.general.email")}</dt>
              <dd className="settings-general__value">{user.email}</dd>
            </div>
          </dl>
        </div>
      ) : (
        <p className="settings-section__muted">{t("account.general.signedOut")}</p>
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
  const t = useT()
  const stableT = useStableT()
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
      setError(stableT("account.password.mismatch"))
      return
    }
    setSaving(true)
    try {
      await setPassword(token, newPassword, currentPassword || undefined)
      setStatus(stableT("account.password.updated"))
      setCurrentPassword("")
      setNewPassword("")
      setConfirmPassword("")
    } catch (err) {
      setError(getErrorMessage(err, stableT("account.password.error")))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.password.title")}</h2>
          <p className="settings-page__section-copy">{t("account.password.copy")}</p>
        </div>
      </div>
      <form className="settings-general" onSubmit={handleSubmit}>
        <label className="settings-general__label" htmlFor="current-password">
          {t("account.password.current")}
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
          {t("account.password.new")}
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
          {t("account.password.confirm")}
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
        <Button
          type="submit"
          variant="primary" busy={saving}
          disabled={!token || newPassword === ""}
        >
          {t("account.password.save")}
        </Button>
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
  const t = useT()
  const pressure = describeQuotaPressure(usage, t)
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

// Your own CLI and Desktop sessions on this deployment. They belong to no
// space, so the space figures above cannot include them; without this row a
// signed-in user has nowhere to see what those sessions cost.
function ManagedCallsRow({ totals }: { totals: ApiManagedCallTotals }) {
  const t = useT()
  const cost = totals.costs.map((c) => formatAmount(c.total, c.currency)).join(" + ")
  return (
    <p className="settings-usage__row">
      <span className="settings-usage__label">{t("account.usage.sessions")}</span>
      <span>
        {t("account.usage.calls", {
          calls: totals.call_count.toLocaleString(),
          tokens: totals.total_tokens.toLocaleString(),
        })}
        {cost ? t("account.usage.cost", { cost }) : ""}
        {totals.unpriced_calls > 0 ? t("account.usage.unpriced", { count: totals.unpriced_calls }) : ""}
      </span>
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
  const t = useT()
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.usage.title")}</h2>
          <p className="settings-page__section-copy">{t("account.usage.copy")}</p>
        </div>
      </div>
      {loading && <p className="settings-section__muted">{t("account.usage.loading")}</p>}
      {error ? (
        <p className="settings-section__error" role="alert">
          {error === "usage not available" ? t("account.usage.unavailable") : error}
        </p>
      ) : null}
      {/* Stated above the numbers, because a reader who has to divide two
          figures in their head to notice they are out of quota will not. */}
      {!loading && !error ? <QuotaPressureNote usage={usage} /> : null}
      {!loading && !error && usage ? (
        <div className="settings-usage">
          {usage.tier ? (
            <p className="settings-usage__row">
              <span className="settings-usage__label">{t("account.usage.tier")}</span>
              <span>{usage.tier}</span>
            </p>
          ) : null}
          <p className="settings-usage__row">
            <span className="settings-usage__label">{t("account.usage.runs")}</span>
            <span>
              {usage.run_count}
              {usage.max_runs_per_period != null ? ` / ${usage.max_runs_per_period}` : ""}
            </span>
          </p>
          <p className="settings-usage__row">
            <span className="settings-usage__label">{t("account.usage.tokens")}</span>
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
              <span className="settings-usage__label">{t("account.usage.storage")}</span>
              <span>
                {formatSize(usage.storage_bytes)}
                {usage.max_storage_bytes != null && usage.max_storage_bytes > 0
                  ? ` / ${formatSize(usage.max_storage_bytes)}`
                  : ""}
              </span>
            </p>
          ) : null}
          {usage.managed_calls && usage.managed_calls.call_count > 0 ? (
            <ManagedCallsRow totals={usage.managed_calls} />
          ) : null}
          {usage.period_days > 0 ? (
            <p className="settings-usage__row settings-usage__period">
              {t("account.usage.period", { days: usage.period_days })}
            </p>
          ) : null}
        </div>
      ) : null}
    </section>
  )
}

export function AccountWebhookSection({ token }: { token: string | null }) {
  const t = useT()
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.webhook.title")}</h2>
          <p className="settings-page__section-copy">{t("account.webhook.copy")}</p>
        </div>
      </div>
      <WebhookKeysSection token={token} />
    </section>
  )
}

export function AccountChatSection({ token, code }: { token: string | null; code?: string }) {
  const t = useT()
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.chat.title")}</h2>
          <p className="settings-page__section-copy">{t("account.chat.copy")}</p>
        </div>
      </div>
      <ChatAccountsSection token={token} code={code} />
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
  const t = useT()
  const loading = t("settings.loading")
  const unavailable = t("settings.unavailable")
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("settings.overview.title")}</h2>
          <p className="settings-page__section-copy">{t("settings.overview.copy")}</p>
        </div>
        <span className="space-settings-page__badge">
          {isPersonalSpace ? t("settings.overview.personal") : t("settings.overview.space")}
        </span>
      </div>
      <dl className="space-settings-page__summary">
        <div>
          <dt>{t("settings.overview.name")}</dt>
          <dd>{currentSpaceName}</dd>
        </div>
        <div>
          <dt>{t("settings.overview.members")}</dt>
          <dd>{loadingMembers ? loading : members.length}</dd>
        </div>
        <div>
          <dt>{t("settings.overview.role")}</dt>
          <dd>{roleLabel(currentUserRole ?? "member", t)}</dd>
        </div>
        <div>
          <dt>{t("settings.overview.tier")}</dt>
          <dd>{loadingUsage ? loading : usage?.tier ?? unavailable}</dd>
        </div>
        <div>
          <dt>{t("settings.overview.runs")}</dt>
          <dd>
            {loadingUsage
              ? loading
              : usage?.max_runs_per_period != null
                ? `${usage.run_count} / ${usage.max_runs_per_period}`
                : (usage?.run_count ?? unavailable)}
          </dd>
        </div>
        <div>
          <dt>{t("settings.overview.tokens")}</dt>
          <dd>
            {loadingUsage
              ? loading
              : usage?.max_tokens_per_period != null
                ? `${usage.total_tokens.toLocaleString()} / ${usage.max_tokens_per_period.toLocaleString()}`
                : (usage?.total_tokens != null ? usage.total_tokens.toLocaleString() : unavailable)}
          </dd>
        </div>
      </dl>
      {usage ? (
        <p className="space-settings-page__muted">
          {t("settings.overview.window", { days: usage.period_days })}
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
  const t = useT()
  const formatTimestamp = useTimestamp()
  const canInvite = currentUserIsOwner || currentUserRole === "admin"

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("settings.members.title")}</h2>
          <p className="settings-page__section-copy">
            {t("settings.members.copy", { space: currentSpaceName })}
          </p>
        </div>
        <div className="space-settings-page__member-head-actions">
          <span className="page-activity__meta">
            {t("settings.members.count", { count: members.length })}
          </span>
          {canInvite ? (
            <Button
              variant="primary"
              onClick={() => navigate({ name: "space", spaceId, section: "memberNew" })}
            >
              {t("settings.members.invite")}
            </Button>
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
          retry={{ label: t("shell.retry"), onClick: onRetryMembers }}
        />
      )}

      {membersState.kind === "loading" ? (
        <p className="page-activity__empty">{t("settings.members.loading")}</p>
      ) : membersState.kind === "readyEmpty" ? (
        <p className="page-activity__empty">{t("settings.members.empty")}</p>
      ) : membersState.kind === "error" || membersState.kind === "forbidden" || membersState.kind === "notFound" ? null : (
        <ul className="space-settings-page__member-list">
          {members.map((member) => {
            const isSelf = member.user_id === userId
            const isService = member.user_kind === "service"
            // A member's own row never carries a role editor, a remove
            // button, or a login-code action -- changing your own role
            // (including demoting the sole owner) goes through transfer,
            // not this list. See docs/design/space-membership-lifecycle.md
            // §5.2-§5.3. A service account's row carries none either: it is
            // a member for life and is managed under Service accounts.
            const canManageThisRow = currentUserIsOwner && !isSelf && !isService
            return (
              <li key={member.user_id} className="space-settings-page__member">
                <div className="space-settings-page__member-main">
                  <span className="space-settings-page__member-name">
                    {memberDisplayName(member, t, userId)}
                  </span>
                  <span className="space-settings-page__member-meta">
                    {isService ? t("settings.members.serviceAccount") : member.user_email ?? member.user_id}
                  </span>
                </div>
                <div className="space-settings-page__member-actions">
                  {canManageThisRow ? (
                    <select
                      className="space-settings-page__role-select"
                      value={member.role === "owner" ? "owner" : member.role}
                      disabled={changingRoleUserId === member.user_id}
                      onChange={(e) => void onChangeRole(member.user_id, e.target.value)}
                      aria-label={t("settings.members.roleFor", { name: memberDisplayName(member, t, userId) })}
                    >
                      <option value="member">{t("settings.roleOption.member")}</option>
                      <option value="admin">{t("settings.roleOption.admin")}</option>
                    </select>
                  ) : (
                    <span className="space-settings-page__role">{roleLabel(member.role, t)}</span>
                  )}
                  {canManageThisRow && member.role !== "owner" ? (
                    <Button
                      variant="secondary" size="compact"
                      disabled={changingRoleUserId === member.user_id}
                      onClick={() => void onTransferOwnership(member.user_id)}
                    >
                      {t("settings.members.makeOwner")}
                    </Button>
                  ) : null}
                  {canManageThisRow ? (
                    <Button
                      variant="secondary" size="compact" busy={issuingLoginCodeUserId === member.user_id}
                      onClick={() => void onIssueLoginCode(member.user_id)}
                    >
                      {t("settings.members.loginCode")}
                    </Button>
                  ) : null}
                  {canManageThisRow ? (
                    <Button
                      variant="danger" size="compact" busy={removingUserId === member.user_id}
                      onClick={() => void onRemoveMember(member.user_id)}
                    >
                      {t("settings.members.remove")}
                    </Button>
                  ) : null}
                </div>
                {issuedLoginCode && issuedLoginCode.userId === member.user_id ? (
                  <div className="admin-code" role="status">
                    <p className="admin-code__label">
                      {t("settings.members.codeShownOnce", { name: memberDisplayName(member, t, userId) })}
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
          <h3 className="space-settings-page__subheading">{t("settings.members.pending")}</h3>
          {(invitationsState.kind === "error" ||
            invitationsState.kind === "forbidden" ||
            invitationsState.kind === "notFound" ||
            invitationsState.kind === "stale") && (
            <Alert
              tone={invitationsState.kind === "stale" ? "stale" : invitationsState.kind}
              message={invitationsState.error.message}
              retry={{ label: t("shell.retry"), onClick: onRetryInvitations }}
            />
          )}
          {invitationsState.kind === "loading" ? (
            <p className="page-activity__empty">{t("settings.members.loadingInvitations")}</p>
          ) : invitationsState.kind === "readyEmpty" ? (
            <p className="page-activity__empty">{t("settings.members.noInvitations")}</p>
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
                      {t("settings.members.invitedAsExpires", {
                        role: roleLabel(invitation.role, t),
                        expires: formatTimestamp(invitation.expires_at),
                      })}
                    </span>
                  </div>
                  <div className="space-settings-page__member-actions">
                    <Button
                      variant="danger" size="compact" busy={revokingInvitationId === invitation.id}
                      onClick={() => void onRevokeInvitation(invitation.id)}
                    >
                      {t("settings.members.revoke")}
                    </Button>
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
  const t = useT()
  const canInviteAsAdmin = currentUserRole === "owner"
  if (currentUserRole !== "owner" && currentUserRole !== "admin") {
    return null
  }

  return (
    <BaseModal
      open={open}
      title={t("settings.invite.title")}
      titleId="space-invite-member-dialog-title"
      onClose={() => {
        if (saving) return
        onClose()
      }}
    >
      <div className="modal__body">
        <div className="space-settings-page__dialog">
          <p className="space-settings-page__muted">
            {t("settings.invite.copy", { space: currentSpaceName })}
          </p>
          <label className="settings-page__field-label" htmlFor="settings-member-email">
            {t("settings.invite.email")}
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
                {t("settings.invite.role")}
              </label>
              <select
                id="settings-member-role"
                className="issues-page__input"
                value={role}
                onChange={(e) => onRoleChange(e.target.value)}
              >
                <option value="member">{t("settings.roleOption.member")}</option>
                <option value="admin">{t("settings.roleOption.admin")}</option>
              </select>
            </>
          ) : null}
          {error ? (
            <p className="modal__error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="space-settings-page__dialog-actions">
            <Button
              variant="secondary"
              disabled={saving}
              onClick={onClose}
            >
              {t("settings.invite.cancel")}
            </Button>
            <Button
              variant="primary" busy={saving}
              disabled={!email.trim()}
              onClick={() => void onSubmit()}
            >
              {t("settings.invite.send")}
            </Button>
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
  const t = useT()
  const formatTimestamp = useTimestamp()
  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("account.invitations.title")}</h2>
          <p className="settings-page__section-copy">{t("account.invitations.copy")}</p>
        </div>
      </div>
      {(invitationsState.kind === "error" ||
        invitationsState.kind === "forbidden" ||
        invitationsState.kind === "notFound" ||
        invitationsState.kind === "stale") && (
        <Alert
          tone={invitationsState.kind === "stale" ? "stale" : invitationsState.kind}
          message={invitationsState.error.message}
          retry={{ label: t("shell.retry"), onClick: onRetry }}
        />
      )}
      {acceptError ? (
        <p className="settings-section__error" role="alert">
          {acceptError}
        </p>
      ) : null}
      {invitationsState.kind === "loading" ? (
        <p className="page-activity__empty">{t("settings.members.loadingInvitations")}</p>
      ) : invitationsState.kind === "readyEmpty" ? (
        <p className="page-activity__empty">{t("settings.members.noInvitations")}</p>
      ) : invitationsState.kind === "error" ||
        invitationsState.kind === "forbidden" ||
        invitationsState.kind === "notFound" ? null : (
        <ul className="space-settings-page__member-list">
          {invitations.map((invitation) => (
            <li key={invitation.id} className="space-settings-page__member">
              <div className="space-settings-page__member-main">
                <span className="space-settings-page__member-name">
                  {t("account.invitations.invitedAs", { role: roleLabel(invitation.role, t) })}
                </span>
                <span className="space-settings-page__member-meta">
                  {t("account.invitations.expires", { date: formatTimestamp(invitation.expires_at) })}
                </span>
              </div>
              <div className="space-settings-page__member-actions">
                <Button
                  variant="primary" size="compact" busy={acceptingInvitationId === invitation.id}
                  onClick={() => void onAccept(invitation.id)}
                >
                  {t("account.invitations.accept")}
                </Button>
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
  const t = useT()
  const stableT = useStableT()
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
      setMembersError(classifyError(err, stableT("settings.error.loadMembers")))
    } finally {
      setMembersLoading(false)
    }
  }, [token, currentSpaceId, stableT])

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
      setPageError(getErrorMessage(err, stableT("settings.error.loadSpaceUsage")))
    } finally {
      setSpaceUsageLoading(false)
    }
  }, [token, currentSpaceId, stableT])

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
        setPageError(getErrorMessage(err, stableT("settings.error.loadUsage")))
      })
      .finally(() => {
        setUsageLoading(false)
      })
  }, [token, stableT])

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
  const currentSpaceName = currentSpace?.name ?? t("settings.currentSpace")

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
      setInvitationsError(classifyError(err, stableT("settings.error.loadInvitations")))
    } finally {
      setInvitationsLoading(false)
    }
  }, [token, currentSpaceId, canInvite, stableT])

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
      setMyInvitationsError(classifyError(err, stableT("settings.error.loadInvitations")))
    } finally {
      setMyInvitationsLoading(false)
    }
  }, [token, stableT])

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
      setInviteError(getErrorMessage(err, stableT("settings.error.invite")))
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
      setRevokeError({ invitationId, message: getErrorMessage(err, stableT("settings.error.revokeInvitation")) })
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
      setRemoveError({ userId: memberUserId, message: getErrorMessage(err, stableT("settings.error.removeMember")) })
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
      setRoleError({ userId: memberUserId, message: getErrorMessage(err, stableT("settings.error.changeRole")) })
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
    const label = target ? memberDisplayName(target, stableT, user?.id) : memberUserId
    if (
      !window.confirm(
        stableT("settings.members.transferConfirm", { name: label, space: currentSpaceName }),
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
      setLoginCodeError({ userId: memberUserId, message: getErrorMessage(err, stableT("settings.error.issueLoginCode")) })
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
      setAcceptInvitationError(getErrorMessage(err, stableT("settings.error.acceptInvitation")))
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
