import { useState, useEffect, useMemo } from "react"
import type { Route } from "./lib/types"

/**
 * Path segment names used in the hash URL. Single source of truth for parseHash/buildHash.
 *
 * A Space-owned route's canonical shape is `#/spaces/{space_id}/<resource>[/<id>]`,
 * one shared plural segment per resource family for both its collection and its
 * detail (`issues` / `issues/{id}`), per
 * docs/design/portal-navigation-and-space-context.md. Global routes (Account,
 * Admin, Marketplace, Help, Login) never carry a Space prefix. Artifact detail
 * is the one ID-resolved exception -- no Space in its path at all.
 */
export const SEGMENT = {
  login: "login",
  spaces: "spaces",
  chat: "chat",
  tasks: "tasks",
  files: "files",
  agents: "agents",
  account: "account",
  // The Space-settings resource word under a Space prefix, e.g. `#/spaces/{id}/settings`.
  space: "settings",
  admin: "admin",
  workflows: "workflows",
  workflowRuns: "workflow-runs",
  schedules: "schedules",
  issues: "issues",
  artifacts: "artifacts",
  artifact: "artifact",
  marketplace: "marketplace",
  help: "help",
} as const

/**
 * Resolve the segments after `#/spaces/{space_id}/` into a Route. `rest[0]`
 * is the canonical plural resource word (`issues`, `agents`, ...).
 */
function parseSpaceScopedRoute(spaceId: string, rest: string[]): Route {
  const [resource, id, sub] = rest
  switch (resource) {
    case SEGMENT.chat:
      return { name: "chat", spaceId, conversationId: id || undefined }
    case SEGMENT.issues:
      return id ? { name: "issue", spaceId, issueId: id } : { name: "issues", spaceId }
    case SEGMENT.agents:
      return id ? { name: "agent", spaceId, agentId: id } : { name: "agents", spaceId }
    case SEGMENT.workflows:
      return id ? { name: "workflow", spaceId, workflowId: id } : { name: "workflows", spaceId }
    case SEGMENT.workflowRuns:
      if (id) return { name: "workflowRun", spaceId, workflowRunId: id }
      break
    case SEGMENT.schedules:
      // Space-wide overview only; a single schedule is edited on its agent's
      // detail page, so there is no schedule detail route.
      return { name: "schedules", spaceId }
    case SEGMENT.tasks:
      if (id) return { name: "task", spaceId, taskId: id }
      break
    case SEGMENT.files:
      return { name: "explore", spaceId }
    case SEGMENT.artifacts:
      return { name: "artifacts", spaceId }
    case SEGMENT.space:
      if (id === "members" && sub === "new") return { name: "space", spaceId, section: "memberNew" }
      if (id === "members") return { name: "space", spaceId, section: "members" }
      if (id === "plugins") return { name: "space", spaceId, section: "plugins" }
      if (id === "security") return { name: "space", spaceId, section: "security" }
      if (id === "secrets") return { name: "space", spaceId, section: "secrets" }
      if (id === "audit") return { name: "space", spaceId, section: "audit" }
      return { name: "space", spaceId, section: "overview" }
  }
  // An unrecognized or incomplete Space-scoped path, e.g. a bad resource word
  // or a detail path missing its id.
  return { name: "notFound" }
}

/**
 * Parse window.location.hash into a typed Route. `currentSpaceId` resolves
 * the bare `#/` entry point, the one route that carries no Space id of its
 * own.
 */
export function parseHash(hash: string, currentSpaceId: string): Route {
  const raw = hash.replace(/^#\/?/, "")
  const parts = raw.split("/").filter(Boolean)

  // --- Global routes: never carry a Space prefix. ---
  if (parts[0] === SEGMENT.login) {
    return { name: "login" }
  }
  if (parts[0] === SEGMENT.account) {
    if (parts[1] === "usage") return { name: "account", section: "usage" }
    if (parts[1] === "webhook") return { name: "account", section: "webhook" }
    // Account's plugin catalog was a duplicate of Marketplace at a different
    // scope; kept as a redirect, not dropped, because the old address is what
    // any saved link points at. See docs/design/portal-data-and-plugin-surfaces.md.
    if (parts[1] === "plugins") return { name: "marketplace" }
    if (parts[1] === "invitations") return { name: "account", section: "invitations" }
    return { name: "account", section: "general" }
  }
  if (parts[0] === SEGMENT.admin) {
    if (parts[1] === "administrators") return { name: "admin", section: "administrators" }
    if (parts[1] === "accounts") return { name: "admin", section: "accounts", userId: parts[2] || undefined }
    if (parts[1] === "spaces") return { name: "admin", section: "spaces" }
    if (parts[1] === "models") return { name: "admin", section: "models" }
    if (parts[1] === "llm-calls") return { name: "admin", section: "calls" }
    if (parts[1] === "plugins") return { name: "admin", section: "plugins" }
    if (parts[1] === "audit") return { name: "admin", section: "audit" }
    return { name: "admin", section: "overview" }
  }
  if (parts[0] === SEGMENT.marketplace) {
    return { name: "marketplace" }
  }
  // #/help opens the manual's first page; #/help/<slug> opens one page.
  if (parts[0] === SEGMENT.help) {
    return { name: "help", slug: parts[1] }
  }
  // An artifact's address is its id alone -- no space in the path, matching the
  // API. See docs/design/unified-artifacts.md section 6.1.
  if (parts[0] === SEGMENT.artifact && parts[1]) {
    return { name: "artifact", artifactId: parts[1] }
  }

  // --- Canonical Space-prefixed routes. ---
  if (parts[0] === SEGMENT.spaces && parts[1]) {
    return parseSpaceScopedRoute(parts[1], parts.slice(2))
  }

  // Bare `#/` is the one legitimate empty path: the Space's Chat. Anything
  // else here matched no route at all -- a genuinely unknown address, not a
  // fallback to guess from. The pre-Space-prefix hash shapes this used to
  // redirect were bounded to the migration and are gone now that
  // docs/design/portal-navigation-and-space-context.md is fully implemented.
  if (parts.length === 0) {
    return parseSpaceScopedRoute(currentSpaceId, [SEGMENT.chat])
  }
  return { name: "notFound" }
}

/** Convert a Route into a canonical hash string (includes leading #). */
export function buildHash(route: Route): string {
  switch (route.name) {
    case "login":
      return `#/${SEGMENT.login}`
    case "chat":
      return route.conversationId
        ? `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.chat}/${route.conversationId}`
        : `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.chat}`
    case "task":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.tasks}/${route.taskId}`
    case "explore":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.files}`
    case "agents":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.agents}`
    case "agent":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.agents}/${route.agentId}`
    case "account":
      switch (route.section) {
        case "usage":
          return `#/${SEGMENT.account}/usage`
        case "webhook":
          return `#/${SEGMENT.account}/webhook`
        case "invitations":
          return `#/${SEGMENT.account}/invitations`
        case "general":
        default:
          return `#/${SEGMENT.account}`
      }
    case "space": {
      const prefix = `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.space}`
      switch (route.section) {
        case "members":
          return `${prefix}/members`
        case "memberNew":
          return `${prefix}/members/new`
        case "plugins":
          return `${prefix}/plugins`
        case "security":
          return `${prefix}/security`
        case "secrets":
          return `${prefix}/secrets`
        case "audit":
          return `${prefix}/audit`
        case "overview":
        default:
          return prefix
      }
    }
    case "admin":
      switch (route.section) {
        case "administrators":
          return `#/${SEGMENT.admin}/administrators`
        case "accounts":
          return route.userId
            ? `#/${SEGMENT.admin}/accounts/${route.userId}`
            : `#/${SEGMENT.admin}/accounts`
        case "spaces":
          return `#/${SEGMENT.admin}/spaces`
        case "models":
          return `#/${SEGMENT.admin}/models`
        case "calls":
          return `#/${SEGMENT.admin}/llm-calls`
        case "plugins":
          return `#/${SEGMENT.admin}/plugins`
        case "audit":
          return `#/${SEGMENT.admin}/audit`
        case "overview":
        default:
          return `#/${SEGMENT.admin}`
      }
    case "workflows":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.workflows}`
    case "workflow":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.workflows}/${route.workflowId}`
    case "workflowRun":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.workflowRuns}/${route.workflowRunId}`
    case "schedules":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.schedules}`
    case "issues":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.issues}`
    case "issue":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.issues}/${route.issueId}`
    case "artifacts":
      return `#/${SEGMENT.spaces}/${route.spaceId}/${SEGMENT.artifacts}`
    case "artifact":
      return `#/${SEGMENT.artifact}/${route.artifactId}`
    case "marketplace":
      return `#/${SEGMENT.marketplace}`
    case "help":
      return route.slug ? `#/${SEGMENT.help}/${route.slug}` : `#/${SEGMENT.help}`
    case "notFound":
      // No canonical address of its own -- Portal never links here, it only
      // ever arrives by the hash already being unrecognized. This marker
      // parses back to the same state, rather than clobbering whatever the
      // reader actually typed.
      return "#/404"
  }
}

/** Navigate to a Route by setting the hash. */
export function navigate(route: Route): void {
  window.location.hash = buildHash(route)
}

/**
 * React hook: returns the current Route and re-renders on hashchange.
 * `currentSpaceId` resolves the bare `#/` entry point, which carries no
 * Space id of its own -- pass `""` while it is still unresolved (e.g. the
 * account's Spaces have not loaded yet); every Space-scoped Route's
 * `spaceId` will read as `""` until a real one is available.
 */
export function useHashRoute(currentSpaceId: string): Route {
  const [hash, setHash] = useState<string>(() => window.location.hash)

  useEffect(() => {
    function onHashChange() {
      setHash(window.location.hash)
    }
    window.addEventListener("hashchange", onHashChange)
    return () => window.removeEventListener("hashchange", onHashChange)
  }, [])

  return useMemo(() => parseHash(hash, currentSpaceId), [hash, currentSpaceId])
}
