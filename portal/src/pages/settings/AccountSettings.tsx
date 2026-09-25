import {
  ACCOUNT_NAV,
  AccountChatSection,
  AccountInvitationsSection,
  AccountWebhookSection,
  SettingsGeneralSection,
  SettingsPasswordSection,
  SettingsUsageSection,
  type AccountSection,
  useSettingsData,
} from "./shared"
import { navigate } from "../../router"

export function AccountSettings({ section, code }: { section: AccountSection; code?: string }) {
  const {
    token,
    user,
    usage,
    usageLoading,
    pageError,
    myInvitations,
    myInvitationsState,
    loadMyInvitations,
    acceptInvitationError,
    acceptingInvitationId,
    handleAcceptInvitation,
  } = useSettingsData()

  return (
    <div className="settings-page">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Account</h1>
          <p className="page-activity__subtitle">
            Your global profile, usage, and integration settings.
          </p>
        </div>
      </div>

      {pageError ? (
        <p className="settings-section__error" role="alert">
          {pageError}
        </p>
      ) : null}

      <div className="settings-page__tabs" aria-label="Account sections" role="tablist">
          {ACCOUNT_NAV.map((item) => {
            const Icon = item.icon
            const active = item.id === section
            return (
              <button
                key={item.id}
                type="button"
                role="tab"
                aria-selected={active}
                className={`settings-page__tab ${active ? "settings-page__tab--active" : ""}`}
                onClick={() => navigate({ name: "account", section: item.id })}
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
        {section === "general" ? (
          <>
            <SettingsGeneralSection user={user} />
            <SettingsPasswordSection token={token} />
          </>
        ) : null}
        {section === "usage" ? (
          <SettingsUsageSection loading={usageLoading} error={pageError} usage={usage} />
        ) : null}
        {section === "webhook" ? <AccountWebhookSection token={token} /> : null}
        {section === "chat" ? <AccountChatSection token={token} code={code} /> : null}
        {section === "invitations" ? (
          <AccountInvitationsSection
            invitationsState={myInvitationsState}
            invitations={myInvitations}
            onRetry={() => void loadMyInvitations()}
            acceptingInvitationId={acceptingInvitationId}
            acceptError={acceptInvitationError}
            onAccept={handleAcceptInvitation}
          />
        ) : null}
      </div>
    </div>
  )
}
