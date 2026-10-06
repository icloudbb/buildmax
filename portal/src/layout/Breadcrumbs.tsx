import { useState } from "react"
import type { Route, Conversation } from "../lib/types"
import { navigate } from "../router"
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
 * Builds the crumb trail for a route. Shared by the inline Breadcrumbs nav
 * and the compact header, which needs only the current page's label.
 */
export function useBreadcrumbs(route: Route, conversations: Conversation[] = []): Crumb[] {
  const t = useT()
  const relativeTime = useRelativeTime()
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
    const conv = conversations.find((c) => c.id === route.conversationId)
    const convLabel = conv?.title?.trim() || (conv ? relativeTime(conv.createdAt) : t("shell.crumbs.conversation"))
    return [
      { label: t("shell.nav.chat"), route: { name: "chat", spaceId: route.spaceId } },
      { label: convLabel, route },
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

export function Breadcrumbs({ route, conversations = [] }: BreadcrumbsProps) {
  const t = useT()
  const crumbs = useBreadcrumbs(route, conversations)
  const [overflowOpen, setOverflowOpen] = useState(false)

  // Keep the current object and its nearest parent inline; earlier ancestors
  // collapse behind an overflow disclosure rather than crowding or wrapping.
  const collapsedAncestors = crumbs.length > 2 ? crumbs.slice(0, -2) : []
  const visibleCrumbs = collapsedAncestors.length > 0 ? crumbs.slice(-2) : crumbs

  return (
    <nav className="breadcrumbs" aria-label={t("shell.crumbs.label")}>
      {collapsedAncestors.length > 0 && (
        <span className="breadcrumbs__segment breadcrumbs__segment--overflow">
          <button
            type="button"
            className="breadcrumbs__link breadcrumbs__overflow-toggle"
            aria-expanded={overflowOpen}
            aria-haspopup="menu"
            aria-label={t("shell.crumbs.earlier")}
            onClick={() => setOverflowOpen((open) => !open)}
          >
            …
          </button>
          {overflowOpen && (
            <div className="breadcrumbs__overflow-menu" role="menu">
              {collapsedAncestors.map((crumb, i) => (
                <button
                  key={i}
                  type="button"
                  role="menuitem"
                  className="breadcrumbs__overflow-item"
                  onClick={() => {
                    setOverflowOpen(false)
                    navigate(crumb.route)
                  }}
                >
                  {crumb.label}
                </button>
              ))}
            </div>
          )}
          <span className="breadcrumbs__separator">/</span>
        </span>
      )}
      {visibleCrumbs.map((crumb, i) => {
        const isLast = i === visibleCrumbs.length - 1
        return (
          <span key={i} className="breadcrumbs__segment">
            {isLast ? (
              <span className="breadcrumbs__current">{crumb.label}</span>
            ) : (
              <>
                <button
                  type="button"
                  className="breadcrumbs__link"
                  onClick={() => navigate(crumb.route)}
                >
                  {crumb.label}
                </button>
                <span className="breadcrumbs__separator">/</span>
              </>
            )}
          </span>
        )
      })}
    </nav>
  )
}
