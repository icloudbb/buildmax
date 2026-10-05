import { type ComponentType } from "react"
import type { MessageKey } from "../../i18n"
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

export interface AdminNavItem {
  id: AdminSection
  labelKey: MessageKey
  icon: ComponentType<{ className?: string }>
}

/**
 * The Administration destinations, listed in the sidebar when the deployment
 * scope is active and reused by the page to resolve the active section. Ordered
 * by the question an operator arrives with, not by resource: is this deployment
 * all right, then who has access, then which spaces exist, then what they can
 * call, then what happened.
 */
export const ADMIN_NAV: AdminNavItem[] = [
  { id: "overview", labelKey: "shell.admin.overview", icon: SettingsIcon },
  { id: "administrators", labelKey: "shell.admin.administrators", icon: ShieldIcon },
  { id: "accounts", labelKey: "shell.admin.accounts", icon: AgentsIcon },
  { id: "spaces", labelKey: "shell.admin.spaces", icon: FilesIcon },
  { id: "models", labelKey: "shell.admin.models", icon: ToolboxIcon },
  { id: "calls", labelKey: "shell.admin.calls", icon: UsageIcon },
  { id: "plugins", labelKey: "shell.admin.plugins", icon: ToolboxIcon },
  { id: "audit", labelKey: "shell.admin.audit", icon: UsageIcon },
]
