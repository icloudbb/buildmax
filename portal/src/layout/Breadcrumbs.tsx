import { Menu, MenuItem } from "@buildmax/gui"
import type { Route, Conversation } from "../lib/types"
import { buildHash, navigate } from "../router"
import { useApp } from "../contexts/AppContext"
import { useSpace } from "../contexts/SpaceContext"
import { useT } from "../i18n"
import { useRelativeTime } from "../lib/dateFormat"

export interface Crumb {
  label: string
  route: Route
}

interface BreadcrumbsProps {
  route: Route
  conversations?: Conversation[]
}

/**
 * A conversation's name: its title, else when it started, and a generic word
 * until the list has it. The breadcrumb and the conversation's own heading say
 * the same thing.
 */
export function useConversationLabel(): (conversation: Conversation | undefined) => string {
  const t = useT()
  const relativeTime = useRelativeTime()
  return (conversation) =>
    conversation?.title?.trim() || (conversation ? relativeTime(conversation.createdAt) : t("shell.crumbs.conversation"))
}

/**
 * Builds the crumb trail for a route, ending at the current page. The
 * breadcrumb shows the part before it (see breadcrumbTrail); the browser tab
 * title uses the current page's label.
 */
export function useBreadcrumbs(route: Route, conversations: Conversation[] = []): Crumb[] {
  const t = useT()
  const conversationLabel = useConversationLabel()
  const { entityLabels, breadcrumbTrails } = useApp()
  // Artifact detail carries no Space id of its own (the one ID-resolved
  // exception) -- by the time its label is published here, ArtifactDetail has
  // already reconciled the shell to the artifact's own Space, so the current
  // one is the right target for the collection crumb.
  const { currentSpaceId } = useSpace()

  if (route.name === "explore") {
    return [{ label: t("shell.nav.files"), route: { name: "explore", spaceId: route.spaceId } }]
  }
  if (route.name === "agents") {
    return [{ label: t("shell.nav.agents"), route: { name: "agents", spaceId: route.spaceId } }]
  }
  if (route.name === "agent") {
    return [
      { label: t("shell.nav.agents"), route: { name: "agents", spaceId: route.spaceId } },
      { label: entityLabels[route.agentId] ?? t("shell.crumbs.agent"), route },
    ]
  }
  if (route.name === "account") {
    const sectionLabel = (() => {
      switch (route.section) {
        case "usage":
          return t("shell.crumbs.usage")
        case "webhook":
          return t("shell.crumbs.webhookKeys")
        case "chat":
          return t("shell.crumbs.chatAccounts")
        case "invitations":
          return t("shell.crumbs.invitations")
        case "sessions":
          return t("shell.crumbs.sessions")
        case "general":
        default:
          return t("shell.crumbs.general")
      }
    })()
    return [
      { label: t("shell.crumbs.account"), route: { name: "account", section: "general" } },
      { label: sectionLabel, route },
    ]
  }
  if (route.name === "space") {
    const sectionLabel = (() => {
      switch (route.section) {
        case "members":
          return t("shell.crumbs.members")
        case "memberNew":
          return t("shell.crumbs.inviteMember")
        case "plugins":
          return t("shell.crumbs.plugins")
        case "security":
          return t("shell.crumbs.security")
        case "secrets":
          return t("shell.crumbs.secrets")
        case "serviceAccounts":
          return t("shell.crumbs.serviceAccounts")
        case "assistants":
          return t("shell.crumbs.assistants")
        case "audit":
          return t("shell.crumbs.audit")
        case "overview":
        default:
          return t("shell.crumbs.overview")
      }
    })()
    if (route.section === "assistants" && route.assistantId) {
      return [
        { label: t("shell.nav.spaceSettings"), route: { name: "space", spaceId: route.spaceId, section: "overview" } },
        { label: t("shell.crumbs.assistants"), route: { name: "space", spaceId: route.spaceId, section: "assistants" } },
        { label: entityLabels[route.assistantId] ?? t("shell.crumbs.assistant"), route },
      ]
    }
    return [
      { label: t("shell.nav.spaceSettings"), route: { name: "space", spaceId: route.spaceId, section: "overview" } },
      { label: sectionLabel, route },
    ]
  }
  if (route.name === "admin") {
    const sectionLabel = (() => {
      switch (route.section) {
        case "administrators":
          return t("shell.admin.administrators")
        case "accounts":
          return t("shell.admin.accounts")
        case "spaces":
          return t("shell.admin.spaces")
        case "models":
          return t("shell.admin.models")
        case "plugins":
          return t("shell.crumbs.plugins")
        case "audit":
          return t("shell.crumbs.audit")
        case "overview":
        default:
          return t("shell.crumbs.overview")
      }
    })()
    return [
      { label: t("shell.nav.administration"), route: { name: "admin", section: "overview" } },
      { label: sectionLabel, route },
    ]
  }
  if (route.name === "workflows") {
    return [{ label: t("shell.nav.workflows"), route: { name: "workflows", spaceId: route.spaceId } }]
  }
  if (route.name === "workflow") {
    return [
      { label: t("shell.nav.workflows"), route: { name: "workflows", spaceId: route.spaceId } },
      { label: entityLabels[route.workflowId] ?? t("shell.crumbs.workflow"), route },
    ]
  }
  if (route.name === "workflowRun") {
    return [
      { label: t("shell.nav.workflows"), route: { name: "workflows", spaceId: route.spaceId } },
      { label: entityLabels[route.workflowRunId] ?? t("shell.crumbs.workflowRun"), route },
    ]
  }
  if (route.name === "schedules") {
    return [{ label: t("shell.nav.schedules"), route: { name: "schedules", spaceId: route.spaceId } }]
  }
  if (route.name === "issues") {
    return [{ label: t("shell.nav.issues"), route: { name: "issues", spaceId: route.spaceId } }]
  }
  if (route.name === "issue") {
    return [
      { label: t("shell.nav.issues"), route: { name: "issues", spaceId: route.spaceId } },
      { label: entityLabels[route.issueId] ?? t("shell.crumbs.issue"), route },
    ]
  }
  if (route.name === "artifacts") {
    return [{ label: t("shell.nav.artifacts"), route: { name: "artifacts", spaceId: route.spaceId } }]
  }
  if (route.name === "artifact") {
    return currentSpaceId
      ? [
          { label: t("shell.nav.artifacts"), route: { name: "artifacts", spaceId: currentSpaceId } },
          { label: entityLabels[route.artifactId] ?? t("shell.crumbs.artifact"), route },
        ]
      : [{ label: entityLabels[route.artifactId] ?? t("shell.crumbs.artifact"), route }]
  }
  if (route.name === "marketplace") {
    return [{ label: t("shell.marketplace"), route: { name: "marketplace" } }]
  }
  if (route.name === "task") {
    // A task's parents (agent / issue / conversation) are not in the route, so
    // the detail page publishes the trail; fall back until it loads.
    return (
      breadcrumbTrails[route.taskId] ?? [
        { label: t("shell.nav.chat"), route: { name: "chat", spaceId: route.spaceId } },
        { label: t("shell.crumbs.task"), route },
      ]
    )
  }
  if (route.name === "chat" && route.conversationId) {
    return [
      { label: t("shell.nav.chat"), route: { name: "chat", spaceId: route.spaceId } },
      { label: conversationLabel(conversations.find((c) => c.id === route.conversationId)), route },
    ]
  }
  if (route.name === "chat") {
    return [{ label: t("shell.nav.chat"), route }]
  }
  if (route.name === "help") {
    return [{ label: t("shell.help"), route: { name: "help" } }]
  }
  if (route.name === "notFound") {
    return [{ label: t("shell.crumbs.notFound"), route }]
  }
  return []
}

/**
 * What the breadcrumb shows of a route's crumbs. The page names itself once, in
 * its own H1, so the breadcrumb is the way back to its parents and leaves the
 * page's own crumb out (docs/design/portal-frontend-page-system.md). In
 * Account, Space settings, and Administration the last crumb is the open
 * section, not the page the H1 names, so it stays as the current position.
 */
export function breadcrumbTrail(route: Route, crumbs: Crumb[]): { trail: Crumb[]; endsAtCurrent: boolean } {
  if (route.name === "account" || route.name === "space" || route.name === "admin") {
    return { trail: crumbs, endsAtCurrent: true }
  }
  return { trail: crumbs.slice(0, -1), endsAtCurrent: false }
}

export function Breadcrumbs({ route, conversations = [] }: BreadcrumbsProps) {
  const t = useT()
  const { trail, endsAtCurrent } = breadcrumbTrail(route, useBreadcrumbs(route, conversations))
  if (trail.length === 0) return null

  // Keep the last two crumbs inline; earlier ancestors collapse behind an
  // overflow disclosure rather than crowding or wrapping.
  const collapsedAncestors = trail.length > 2 ? trail.slice(0, -2) : []
  const visibleCrumbs = collapsedAncestors.length > 0 ? trail.slice(-2) : trail

  return (
    <nav className="breadcrumbs" aria-label={t("shell.crumbs.label")}>
      {collapsedAncestors.length > 0 && (
        <span className="breadcrumbs__segment breadcrumbs__segment--overflow">
          <Menu
            label={t("shell.crumbs.earlier")}
            trigger={(props) => (
              <button
                {...props}
                type="button"
                className="breadcrumbs__link breadcrumbs__overflow-toggle"
                aria-label={t("shell.crumbs.earlier")}
              >
                …
              </button>
            )}
          >
            {collapsedAncestors.map((crumb, i) => (
              <MenuItem key={i} onSelect={() => navigate(crumb.route)}>
                {crumb.label}
              </MenuItem>
            ))}
          </Menu>
          <span className="breadcrumbs__separator">/</span>
        </span>
      )}
      {visibleCrumbs.map((crumb, i) => {
        const isLast = i === visibleCrumbs.length - 1
        return (
          <span key={i} className="breadcrumbs__segment">
            {isLast && endsAtCurrent ? (
              <span className="breadcrumbs__current" aria-current="location">
                {crumb.label}
              </span>
            ) : (
              <>
                <a className="breadcrumbs__link" href={buildHash(crumb.route)}>
                  {crumb.label}
                </a>
                {isLast ? null : <span className="breadcrumbs__separator">/</span>}
              </>
            )}
          </span>
        )
      })}
    </nav>
  )
}
