import { type ComponentType } from "react"
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
  label: string
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
  { id: "overview", label: "Overview", icon: SettingsIcon },
  { id: "administrators", label: "Administrators", icon: ShieldIcon },
  { id: "accounts", label: "Accounts", icon: AgentsIcon },
  { id: "spaces", label: "Spaces", icon: FilesIcon },
  { id: "models", label: "Models", icon: ToolboxIcon },
  { id: "calls", label: "LLM calls", icon: UsageIcon },
  { id: "plugins", label: "Plugins", icon: ToolboxIcon },
  { id: "audit", label: "Audit", icon: UsageIcon },
]
