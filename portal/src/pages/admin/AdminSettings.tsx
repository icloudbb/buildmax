import { type ComponentType } from "react"
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
} from "../../features/admin"
import { useAuth } from "../../contexts/AuthContext"
import { useSpace } from "../../contexts/SpaceContext"
import { navigate } from "../../router"
import SettingsIcon from "../../icons/settings.svg?react"
import AgentsIcon from "../../icons/agents.svg?react"
import ShieldIcon from "../../icons/shield.svg?react"
import FilesIcon from "../../icons/files.svg?react"
import UsageIcon from "../../icons/usage.svg?react"
import ToolboxIcon from "../../icons/toolbox.svg?react"

export type AdminSection =
  | "overview"
  | "administrators"
  | "accounts"
  | "spaces"
  | "models"
  | "calls"
  | "plugins"
  | "audit"

interface AdminNavItem {
  id: AdminSection
  label: string
  icon: ComponentType<{ className?: string }>
}

/**
 * Ordered by the question an operator arrives with, not by resource: is this
 * deployment all right, then who has access, then which spaces exist, then what
 * they can call, then what happened.
 */
export const ADMIN_NAV: AdminNavItem[] = [
  { id: "overview", label: "Overview", icon: SettingsIcon },
  { id: "administrators", label: "Administrators", icon: ShieldIcon },
  { id: "accounts", label: "Accounts", icon: AgentsIcon },
  { id: "spaces", label: "Spaces", icon: FilesIcon },
  { id: "models", label: "Models", icon: ToolboxIcon },
  { id: "calls", label: "LLM calls", icon: UsageIcon },
  { id: "plugins", label: "Plugins", icon: ToolboxIcon },
  { id: "audit", label: "Audit", icon: UsageIcon },
]

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

      <div className="settings-page__tabs" aria-label="Administration sections" role="tablist">
        {ADMIN_NAV.map((item) => {
          const Icon = item.icon
          const active = item.id === section
          return (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={active}
              className={`settings-page__tab ${active ? "settings-page__tab--active" : ""}`}
              onClick={() => navigate({ name: "admin", section: item.id })}
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
