import { useEffect, useState, type ReactNode } from "react"
import type { Conversation, Route } from "../lib/types"
import type { LoginUser } from "../lib/api"
import { Sidebar, SidebarNavContent, unresolvedSpaceLabel } from "./Sidebar"
import { Breadcrumbs, useBreadcrumbs } from "./Breadcrumbs"
import { Drawer, ThemeToggle } from "@buildmax/gui"
import { navigate } from "../router"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { useSpace } from "../contexts/SpaceContext"

// Matches the Narrow range's upper bound in the Portal responsive design
// (320–767px; Compact starts at 768px) — the shell switches from the
// persistent sidebar to a compact header + overlay drawer below this width.
const NARROW_QUERY = "(max-width: 767px)"

export interface LayoutProps {
  route: Route
  conversations: Conversation[]
  user: LoginUser
  onLogout: () => void
  children: ReactNode
}

export function Layout({
  route,
  conversations,
  user,
  onLogout,
  children,
}: LayoutProps) {
  const narrow = useMediaQuery(NARROW_QUERY)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const { currentSpace, spacesState } = useSpace()
  const crumbs = useBreadcrumbs(route, conversations)
  const pageTitle = crumbs[crumbs.length - 1]?.label ?? ""

  // A resize back to Compact/Wide while the drawer happens to be open must
  // not leave it mounted (and its focus trap active) behind the now-visible
  // persistent sidebar.
  useEffect(() => {
    if (!narrow) setDrawerOpen(false)
  }, [narrow])

  // The browser tab is the one orientation surface the in-page chrome can't
  // cover: a person with several Spaces open across tabs tells them apart by
  // title alone. Space-scoped pages get the Space name; global pages (Account,
  // Admin, Marketplace, Help) don't, since the selected Space has no bearing
  // on them.
  useEffect(() => {
    const spaceScoped = "spaceId" in route
    document.title = [pageTitle, spaceScoped ? currentSpace?.name : null, "BuildMax"]
      .filter(Boolean)
      .join(" · ")
  }, [pageTitle, route, currentSpace])

  return (
    <div className="shell">
      <header className="shell__compact-header">
        <button
          type="button"
          className="shell__menu-btn"
          aria-expanded={drawerOpen}
          aria-controls="nav-drawer"
          aria-label="Open navigation"
          onClick={() => setDrawerOpen(true)}
        >
          <MenuIcon className="shell__menu-icon" />
        </button>
        <div className="shell__compact-title">
          <span className="shell__compact-space">{route.name === "admin" ? "Deployment" : currentSpace?.name ?? unresolvedSpaceLabel(spacesState)}</span>
          <span className="shell__compact-page">{pageTitle}</span>
        </div>
      </header>
      <Drawer
        id="nav-drawer"
        open={drawerOpen && narrow}
        onClose={() => setDrawerOpen(false)}
        title="Navigation"
        titleId="nav-drawer-title"
      >
        <SidebarNavContent
          route={route}
          user={user}
          onLogout={onLogout}
          onNavigate={() => setDrawerOpen(false)}
        />
      </Drawer>
      <div className="shell__body">
        <Sidebar
          route={route}
          user={user}
          onLogout={onLogout}
        />
        <main className="shell__main">
          <div className="shell__top">
            <Breadcrumbs route={route} conversations={conversations} />
            <div className="shell__top-actions">
              <HelpButton active={route.name === "help"} />
              <MarketplaceButton active={route.name === "marketplace"} />
              <ThemeToggle />
            </div>
          </div>
          <div className="shell__content">{children}</div>
        </main>
      </div>
    </div>
  )
}

function MenuIcon({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden
    >
      <path d="M4 6h16" />
      <path d="M4 12h16" />
      <path d="M4 18h16" />
    </svg>
  )
}

/** HelpButton opens the end-user help manual served from the portal image. */
function HelpButton({ active }: { active: boolean }) {
  return (
    <button
      type="button"
      className={`theme-toggle ${active ? "theme-toggle--active" : ""}`}
      onClick={() => navigate({ name: "help" })}
      aria-label="Help"
      aria-current={active ? "page" : undefined}
      title="Help"
    >
      <HelpIcon className="theme-toggle__icon" />
    </button>
  )
}

function HelpIcon({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden
    >
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9.2a2.5 2.5 0 0 1 4.9.8c0 1.7-2.4 2-2.4 3.6" />
      <path d="M12 17.2h.01" />
    </svg>
  )
}

/** MarketplaceButton opens the deployment-wide plugin catalog. */
function MarketplaceButton({ active }: { active: boolean }) {
  return (
    <button
      type="button"
      className={`theme-toggle ${active ? "theme-toggle--active" : ""}`}
      onClick={() => navigate({ name: "marketplace" })}
      aria-label="Marketplace"
      aria-current={active ? "page" : undefined}
      title="Marketplace"
    >
      <StorefrontIcon className="theme-toggle__icon" />
    </button>
  )
}

function StorefrontIcon({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden
    >
      <path d="M3 9.5 4.5 4h15L21 9.5" />
      <path d="M3 9.5a2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 3 0" />
      <path d="M5 11v8a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-8" />
      <path d="M9 20v-5h6v5" />
    </svg>
  )
}
