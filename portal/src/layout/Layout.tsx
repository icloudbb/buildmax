import { useEffect, useState, type ReactNode } from "react"
import type { Conversation, Route } from "../lib/types"
import type { LoginUser } from "../lib/api"
import { Sidebar, SidebarNavContent, unresolvedSpaceLabel } from "./Sidebar"
import { Breadcrumbs, useBreadcrumbs } from "./Breadcrumbs"
import { Drawer, HelpIcon, MenuIcon, StorefrontIcon, ThemeToggle } from "@buildmax/gui"
import { navigate } from "../router"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { useSpace } from "../contexts/SpaceContext"
import { useT } from "../i18n"

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
  const t = useT()
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
          aria-label={t("shell.openNavigation")}
          onClick={() => setDrawerOpen(true)}
        >
          <MenuIcon className="shell__menu-icon" />
        </button>
        {/* Scope only: the page's own H1 names the page, so the header does
            not repeat it (docs/design/portal-frontend-page-system.md). */}
        <span className="shell__compact-space">{route.name === "admin" ? t("shell.deployment") : currentSpace?.name ?? unresolvedSpaceLabel(spacesState, t)}</span>
      </header>
      <Drawer
        id="nav-drawer"
        open={drawerOpen && narrow}
        onClose={() => setDrawerOpen(false)}
        title={t("shell.navigation")}
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

/** HelpButton opens the end-user help manual served from the portal image. */
function HelpButton({ active }: { active: boolean }) {
  const t = useT()
  return (
    <button
      type="button"
      className={`theme-toggle ${active ? "theme-toggle--active" : ""}`}
      onClick={() => navigate({ name: "help" })}
      aria-label={t("shell.help")}
      aria-current={active ? "page" : undefined}
      title={t("shell.help")}
    >
      <HelpIcon className="theme-toggle__icon" />
    </button>
  )
}

/** MarketplaceButton opens the deployment-wide plugin catalog. */
function MarketplaceButton({ active }: { active: boolean }) {
  const t = useT()
  return (
    <button
      type="button"
      className={`theme-toggle ${active ? "theme-toggle--active" : ""}`}
      onClick={() => navigate({ name: "marketplace" })}
      aria-label={t("shell.marketplace")}
      aria-current={active ? "page" : undefined}
      title={t("shell.marketplace")}
    >
      <StorefrontIcon className="theme-toggle__icon" />
    </button>
  )
}
