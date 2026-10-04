import { useEffect, useState } from "react"
import {
  SPACE_NAV,
  SpaceInviteMemberDialog,
  SpaceMembersSection,
  SpaceOverviewSection,
  type SpaceSection,
  useSettingsData,
} from "./shared"
import { navigate } from "../../router"
import { SpaceAuditSection } from "../../features/audit"
import { SpacePlugins } from "../../features/spacePlugins"
import { SpaceSandboxDefaults } from "../../features/spaceSandbox"
import { SpaceSecrets } from "../../features/spaceSecrets"
import { SpaceServiceAccounts } from "../../features/spaceServiceAccounts"
import { SpaceAgentInstructions } from "../../features/spaceInstructions"
import { useAuth } from "../../contexts/AuthContext"
import { isAllowed } from "../../state/permissionState"

export function SpaceSettings({ spaceId, section }: { spaceId: string; section: SpaceSection }) {
  const [inviteOpen, setInviteOpen] = useState(section === "memberNew")
  const {
    user,
    spaceUsage,
    members,
    membersState,
    loadMembers,
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
    revokingInvitationId,
    revokeError,
    changingRoleUserId,
    roleError,
    issuingLoginCodeUserId,
    issuedLoginCode,
    loginCodeError,
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
  } = useSettingsData(spaceId)
  const { token } = useAuth()

  useEffect(() => {
    setInviteOpen(section === "memberNew")
    if (section !== "memberNew") {
      setEmail("")
      setInviteRole("member")
    }
  }, [section, setEmail, setInviteRole])

  function closeInviteDialog() {
    setInviteOpen(false)
    setEmail("")
    setInviteRole("member")
    if (section === "memberNew") {
      navigate({ name: "space", spaceId, section: "members" })
    }
  }

  async function submitInvite() {
    const ok = await handleInviteMember()
    if (ok) {
      setInviteOpen(false)
    }
  }

  return (
    <div className="settings-page">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Space settings</h1>
          <p className="page-activity__subtitle">
            Current space information, quota visibility, and member management.
          </p>
        </div>
      </div>

      {pageError ? (
        <p className="settings-section__error" role="alert">
          {pageError}
        </p>
      ) : null}

      <div className="settings-page__tabs" aria-label="Space sections" role="tablist">
          {SPACE_NAV.map((item) => {
            const Icon = item.icon
            const active = item.id === (section === "memberNew" ? "members" : section)
            return (
              <button
                key={item.id}
                type="button"
                role="tab"
                aria-selected={active}
                className={`settings-page__tab ${active ? "settings-page__tab--active" : ""}`}
                onClick={() => navigate({ name: "space", spaceId, section: item.id })}
              >
                <span className="settings-page__tab-icon" aria-hidden>
                  <Icon />
                </span>
                <span className="settings-page__tab-label">{item.label}</span>
              </button>
            )
          })}
      </div>

      <div className="settings-page__content">
        {section === "overview" ? (
          <>
            <SpaceOverviewSection
              currentSpaceName={currentSpaceName}
              isPersonalSpace={isPersonalSpace}
              loadingMembers={membersLoading}
              loadingUsage={spaceUsageLoading}
              members={members}
              usage={spaceUsage}
              currentUserRole={currentUserMember?.role ?? null}
            />
            <SpaceAgentInstructions
              token={token}
              spaceId={spaceId}
              canManage={isAllowed(canManageSpaceState)}
            />
          </>
        ) : null}
        {section === "plugins" ? (
          <SpacePlugins
            token={token}
            spaceId={spaceId}
            // Changing an activation is owner-or-admin, the authority the
            // space's other shared automation already needs. Reading is not.
            canManage={isAllowed(canManageSpaceState)}
          />
        ) : null}
        {section === "security" ? (
          <SpaceSandboxDefaults
            token={token}
            spaceId={spaceId}
            canManage={isAllowed(canManageSpaceState)}
          />
        ) : null}
        {section === "secrets" ? (
          <SpaceSecrets
            token={token}
            spaceId={spaceId}
            // Secrets are owner-only: value authority stays with the owner
            // until BuildMax has finer space grants. See
            // docs/design/space-secrets.md §10.
            ownerState={currentUserIsOwnerState}
          />
        ) : null}
        {section === "serviceAccounts" ? (
          <SpaceServiceAccounts
            token={token}
            spaceId={spaceId}
            isPersonalSpace={isPersonalSpace}
            members={members}
            currentUserId={user?.id}
            manageState={canManageSpaceState}
            onChanged={() => void loadMembers()}
          />
        ) : null}
        {section === "audit" ? (
          <SpaceAuditSection
            spaceId={spaceId}
            token={token}
            ownerState={currentUserIsOwnerState}
            currentUserId={user?.id}
          />
        ) : null}
        {section === "members" ? (
          <SpaceMembersSection
            spaceId={spaceId}
            currentSpaceName={currentSpaceName}
            currentUserIsOwner={currentUserIsOwner}
            currentUserRole={currentUserRole}
            membersState={membersState}
            onRetryMembers={() => void loadMembers()}
            members={members}
            userId={user?.id}
            removingUserId={removingUserId}
            removeError={removeError}
            onRemoveMember={handleRemoveMember}
            invitationsState={invitationsState}
            onRetryInvitations={() => void loadInvitations()}
            invitations={invitations}
            revokingInvitationId={revokingInvitationId}
            revokeError={revokeError}
            onRevokeInvitation={handleRevokeInvitation}
            changingRoleUserId={changingRoleUserId}
            roleError={roleError}
            onChangeRole={handleChangeRole}
            onTransferOwnership={handleTransferOwnership}
            issuingLoginCodeUserId={issuingLoginCodeUserId}
            issuedLoginCode={issuedLoginCode}
            loginCodeError={loginCodeError}
            onIssueLoginCode={handleIssueLoginCode}
          />
        ) : null}
      </div>
      <SpaceInviteMemberDialog
        open={inviteOpen}
        onClose={closeInviteDialog}
        currentSpaceName={currentSpaceName}
        currentUserRole={currentUserRole}
        saving={savingInvite}
        email={email}
        role={inviteRole}
        error={inviteError}
        onEmailChange={setEmail}
        onRoleChange={setInviteRole}
        onSubmit={submitInvite}
      />
    </div>
  )
}
