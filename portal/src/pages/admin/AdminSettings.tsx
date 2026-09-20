import {
  AdminAccounts,
  AdminAdministrators,
  AdminAudit,
  AdminLLMCalls,
  AdminModels,
  AdminOverview,
  AdminPlugins,
  AdminSpaces,
  useAdminAccess,
  type AdminSection,
} from "../../features/admin"
import { useAuth } from "../../contexts/AuthContext"
import { useSpace } from "../../contexts/SpaceContext"
import { navigate } from "../../router"

/**
 * AdminSettings is the deployment administration area.
 *
 * It is a separate area rather than another tab in space settings, and that
 * separation is the product statement: this is not something a space owner has
 * more of. Authority over the deployment is not authority inside a space, and
 * nothing here reads a space's contents.
 *
 * Someone without a grant is sent home rather than shown a forbidden screen.
 * There is nothing here to tell them about, and the server refuses regardless —
 * hiding the page is presentation, not enforcement.
 */
export function AdminSettings({ section, userId }: { section: AdminSection; userId?: string }) {
  const { token, user } = useAuth()
  const { isAdmin, loading } = useAdminAccess()
  const { currentSpaceId } = useSpace()

  if (loading) {
    return (
      <div className="settings-page">
        <p className="admin-empty">Checking your access…</p>
      </div>
    )
  }
  if (!isAdmin) {
    if (currentSpaceId) navigate({ name: "chat", spaceId: currentSpaceId })
    return null
  }

  return (
    <div className="settings-page">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Administration</h1>
          <p className="page-activity__subtitle">
            This deployment: its health, its accounts, and what has been done to it.
            Space contents are not here and are not reachable from here.
          </p>
        </div>
      </div>

      <div className="settings-page__content">
        {section === "overview" ? <AdminOverview token={token} /> : null}
        {section === "administrators" ? <AdminAdministrators token={token} /> : null}
        {section === "accounts" ? <AdminAccounts token={token} selectedUserId={userId} /> : null}
        {section === "spaces" ? <AdminSpaces token={token} /> : null}
        {section === "models" ? <AdminModels token={token} /> : null}
        {section === "calls" ? <AdminLLMCalls token={token} /> : null}
        {section === "plugins" ? <AdminPlugins token={token} /> : null}
        {section === "audit" ? <AdminAudit token={token} currentUserId={user?.id} /> : null}
      </div>
    </div>
  )
}
