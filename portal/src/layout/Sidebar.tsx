import { useId, useState, useRef, useEffect } from "react"
import { cn } from "../lib/cn"
import type { Route } from "../lib/types"
import type { LoginUser } from "../lib/api"
import { navigate } from "../router"
import { UserAvatar } from "../components/UserAvatar"
import SidebarExpandIcon from "../icons/sidebar-expand.svg?react"
import SidebarCollapseIcon from "../icons/sidebar-collapse.svg?react"
import NewChatIcon from "../icons/new-chat.svg?react"
import SettingsIcon from "../icons/settings.svg?react"
import HelpIcon from "../icons/help.svg?react"
import SignOutIcon from "../icons/sign-out.svg?react"
import IssueIcon from "../icons/issue.svg?react"
import WorkflowIcon from "../icons/workflow.svg?react"
import ScheduleIcon from "../icons/schedule.svg?react"
import AgentsIcon from "../icons/agents.svg?react"
import ArtifactIcon from "../icons/artifact.svg?react"
import FilesIcon from "../icons/files.svg?react"
import ShieldIcon from "../icons/shield.svg?react"
import { CreateSpaceDialog } from "../components/CreateSpaceDialog"
import { useSpace } from "../contexts/SpaceContext"
import { useApp } from "../contexts/AppContext"
import { useAdminAccess } from "../features/admin"
import { ADMIN_NAV } from "../features/admin/nav"
import { spaceSwitchTarget } from "../lib/spaceSwitch"
import type { ResourceState } from "../state/resourceState"

/** ASCII art for "BuildMax" (matches internal/tui/banner.go). */
const LOGO_ASCII = `
 ______        _ _     _ ______         _    _
(____  \\      (_) |   | |  ___ \\   /\\  \\ \\  / /
 ____)  )_   _ _| | _ | | | _ | | /  \\  \\ \\/ /
|  __  (| | | | | |/ || | || || |/ /\\ \\  )  (
| |__)  ) |_| | | ( (_| | || || | |__| |/ /\\ \\
|______/ \\____|_|_|\\____|_||_||_|______/_/  \\_\\
`.trim()

interface SidebarProps {
  route: Route
  user: LoginUser
  onLogout: () => void
}

function isAgentsActive(route: Route): boolean {
  return route.name === "agents" || route.name === "agent"
}

function isIssuesActive(route: Route): boolean {
  return route.name === "issues" || route.name === "issue"
}

function isWorkflowsActive(route: Route): boolean {
  return route.name === "workflows" || route.name === "workflow" || route.name === "workflowRun"
}

function isSchedulesActive(route: Route): boolean {
  return route.name === "schedules"
}

function isArtifactsActive(route: Route): boolean {
  return route.name === "artifacts" || route.name === "artifact"
}

function isFilesActive(route: Route): boolean {
  return route.name === "explore"
}

function isSpaceSettingsActive(route: Route): boolean {
  return route.name === "space"
}

function isAdminActive(route: Route): boolean {
  return route.name === "admin"
}

/**
 * Label for when there is no resolved current Space to name. Never fabricates
 * a Space name (e.g. the old "My Space" fallback while loading failed) — see
 * docs/design/portal-state-and-permission-feedback.md.
 */
export function unresolvedSpaceLabel(spacesState: ResourceState<unknown>): string {
  switch (spacesState.kind) {
    case "loading":
    case "refreshing":
      return "Loading…"
    case "readyEmpty":
      return "No space"
    default:
      return "Space unavailable"
  }
}

/** The persistent desktop/compact sidebar: a collapsible icon rail, the app's chrome. */
export function Sidebar({ route, user, onLogout }: SidebarProps) {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)

  return (
    <aside
      className={cn("sidebar", sidebarCollapsed && "sidebar--collapsed")}
      aria-label="Sidebar"
    >
      <div className="sidebar__header">
        {sidebarCollapsed ? (
          <button
            type="button"
            className="sidebar__logo-expand"
            onClick={() => setSidebarCollapsed(false)}
            aria-label="Expand sidebar"
            title="Expand sidebar"
          >
            <SidebarExpandIcon className="sidebar__logo-expand-icon" aria-hidden />
          </button>
        ) : (
          <>
            <pre className="sidebar__logo-ascii" aria-hidden>
              {LOGO_ASCII}
            </pre>
            <button
              type="button"
              className="sidebar__collapse-btn"
              onClick={() => setSidebarCollapsed(true)}
              aria-label="Collapse sidebar"
              title="Collapse sidebar"
            >
              <SidebarCollapseIcon className="sidebar__collapse-btn-icon" aria-hidden />
            </button>
          </>
        )}
      </div>
      <SidebarNavContent route={route} user={user} onLogout={onLogout} collapsed={sidebarCollapsed} />
    </aside>
  )
}

export interface SidebarNavContentProps {
  route: Route
  user: LoginUser
  onLogout: () => void
  /** Icon-only rail rendering. Only meaningful for the persistent desktop sidebar. */
  collapsed?: boolean
  /** Called after every navigation, so a host (e.g. the narrow drawer) can close itself. */
  onNavigate?: () => void
}

/**
 * The Space switcher, grouped primary navigation, and user menu — the part of
 * the sidebar shared between the persistent desktop `<aside>` and the narrow
 * overlay drawer.
 */
export function SidebarNavContent({
  route,
  user,
  onLogout,
  collapsed = false,
  onNavigate,
}: SidebarNavContentProps) {
  const { spaces, spacesState, currentSpace, currentSpaceId, loading: spacesLoading, setCurrentSpaceId } = useSpace()
  const { setPendingConversation } = useApp()
  const { isAdmin: isSystemAdmin } = useAdminAccess()
  const showSpaceSwitcher = spaces.length > 1
  // The persistent sidebar and the narrow drawer can both mount this
  // component at once (the drawer over the CSS-hidden persistent aside), so a
  // hardcoded id would collide; useId keeps each instance's <select> paired
  // with its own <label>.
  const spaceSelectId = useId()
  const personalSpaces = spaces.filter((space) => Boolean(space.personalForUserId))
  const sharedSpaces = spaces.filter((space) => !space.personalForUserId)
  const [userMenuOpen, setUserMenuOpen] = useState(false)
  const [createSpaceOpen, setCreateSpaceOpen] = useState(false)
  const userMenuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!userMenuOpen) return
    function handleClickOutside(e: MouseEvent) {
      if (userMenuRef.current && !userMenuRef.current.contains(e.target as Node)) {
        setUserMenuOpen(false)
      }
    }
    document.addEventListener("mousedown", handleClickOutside)
    return () => document.removeEventListener("mousedown", handleClickOutside)
  }, [userMenuOpen])

  function go(target: Route) {
    navigate(target)
    onNavigate?.()
  }

  // The only place a Space switch is a genuine user action, as opposed to the
  // shell catching up to a Space a route or a loaded resource already named
  // (see App.tsx) -- so this is the only place that also navigates.
  // Unlike go(), this deliberately does not call onNavigate(): switching
  // Space is not "choosing a destination and leaving" the way a nav button
  // is, so the narrow drawer stays open for further switcher interaction
  // (see golden-path-viewports.spec.ts).
  function switchSpace(spaceId: string) {
    setCurrentSpaceId(spaceId)
    setPendingConversation(null)
    const target = spaceSwitchTarget(route, spaceId)
    if (target) navigate(target)
  }

  return (
    <>
      <nav className="sidebar__nav" aria-label="Primary">
        <div className="sidebar__section">
          {route.name === "admin" ? (
            <div className="sidebar__space-display" aria-label="Current scope">{collapsed ? "D" : "Deployment"}</div>
          ) : !collapsed ? (
            <div className="sidebar__space-switcher">
              <div className="sidebar__space-head">
                <label
                  className="sidebar__space-label"
                  htmlFor={showSpaceSwitcher ? spaceSelectId : undefined}
                >
                  Space
                </label>
                <button
                  type="button"
                  className="sidebar__space-add"
                  onClick={() => setCreateSpaceOpen(true)}
                  aria-label="Create a new space"
                  title="Create a new space"
                >
                  +
                </button>
              </div>
              {showSpaceSwitcher ? (
                <select
                  id={spaceSelectId}
                  className="sidebar__space-select"
                  value={currentSpaceId ?? ""}
                  onChange={(e) => switchSpace(e.target.value)}
                  disabled={spacesLoading}
                >
                  {personalSpaces.length > 0 ? (
                    <optgroup label="Personal">
                      {personalSpaces.map((space) => (
                        <option key={space.id} value={space.id}>
                          {space.name}
                        </option>
                      ))}
                    </optgroup>
                  ) : null}
                  {sharedSpaces.length > 0 ? (
                    <optgroup label="Spaces">
                      {sharedSpaces.map((space) => (
                        <option key={space.id} value={space.id}>
                          {space.name}
                        </option>
                      ))}
                    </optgroup>
                  ) : null}
                </select>
              ) : (
                <div className="sidebar__space-display" aria-label="Current space">
                  {currentSpace?.name ?? unresolvedSpaceLabel(spacesState)}
                </div>
              )}
            </div>
          ) : (
            <div
              className="sidebar__space-badge"
              title={currentSpace?.name ?? unresolvedSpaceLabel(spacesState)}
            >
              {currentSpace ? currentSpace.name.slice(0, 1).toUpperCase() : "…"}
            </div>
          )}
        </div>
        {route.name !== "admin" ? <><div className="sidebar__group">
          <span className="sidebar__group-label">Work</span>
          <button
            type="button"
            className={cn("sidebar__nav-item", route.name === "chat" && "sidebar__nav-item--active")}
            onClick={() => go({ name: "chat", spaceId: currentSpaceId! })}
          >
            <NewChatIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Chat</span>
          </button>
          <button
            type="button"
            className={cn("sidebar__nav-item", isIssuesActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "issues", spaceId: currentSpaceId! })}
          >
            <IssueIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Issues</span>
          </button>
          <button
            type="button"
            className={cn("sidebar__nav-item", isAgentsActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "agents", spaceId: currentSpaceId! })}
          >
            <AgentsIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Agents</span>
          </button>
          <button
            type="button"
            className={cn("sidebar__nav-item", isWorkflowsActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "workflows", spaceId: currentSpaceId! })}
          >
            <WorkflowIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Workflows</span>
          </button>
          <button
            type="button"
            className={cn("sidebar__nav-item", isSchedulesActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "schedules", spaceId: currentSpaceId! })}
          >
            <ScheduleIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Schedules</span>
          </button>
        </div>
        <div className="sidebar__group">
          <span className="sidebar__group-label">Resources</span>
          <button
            type="button"
            className={cn("sidebar__nav-item", isFilesActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "explore", spaceId: currentSpaceId! })}
          >
            <FilesIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Files</span>
          </button>
          <button
            type="button"
            className={cn("sidebar__nav-item", isArtifactsActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "artifacts", spaceId: currentSpaceId! })}
          >
            <ArtifactIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Artifacts</span>
          </button>
        </div>
        <div className="sidebar__group">
          <span className="sidebar__group-label">Manage</span>
          <button
            type="button"
            className={cn("sidebar__nav-item", isSpaceSettingsActive(route) && "sidebar__nav-item--active")}
            onClick={() => go({ name: "space", spaceId: currentSpaceId!, section: "overview" })}
          >
            <SettingsIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Space settings</span>
          </button>
        </div>
        {isSystemAdmin && (
          // Deployment administration is a global-scope destination, not a Space
          // one: it stays a first-level nav item but sits in its own section,
          // outside the Space-grouped nav above, so it never reads as if the
          // selected Space changed its authority.
          <div className="sidebar__group sidebar__group--global">
            <button
              type="button"
              className={cn("sidebar__nav-item", isAdminActive(route) && "sidebar__nav-item--active")}
              onClick={() => go({ name: "admin", section: "overview" })}
            >
              <ShieldIcon className="sidebar__nav-icon" aria-hidden />
              <span className="sidebar__nav-item-text">Administration</span>
            </button>
          </div>
        )}</> : <>
        {/* On the Deployment scope the sidebar lists the Administration
            destinations themselves — the section owns them elsewhere as content,
            the sidebar is how you move between them — followed by the escape
            hatch back to a Space. */}
        <div className="sidebar__group sidebar__group--global">
          <span className="sidebar__group-label">Administration</span>
          {ADMIN_NAV.map((item) => {
            const Icon = item.icon
            const active = (route.section ?? "overview") === item.id
            return (
              <button
                key={item.id}
                type="button"
                className={cn("sidebar__nav-item", active && "sidebar__nav-item--active")}
                onClick={() => go({ name: "admin", section: item.id })}
              >
                <Icon className="sidebar__nav-icon" aria-hidden />
                <span className="sidebar__nav-item-text">{item.label}</span>
              </button>
            )
          })}
        </div>
        <div className="sidebar__group">
          <button type="button" className="sidebar__nav-item" disabled={!currentSpaceId} onClick={() => {
            if (currentSpaceId) go({ name: "chat", spaceId: currentSpaceId })
          }}>
            <NewChatIcon className="sidebar__nav-icon" aria-hidden />
            <span className="sidebar__nav-item-text">Back to space</span>
          </button>
        </div></>}
      </nav>
      <div className="sidebar__footer" aria-label="User" ref={userMenuRef}>
        <button
          type="button"
          className="sidebar__user-trigger"
          onClick={() => setUserMenuOpen((open) => !open)}
          aria-expanded={userMenuOpen}
          aria-haspopup="menu"
          aria-label="User menu"
        >
          <UserAvatar user={user} size="sm" />
          <span className="sidebar__user-name">
            {user.name?.trim() || (user.email ? user.email.split("@")[0] : "")}
          </span>
        </button>
        {userMenuOpen && (
          <div className="sidebar__user-menu" role="menu">
            <div className="sidebar__user-menu-header" role="none">
              <UserAvatar user={user} size="md" />
              <div className="sidebar__user-menu-header-text">
                {user.name ? (
                  <span className="sidebar__user-menu-name">{user.name}</span>
                ) : null}
                <span className="sidebar__user-menu-email">{user.email}</span>
              </div>
            </div>
            <div className="sidebar__user-menu-divider" role="separator" />
            {!collapsed && currentSpace && route.name !== "admin" ? (
              <>
                <div className="sidebar__user-menu-space" role="none">
                  <span className="sidebar__user-menu-space-label">Current space</span>
                  <span className="sidebar__user-menu-space-name">{currentSpace.name}</span>
                </div>
                <div className="sidebar__user-menu-divider" role="separator" />
              </>
            ) : null}
            <button
              type="button"
              className="sidebar__user-menu-item"
              role="menuitem"
              onClick={() => {
                setUserMenuOpen(false)
                go({ name: "account", section: "general" })
              }}
            >
              <span className="sidebar__user-menu-item-icon" aria-hidden>
                <SettingsIcon />
              </span>
              Account
            </button>
            <button
              type="button"
              className="sidebar__user-menu-item"
              role="menuitem"
              onClick={() => {
                setUserMenuOpen(false)
                go({ name: "help" })
              }}
            >
              <span className="sidebar__user-menu-item-icon" aria-hidden>
                <HelpIcon />
              </span>
              Help
            </button>
            <div className="sidebar__user-menu-divider" role="separator" />
            <button
              type="button"
              className="sidebar__user-menu-item"
              role="menuitem"
              onClick={() => {
                setUserMenuOpen(false)
                onNavigate?.()
                onLogout()
              }}
            >
              <span className="sidebar__user-menu-item-icon" aria-hidden>
                <SignOutIcon />
              </span>
              Sign Out
            </button>
          </div>
        )}
      </div>
      <CreateSpaceDialog open={createSpaceOpen} onClose={() => setCreateSpaceOpen(false)} />
    </>
  )
}
