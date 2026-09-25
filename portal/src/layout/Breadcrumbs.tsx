import { useState } from "react"
import type { Route, Conversation } from "../lib/types"
import { navigate } from "../router"
import { useApp } from "../contexts/AppContext"
import { useSpace } from "../contexts/SpaceContext"

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
  const { entityLabels, breadcrumbTrails } = useApp()
  // Artifact detail carries no Space id of its own (the one ID-resolved
  // exception) -- by the time its label is published here, ArtifactDetail has
  // already reconciled the shell to the artifact's own Space, so the current
  // one is the right target for the collection crumb.
  const { currentSpaceId } = useSpace()

  if (route.name === "explore") {
    return [{ label: "Files", route: { name: "explore", spaceId: route.spaceId } }]
  }
  if (route.name === "agents") {
    return [{ label: "Agents", route: { name: "agents", spaceId: route.spaceId } }]
  }
  if (route.name === "agent") {
    return [
      { label: "Agents", route: { name: "agents", spaceId: route.spaceId } },
      { label: entityLabels[route.agentId] ?? "Agent", route },
    ]
  }
  if (route.name === "account") {
    const sectionLabel = (() => {
      switch (route.section) {
        case "usage":
          return "Usage"
        case "webhook":
          return "Webhook Keys"
        case "chat":
          return "Chat accounts"
        case "invitations":
          return "Invitations"
        case "general":
        default:
          return "General"
      }
    })()
    return [
      { label: "Account", route: { name: "account", section: "general" } },
      { label: sectionLabel, route },
    ]
  }
  if (route.name === "space") {
    const sectionLabel = (() => {
      switch (route.section) {
        case "members":
          return "Members"
        case "memberNew":
          return "Invite Member"
        case "plugins":
          return "Plugins"
        case "security":
          return "Security"
        case "secrets":
          return "Secrets"
        case "audit":
          return "Audit"
        case "overview":
        default:
          return "Overview"
      }
    })()
    return [
      { label: "Space settings", route: { name: "space", spaceId: route.spaceId, section: "overview" } },
      { label: sectionLabel, route },
    ]
  }
  if (route.name === "admin") {
    const sectionLabel = (() => {
      switch (route.section) {
        case "administrators":
          return "Administrators"
        case "accounts":
          return "Accounts"
        case "spaces":
          return "Spaces"
        case "models":
          return "Models"
        case "plugins":
          return "Plugins"
        case "audit":
          return "Audit"
        case "overview":
        default:
          return "Overview"
      }
    })()
    return [
      { label: "Administration", route: { name: "admin", section: "overview" } },
      { label: sectionLabel, route },
    ]
  }
  if (route.name === "workflows") {
    return [{ label: "Workflows", route: { name: "workflows", spaceId: route.spaceId } }]
  }
  if (route.name === "workflow") {
    return [
      { label: "Workflows", route: { name: "workflows", spaceId: route.spaceId } },
      { label: entityLabels[route.workflowId] ?? "Workflow", route },
    ]
  }
  if (route.name === "workflowRun") {
    return [
      { label: "Workflows", route: { name: "workflows", spaceId: route.spaceId } },
      { label: entityLabels[route.workflowRunId] ?? "Workflow Run", route },
    ]
  }
  if (route.name === "schedules") {
    return [{ label: "Schedules", route: { name: "schedules", spaceId: route.spaceId } }]
  }
  if (route.name === "issues") {
    return [{ label: "Issues", route: { name: "issues", spaceId: route.spaceId } }]
  }
  if (route.name === "issue") {
    return [
      { label: "Issues", route: { name: "issues", spaceId: route.spaceId } },
      { label: entityLabels[route.issueId] ?? "Issue", route },
    ]
  }
  if (route.name === "artifacts") {
    return [{ label: "Artifacts", route: { name: "artifacts", spaceId: route.spaceId } }]
  }
  if (route.name === "artifact") {
    return currentSpaceId
      ? [
          { label: "Artifacts", route: { name: "artifacts", spaceId: currentSpaceId } },
          { label: entityLabels[route.artifactId] ?? "Artifact", route },
        ]
      : [{ label: entityLabels[route.artifactId] ?? "Artifact", route }]
  }
  if (route.name === "marketplace") {
    return [{ label: "Marketplace", route: { name: "marketplace" } }]
  }
  if (route.name === "task") {
    // A task's parents (agent / issue / conversation) are not in the route, so
    // the detail page publishes the trail; fall back until it loads.
    return (
      breadcrumbTrails[route.taskId] ?? [
        { label: "Chat", route: { name: "chat", spaceId: route.spaceId } },
        { label: "Task", route },
      ]
    )
  }
  if (route.name === "chat" && route.conversationId) {
    const conv = conversations.find((c) => c.id === route.conversationId)
    const convLabel = conv?.title?.trim() || conv?.timeLabel || "Conversation"
    return [
      { label: "Chat", route: { name: "chat", spaceId: route.spaceId } },
      { label: convLabel, route },
    ]
  }
  if (route.name === "chat") {
    return [{ label: "Chat", route }]
  }
  if (route.name === "help") {
    return [{ label: "Help", route: { name: "help" } }]
  }
  if (route.name === "notFound") {
    return [{ label: "Page not found", route }]
  }
  return []
}

export function Breadcrumbs({ route, conversations = [] }: BreadcrumbsProps) {
  const crumbs = useBreadcrumbs(route, conversations)
  const [overflowOpen, setOverflowOpen] = useState(false)

  // Keep the current object and its nearest parent inline; earlier ancestors
  // collapse behind an overflow disclosure rather than crowding or wrapping.
  const collapsedAncestors = crumbs.length > 2 ? crumbs.slice(0, -2) : []
  const visibleCrumbs = collapsedAncestors.length > 0 ? crumbs.slice(-2) : crumbs

  return (
    <nav className="breadcrumbs" aria-label="Breadcrumb">
      {collapsedAncestors.length > 0 && (
        <span className="breadcrumbs__segment breadcrumbs__segment--overflow">
          <button
            type="button"
            className="breadcrumbs__link breadcrumbs__overflow-toggle"
            aria-expanded={overflowOpen}
            aria-haspopup="menu"
            aria-label="Show earlier breadcrumbs"
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
