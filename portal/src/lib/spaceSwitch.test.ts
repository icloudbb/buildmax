import { describe, expect, it } from "vitest"
import { spaceSwitchTarget } from "./spaceSwitch"
import type { Route } from "./types"

const OLD = "s_old"
const NEW = "s_new"

describe("spaceSwitchTarget", () => {
  it.each([
    [{ name: "chat", spaceId: OLD }, { name: "chat", spaceId: NEW }],
    [{ name: "chat", spaceId: OLD, conversationId: "c_1" }, { name: "chat", spaceId: NEW }],
    [{ name: "task", spaceId: OLD, taskId: "t_1" }, { name: "chat", spaceId: NEW }],
    [{ name: "explore", spaceId: OLD }, { name: "explore", spaceId: NEW }],
    [{ name: "agents", spaceId: OLD }, { name: "agents", spaceId: NEW }],
    [{ name: "agent", spaceId: OLD, agentId: "a_1" }, { name: "agents", spaceId: NEW }],
    [
      { name: "space", spaceId: OLD, section: "members" },
      { name: "space", spaceId: NEW, section: "overview" },
    ],
    [{ name: "workflows", spaceId: OLD }, { name: "workflows", spaceId: NEW }],
    [{ name: "workflowNew", spaceId: OLD }, { name: "workflows", spaceId: NEW }],
    [{ name: "workflow", spaceId: OLD, workflowId: "w_1" }, { name: "workflows", spaceId: NEW }],
    [
      { name: "workflowRun", spaceId: OLD, workflowRunId: "wr_1" },
      { name: "workflows", spaceId: NEW },
    ],
    [{ name: "issues", spaceId: OLD }, { name: "issues", spaceId: NEW }],
    [{ name: "issue", spaceId: OLD, issueId: "i_1" }, { name: "issues", spaceId: NEW }],
    [{ name: "artifacts", spaceId: OLD }, { name: "artifacts", spaceId: NEW }],
    // The one route with no spaceId of its own that still moves: see
    // docs/design/portal-navigation-and-space-context.md.
    [{ name: "artifact", artifactId: "art_1" }, { name: "artifacts", spaceId: NEW }],
  ] satisfies Array<[Route, Route]>)("moves %o to %o", (route, expected) => {
    expect(spaceSwitchTarget(route, NEW)).toEqual(expected)
  })

  it.each([
    { name: "account", section: "general" },
    { name: "admin", section: "overview" },
    { name: "marketplace" },
    { name: "help" },
    { name: "login" },
  ] satisfies Route[])("leaves the global route %o untouched", (route) => {
    expect(spaceSwitchTarget(route, NEW)).toBeNull()
  })
})
