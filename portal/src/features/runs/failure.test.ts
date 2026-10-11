import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"
import { explainRunFailure, failureText, type RunFailureFacts } from "./failure"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)
const zh: Translate<MessageKey> = (key, vars) => translate("zh-CN", key, vars)

function failed(over: Partial<RunFailureFacts> = {}): RunFailureFacts {
  return { status: "FAILED", agentId: "ag_1", errorMessage: "  secret grant unavailable: secret is disabled (409)  ", ...over }
}

const disabledGrant = failed({
  failureClass: "space_configuration",
  failureCause: { kind: "secret_grant", secret_id: "sec_1", secret_problem: "disabled" },
})

describe("explainRunFailure", () => {
  it("explains nothing for a run that did not fail", () => {
    expect(explainRunFailure({ status: "SUCCEEDED" })).toBeNull()
    // A stopped run is not a failure to explain.
    expect(explainRunFailure({ status: "CANCELED", failureClass: "run" })).toBeNull()
  })

  it("names the Secret and the Agent, and leads with the fix, for a refused grant", () => {
    const explained = explainRunFailure(disabledGrant, { agent: "QA Blocked Agent", secret: "deploy-token" })!
    expect(explained.primary).toBe("fixAgent")
    expect(explained.leadsWithFix).toBe(true)
    const text = failureText(explained, t)
    expect(text.title).toBe("This Agent's Secret grant is disabled")
    expect(text.body).toContain("QA Blocked Agent")
    expect(text.body).toContain("“deploy-token”")
  })

  it("keeps the server's text, trimmed, for the details", () => {
    expect(explainRunFailure(disabledGrant)!.raw).toBe("secret grant unavailable: secret is disabled (409)")
    expect(explainRunFailure(failed({ errorMessage: "  " }))!.raw).toBeNull()
  })

  it("falls back to the Secret's id and to 'This Agent' when the names are not readable", () => {
    const text = failureText(explainRunFailure(disabledGrant)!, t)
    expect(text.body).toMatch(/^This Agent needs the Secret “sec_1”/)
  })

  it("explains each Secret problem in its own words", () => {
    const gone = explainRunFailure(
      failed({ failureClass: "space_configuration", failureCause: { kind: "secret_grant", secret_id: "sec_1", secret_problem: "unavailable" } }),
    )!
    expect(failureText(gone, t).title).toBe("This Agent's Secret grant no longer resolves")
    const item = explainRunFailure(
      failed({
        failureClass: "space_configuration",
        failureCause: { kind: "secret_grant", secret_id: "sec_1", secret_problem: "item_missing", secret_item: "token" },
      }),
    )!
    expect(failureText(item, t).body).toContain("“token”")
  })

  it("leads to the Agent for a Space configuration failure with no recorded cause", () => {
    const explained = explainRunFailure(failed({ failureClass: "space_configuration" }))!
    expect(explained.primary).toBe("openAgent")
    expect(explained.leadsWithFix).toBe(true)
    // Without an Agent there is nothing to open; the details are the next step.
    const noAgent = explainRunFailure(failed({ failureClass: "space_configuration", agentId: null }))!
    expect(noAgent.primary).toBe("runDetails")
    expect(noAgent.leadsWithFix).toBe(false)
  })

  it("leads with a retry for failures that are transient or the platform's", () => {
    for (const failureClass of ["model", "dispatch", "worker_lost", "abandoned", "interrupted", "infrastructure"]) {
      const explained = explainRunFailure(failed({ failureClass }))!
      expect(explained.primary, failureClass).toBe("retry")
      expect(explained.leadsWithFix, failureClass).toBe(false)
    }
  })

  it("leads with what the run did when it failed inside the run", () => {
    expect(explainRunFailure(failed({ failureClass: "run" }))!.primary).toBe("runDetails")
  })

  it("says the cause was not recorded for an unclassified or older run", () => {
    for (const failureClass of ["unclassified", undefined, "something_new"]) {
      const explained = explainRunFailure(failed({ failureClass }))!
      expect(failureText(explained, t).title).toBe("This run failed")
      expect(explained.primary).toBe("retry")
    }
  })

  it("explains in Chinese, keeping entity names in English", () => {
    const text = failureText(explainRunFailure(disabledGrant, { agent: "QA Blocked Agent", secret: "deploy-token" })!, zh)
    expect(text.title).toBe("该 Agent 的密钥授权已被禁用")
    expect(text.body).toContain("QA Blocked Agent")
    expect(failureText(explainRunFailure(disabledGrant)!, zh).body).toMatch(/^该 Agent 需要密钥“sec_1”/)
  })
})
