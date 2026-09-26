import { describe, expect, it } from "vitest"
import { buildHash, parseHash } from "./router"
import type { Route } from "./lib/types"

const SPACE = "s_test123"

describe("hash router", () => {
  it.each([
    ["#/", { name: "chat", spaceId: SPACE }],
    ["#/login", { name: "login" }],
    [`#/spaces/${SPACE}/chat`, { name: "chat", spaceId: SPACE }],
    [`#/spaces/${SPACE}/chat/c_123`, { name: "chat", spaceId: SPACE, conversationId: "c_123" }],
    [`#/spaces/${SPACE}/files`, { name: "explore", spaceId: SPACE }],
    [`#/spaces/${SPACE}/agents`, { name: "agents", spaceId: SPACE }],
    [`#/spaces/${SPACE}/agents/a_123`, { name: "agent", spaceId: SPACE, agentId: "a_123" }],
    ["#/account", { name: "account", section: "general" }],
    ["#/account/usage", { name: "account", section: "usage" }],
    ["#/account/webhook", { name: "account", section: "webhook" }],
    ["#/account/chat", { name: "account", section: "chat" }],
    // The address a chat bot sends carries its link code.
    ["#/account/chat/ABCD-EFGH", { name: "account", section: "chat", code: "ABCD-EFGH" }],
    ["#/account/invitations", { name: "account", section: "invitations" }],
    [`#/spaces/${SPACE}/settings`, { name: "space", spaceId: SPACE, section: "overview" }],
    [`#/spaces/${SPACE}/settings/members`, { name: "space", spaceId: SPACE, section: "members" }],
    [`#/spaces/${SPACE}/settings/members/new`, { name: "space", spaceId: SPACE, section: "memberNew" }],
    // A section reachable only by clicking a tab cannot be linked, shared, or
    // survive a reload, so every one of them needs a URL.
    [`#/spaces/${SPACE}/settings/audit`, { name: "space", spaceId: SPACE, section: "audit" }],
    [`#/spaces/${SPACE}/settings/security`, { name: "space", spaceId: SPACE, section: "security" }],
    // Deployment administration is a separate area from space settings, and
    // its sections are linkable for the same reason the space ones are.
    ["#/admin", { name: "admin", section: "overview" }],
    ["#/admin/administrators", { name: "admin", section: "administrators" }],
    ["#/admin/accounts", { name: "admin", section: "accounts" }],
    // An open account detail is a linkable address, so it survives a reload.
    ["#/admin/accounts/u_7Kq2", { name: "admin", section: "accounts", userId: "u_7Kq2" }],
    ["#/admin/spaces", { name: "admin", section: "spaces" }],
    // Account's plugin catalog was a duplicate of Marketplace; the old address
    // redirects rather than landing on a removed tab. See
    // docs/design/portal-data-and-plugin-surfaces.md.
    ["#/account/plugins", { name: "marketplace" }],
    ["#/admin/models", { name: "admin", section: "models" }],
    ["#/admin/plugins", { name: "admin", section: "plugins" }],
    ["#/admin/audit", { name: "admin", section: "audit" }],
    [`#/spaces/${SPACE}/workflows`, { name: "workflows", spaceId: SPACE }],
    [`#/spaces/${SPACE}/workflows/w_123`, { name: "workflow", spaceId: SPACE, workflowId: "w_123" }],
    [`#/spaces/${SPACE}/workflow-runs/wr_123`, { name: "workflowRun", spaceId: SPACE, workflowRunId: "wr_123" }],
    [`#/spaces/${SPACE}/schedules`, { name: "schedules", spaceId: SPACE }],
    [`#/spaces/${SPACE}/issues`, { name: "issues", spaceId: SPACE }],
    [`#/spaces/${SPACE}/issues/i_123`, { name: "issue", spaceId: SPACE, issueId: "i_123" }],
    [`#/spaces/${SPACE}/tasks/t_123`, { name: "task", spaceId: SPACE, taskId: "t_123" }],
    [`#/spaces/${SPACE}/artifacts`, { name: "artifacts", spaceId: SPACE }],
    // No space in the path: an artifact's id is the whole address, matching the
    // API. See docs/design/unified-artifacts.md section 6.1.
    ["#/artifact/gsyt7at6cjfr33d73mta", { name: "artifact", artifactId: "gsyt7at6cjfr33d73mta" }],
    ["#/marketplace", { name: "marketplace" }],
  ] satisfies Array<[string, Route]>)("parses %s", (hash, route) => {
    expect(parseHash(hash, SPACE)).toEqual(route)
  })

  it.each([
    [{ name: "login" }, "#/login"],
    [{ name: "chat", spaceId: SPACE }, `#/spaces/${SPACE}/chat`],
    [{ name: "chat", spaceId: SPACE, conversationId: "c_123" }, `#/spaces/${SPACE}/chat/c_123`],
    [{ name: "explore", spaceId: SPACE }, `#/spaces/${SPACE}/files`],
    [{ name: "agents", spaceId: SPACE }, `#/spaces/${SPACE}/agents`],
    [{ name: "agent", spaceId: SPACE, agentId: "a_123" }, `#/spaces/${SPACE}/agents/a_123`],
    [{ name: "account", section: "general" }, "#/account"],
    [{ name: "account", section: "usage" }, "#/account/usage"],
    [{ name: "admin", section: "overview" }, "#/admin"],
    [{ name: "admin", section: "administrators" }, "#/admin/administrators"],
    [{ name: "admin", section: "accounts" }, "#/admin/accounts"],
    [{ name: "admin", section: "accounts", userId: "u_7Kq2" }, "#/admin/accounts/u_7Kq2"],
    [{ name: "admin", section: "spaces" }, "#/admin/spaces"],
    [{ name: "admin", section: "models" }, "#/admin/models"],
    [{ name: "admin", section: "plugins" }, "#/admin/plugins"],
    [{ name: "admin", section: "audit" }, "#/admin/audit"],
    [{ name: "account", section: "webhook" }, "#/account/webhook"],
    [{ name: "account", section: "chat" }, "#/account/chat"],
    [{ name: "account", section: "chat", code: "ABCD-EFGH" }, "#/account/chat/ABCD-EFGH"],
    [{ name: "account", section: "invitations" }, "#/account/invitations"],
    [{ name: "space", spaceId: SPACE, section: "overview" }, `#/spaces/${SPACE}/settings`],
    [{ name: "space", spaceId: SPACE, section: "audit" }, `#/spaces/${SPACE}/settings/audit`],
    [{ name: "space", spaceId: SPACE, section: "security" }, `#/spaces/${SPACE}/settings/security`],
    [{ name: "space", spaceId: SPACE, section: "members" }, `#/spaces/${SPACE}/settings/members`],
    [{ name: "space", spaceId: SPACE, section: "memberNew" }, `#/spaces/${SPACE}/settings/members/new`],
    [{ name: "workflows", spaceId: SPACE }, `#/spaces/${SPACE}/workflows`],
    [{ name: "workflow", spaceId: SPACE, workflowId: "w_123" }, `#/spaces/${SPACE}/workflows/w_123`],
    [
      { name: "workflowRun", spaceId: SPACE, workflowRunId: "wr_123" },
      `#/spaces/${SPACE}/workflow-runs/wr_123`,
    ],
    [{ name: "schedules", spaceId: SPACE }, `#/spaces/${SPACE}/schedules`],
    [{ name: "issues", spaceId: SPACE }, `#/spaces/${SPACE}/issues`],
    [{ name: "issue", spaceId: SPACE, issueId: "i_123" }, `#/spaces/${SPACE}/issues/i_123`],
    [{ name: "task", spaceId: SPACE, taskId: "t_123" }, `#/spaces/${SPACE}/tasks/t_123`],
    [{ name: "artifacts", spaceId: SPACE }, `#/spaces/${SPACE}/artifacts`],
    [{ name: "artifact", artifactId: "gsyt7at6cjfr33d73mta" }, "#/artifact/gsyt7at6cjfr33d73mta"],
    [{ name: "marketplace" }, "#/marketplace"],
  ] satisfies Array<[Route, string]>)("builds %s", (route, hash) => {
    expect(buildHash(route)).toBe(hash)
  })

  it("resolves the bare root to the current Space's Chat", () => {
    expect(parseHash("#/", SPACE)).toEqual({ name: "chat", spaceId: SPACE })
    expect(parseHash("#", SPACE)).toEqual({ name: "chat", spaceId: SPACE })
  })

  it("renders not-found for an unrecognized hash, never silently falling through to Chat", () => {
    expect(parseHash("#/unknown/path", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash(`#/spaces/${SPACE}/bogus`, SPACE)).toEqual({ name: "notFound" })
  })

  it("renders not-found for a pre-migration flat hash -- the redirect was bounded to the migration, not a compatibility contract", () => {
    expect(parseHash("#/issues", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash("#/issue/i_123", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash("#/agents", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash("#/explore", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash("#/space", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash("#/conversations", SPACE)).toEqual({ name: "notFound" })
    expect(parseHash("#/home", SPACE)).toEqual({ name: "notFound" })
  })

  it("renders not-found for an incomplete Space-scoped path", () => {
    expect(parseHash(`#/spaces/${SPACE}/workflow-runs`, SPACE)).toEqual({ name: "notFound" })
    expect(parseHash(`#/spaces/${SPACE}/tasks`, SPACE)).toEqual({ name: "notFound" })
  })

  it("carries the Issue collection's view and filters in the URL, so a copied link reproduces the projection", () => {
    const route: Route = { name: "issues", spaceId: SPACE, view: "board", owner: "me", executor: "agent:a_1" }
    const hash = buildHash(route)
    expect(hash).toBe(`#/spaces/${SPACE}/issues?view=board&owner=me&executor=agent%3Aa_1`)
    expect(parseHash(hash, SPACE)).toEqual(route)
  })

  it("drops unknown or malformed Issue query values instead of refusing the page", () => {
    expect(parseHash(`#/spaces/${SPACE}/issues?view=grid&executor=person:u_1&owner=`, SPACE)).toEqual({
      name: "issues",
      spaceId: SPACE,
    })
  })

  it("ignores a query on a route that has none", () => {
    expect(parseHash(`#/spaces/${SPACE}/agents?view=board`, SPACE)).toEqual({ name: "agents", spaceId: SPACE })
  })

  it("builds a stable, self-parsing marker for not-found", () => {
    const hash = buildHash({ name: "notFound" })
    expect(parseHash(hash, SPACE)).toEqual({ name: "notFound" })
  })
})
