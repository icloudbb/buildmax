import type { Route, SpaceScopedRouteName } from "./types"

/** Every Space-scoped route name that is itself a collection -- i.e. a
 * Space-switch destination, never just a detail page passing through. */
type SpaceCollectionRouteName = "chat" | "explore" | "agents" | "space" | "workflows" | "schedules" | "issues" | "artifacts"

/**
 * Where a Space-scoped route lands after a Space switch: its own collection
 * (or, for the routes with no collection page, the closest sensible one).
 *
 * A `Record` over every `SpaceScopedRouteName` -- TypeScript refuses to
 * compile this file if a new Space-scoped route name is added to `Route`
 * without an entry here, which is what makes this exhaustive rather than a
 * second if-chain someone can forget to extend. That gap is exactly what let
 * `agent` and `task` fall through the old per-route redirect list in
 * `App.tsx` before this file existed.
 */
const SWITCH_TARGET: Record<SpaceScopedRouteName, SpaceCollectionRouteName> = {
  chat: "chat",
  // No Task collection page exists, and a Task can originate from either an
  // Agent or an Issue with no reliable way to pick one on switch -- Chat is
  // the Space's front door, matching the pre-slice-2 `conversation -> home`
  // precedent.
  task: "chat",
  explore: "explore",
  agents: "agents",
  agent: "agents",
  space: "space",
  workflows: "workflows",
  workflowNew: "workflows",
  workflow: "workflows",
  workflowRun: "workflows",
  schedules: "schedules",
  issues: "issues",
  issue: "issues",
  artifacts: "artifacts",
}

/**
 * The Route a Space switch should land on, or `null` if `route` should stay
 * put (every global route: Account, Admin, Marketplace, Help, Login).
 *
 * Artifact detail is the one exception that carries no `spaceId` of its own
 * (see docs/design/unified-artifacts.md section 6.1) yet still moves on a
 * switch: its data never depended on the selected Space, but leaving it up
 * while the shell now reads a different Space is a worse experience than
 * landing on that Space's own Artifacts collection.
 */
export function spaceSwitchTarget(route: Route, targetSpaceId: string): Route | null {
  if (route.name === "artifact") return { name: "artifacts", spaceId: targetSpaceId }
  if (!("spaceId" in route)) return null
  switch (SWITCH_TARGET[route.name as SpaceScopedRouteName]) {
    case "chat":
      return { name: "chat", spaceId: targetSpaceId }
    case "explore":
      return { name: "explore", spaceId: targetSpaceId }
    case "agents":
      return { name: "agents", spaceId: targetSpaceId }
    case "space":
      return { name: "space", spaceId: targetSpaceId, section: "overview" }
    case "workflows":
      return { name: "workflows", spaceId: targetSpaceId }
    case "schedules":
      return { name: "schedules", spaceId: targetSpaceId }
    case "issues":
      return { name: "issues", spaceId: targetSpaceId }
    case "artifacts":
      return { name: "artifacts", spaceId: targetSpaceId }
  }
}
