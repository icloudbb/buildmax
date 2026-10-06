import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"
import type { ApiAgent, ApiPluginActivation, ApiPluginRelease } from "../../lib/api/types"
import { activationSummary } from "./SpacePlugins"
import { buildPluginRow, contributesExecutable, isNewer } from "./model"

// The English catalog, which every expectation below is written against.
const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function release(version: string, extra: Partial<ApiPluginRelease> = {}): ApiPluginRelease {
  return {
    plugin_name: "code-review",
    version,
    digest: `sha256:${version}`,
    object_key: `bm/code-review/${version}`,
    size_bytes: 1,
    inspection: { skills: ["review"] },
    source: {},
    published_by: "u_admin",
    published_at: "2026-08-01T00:00:00Z",
    ...extra,
  }
}

function activation(version: string, extra: Partial<ApiPluginActivation> = {}): ApiPluginActivation {
  return {
    id: "pa_1",
    space_id: "tm_1",
    plugin_name: "code-review",
    version,
    digest: `sha256:${version}`,
    enabled: true,
    origin: "curated",
    activated_by: "u_admin",
    activated_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    ...extra,
  }
}

function agent(name: string, plugins?: string[]): ApiAgent {
  return {
    id: `a_${name}`,
    user_id: "u_1",
    space_id: "tm_1",
    name,
    description: "",
    instructions: "",
    plugins,
    revision: 1,
    created_at: "2026-08-01T00:00:00Z",
  }
}

function row(args: {
  releases: ApiPluginRelease[]
  activation?: ApiPluginActivation | null
  agents?: ApiAgent[]
}) {
  return buildPluginRow({
    name: "code-review",
    displayName: "Code Review",
    description: "",
    releases: args.releases,
    activation: args.activation ?? null,
    agents: args.agents ?? [],
  })
}

describe("contributesExecutable", () => {
  it("is true for hooks and for MCP servers", () => {
    expect(
      contributesExecutable(release("1.0.0", { inspection: { hooks: [{ event: "pre_tool_use", type: "command" }] } })),
    ).toBe(true)
    expect(
      contributesExecutable(release("1.0.0", { inspection: { mcp: [{ id: "s", transport: "stdio" }] } })),
    ).toBe(true)
    expect(contributesExecutable(release("1.0.0"))).toBe(false)
  })
})

describe("buildPluginRow", () => {
  it("offers the newest release that could actually be activated", () => {
    const got = row({
      releases: [
        release("1.0.0"),
        release("2.0.0", { inspection: { hooks: [{ event: "pre_tool_use", type: "command" }] } }),
      ],
    })
    // 2.0.0 is newer but contributes a hook, and offering an update the server
    // would refuse is worse than offering none.
    expect(got.newest?.version).toBe("1.0.0")
  })

  it("marks a plugin whose every release is executable", () => {
    const got = row({
      releases: [release("1.0.0", { inspection: { mcp: [{ id: "s", transport: "stdio" }] } })],
    })
    expect(got.newest).toBeNull()
    expect(got.executableOnly).toBe(true)
    expect(activationSummary(got, t)).toContain("Cannot be activated yet")
  })

  it("reports a newer release without moving anything", () => {
    const got = row({ releases: [release("1.0.0"), release("2.0.0")], activation: activation("1.0.0") })
    expect(got.staleVersion).toBe("2.0.0")
    expect(got.activation?.version).toBe("1.0.0")
  })

  it("does not call a deliberately older pin stale", () => {
    const got = row({ releases: [release("1.0.0"), release("2.0.0")], activation: activation("2.0.0") })
    expect(got.staleVersion).toBeNull()
  })

  it("names the agents that use it, and says so when none do", () => {
    const used = row({
      releases: [release("1.0.0")],
      activation: activation("1.0.0"),
      agents: [agent("Reviewer", ["code-review"]), agent("Writer")],
    })
    expect(used.usedBy).toEqual(["Reviewer"])
    expect(activationSummary(used, t)).toContain("named by Reviewer")

    const unused = row({
      releases: [release("1.0.0")],
      activation: activation("1.0.0"),
      agents: [agent("Writer")],
    })
    // Nothing is inherited, so an activation no agent names is in force nowhere.
    expect(activationSummary(unused, t)).toContain("no agent names it")
  })

  it("says a suspended activation is suspended, keeping its version", () => {
    const got = row({
      releases: [release("1.0.0")],
      activation: activation("1.0.0", { enabled: false }),
    })
    expect(activationSummary(got, t)).toContain("Suspended at 1.0.0")
  })
})

describe("isNewer", () => {
  it("orders stable versions and refuses anything it cannot parse", () => {
    expect(isNewer("1.2.0", "1.1.9")).toBe(true)
    expect(isNewer("1.1.9", "1.2.0")).toBe(false)
    expect(isNewer("1.0.0", "1.0.0")).toBe(false)
    expect(isNewer("2.0.0-rc.1", "1.0.0")).toBe(false)
  })
})
