import type { Translate } from "@buildmax/gui"
import { describe, expect, it } from "vitest"
import { translate, type MessageKey } from "../../i18n"
import { contributionSummary, shortDigest } from "./AdminPlugins"
import type { ApiPluginRelease } from "../../lib/api/types"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function release(inspection: ApiPluginRelease["inspection"]): ApiPluginRelease {
  return {
    plugin_name: "code-review",
    version: "1.0.0",
    digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    object_key: "k",
    size_bytes: 1024,
    inspection,
    source: {},
    published_by: "u_admin",
    published_at: "1970-01-01T00:00:01Z",
  }
}

describe("contributionSummary", () => {
  it("counts what a release brings", () => {
    expect(
      contributionSummary(
        release({ skills: ["review"], subagents: [{ name: "reviewer" }], hooks: [] }),
        t,
      ),
    ).toBe("1 skill, 1 subagent")
  })

  it("pluralises", () => {
    expect(contributionSummary(release({ skills: ["a", "b"], mcp: [{ id: "x", transport: "stdio" }] }), t)).toBe(
      "2 skills, 1 MCP server",
    )
  })

  // A release that contributes nothing this build reads is a real state, and
  // saying so beats an empty cell somebody has to interpret.
  it("names an empty release", () => {
    expect(contributionSummary(release({}), t)).toBe("nothing this build recognises")
  })
})

describe("shortDigest", () => {
  it("drops the label and keeps enough to tell releases apart", () => {
    expect(shortDigest("sha256:0123456789abcdef0123")).toBe("0123456789ab")
  })

  it("leaves an unlabelled digest alone", () => {
    expect(shortDigest("0123456789abcdef")).toBe("0123456789ab")
  })
})
