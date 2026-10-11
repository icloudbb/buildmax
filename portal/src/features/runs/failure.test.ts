import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"
import { explainRunFailure, failureText, type RunFailureContext, type RunFailureFacts } from "./failure"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)
const zh: Translate<MessageKey> = (key, vars) => translate("zh-CN", key, vars)

function failed(over: Partial<RunFailureFacts> = {}): RunFailureFacts {
  return { status: "FAILED", agentId: "ag_1", errorMessage: "  secret grant unavailable: secret is disabled (409)  ", ...over }
}

const manager: RunFailureContext = { canManage: true }
const member: RunFailureContext = { canManage: false }

const disabledGrant = failed({
  failureClass: "space_configuration",
  failureCause: { kind: "secret_grant", secret_id: "sec_1", secret_problem: "disabled" },
})

const pluginRefusal = (problem: "not_activated" | "suspended") =>
  failed({ failureClass: "space_configuration", failureCause: { kind: "plugin", plugin_name: "code-review", plugin_problem: problem } })

const DAY = 24 * 60 * 60 * 1000
const refusedAt = "2026-10-01T12:00:00Z"
const tokenQuota = failed({
  failureClass: "space_configuration",
  endedAt: refusedAt,
  failureCause: { kind: "usage_quota", quota_limit: "tokens", quota_max: 1000000, quota_period_days: 30 },
})

describe("explainRunFailure", () => {
  it("explains nothing for a run that did not fail", () => {
    expect(explainRunFailure({ status: "SUCCEEDED" }, manager)).toBeNull()
    // A stopped run is not a failure to explain.
    expect(explainRunFailure({ status: "CANCELED", failureClass: "run" }, manager)).toBeNull()
  })

  it("names the Secret and the Agent, and leads with the fix, for a refused grant", () => {
    const explained = explainRunFailure(disabledGrant, { ...manager, agent: "QA Blocked Agent", secret: "deploy-token" })!
    expect(explained.primary).toBe("fixAgent")
    expect(explained.leadsWithFix).toBe(true)
    const text = failureText(explained, t)
    expect(text.title).toBe("This Agent's Secret grant is disabled")
    expect(text.body).toBe(
      "QA Blocked Agent needs the Secret “deploy-token”, which is disabled. Re-enable the Secret or change the Agent's grant, then run again.",
    )
  })

  it("keeps the server's text, trimmed, for the details", () => {
    expect(explainRunFailure(disabledGrant, manager)!.raw).toBe("secret grant unavailable: secret is disabled (409)")
    expect(explainRunFailure(failed({ errorMessage: "  " }), manager)!.raw).toBeNull()
  })

  it("falls back to the Secret's id for an owner or admin, and to 'This Agent', when the names did not load", () => {
    const text = failureText(explainRunFailure(disabledGrant, manager)!, t)
    expect(text.body).toMatch(/^This Agent needs the Secret “sec_1”/)
  })

  it("never shows a member the Secret's id, and tells them who can fix it", () => {
    const explained = explainRunFailure(disabledGrant, member)!
    expect(explained.primary).toBe("askSpaceAdmin")
    expect(explained.leadsWithFix).toBe(true)
    const text = failureText(explained, t)
    expect(text.body).not.toContain("sec_1")
    expect(text.body).toBe(
      "This Agent needs a Secret granted to it, which is disabled. Only a Space owner or admin can fix this; ask one to, then run again.",
    )
  })

  it("explains each Secret problem in its own words", () => {
    const gone = explainRunFailure(
      failed({ failureClass: "space_configuration", failureCause: { kind: "secret_grant", secret_id: "sec_1", secret_problem: "unavailable" } }),
      manager,
    )!
    expect(failureText(gone, t).title).toBe("This Agent's Secret grant no longer resolves")
    const item = explainRunFailure(
      failed({
        failureClass: "space_configuration",
        failureCause: { kind: "secret_grant", secret_id: "sec_1", secret_problem: "item_missing", secret_item: "token" },
      }),
      member,
    )!
    expect(failureText(item, t).body).toContain("the item “token” of a Secret granted to it")
  })

  it("names the plugin and leads an owner or admin to the Space's plugin settings", () => {
    const notActivated = explainRunFailure(pluginRefusal("not_activated"), { ...manager, agent: "Reviewer" })!
    expect(notActivated.primary).toBe("openPlugins")
    expect(notActivated.leadsWithFix).toBe(true)
    expect(failureText(notActivated, t)).toEqual({
      title: "This Agent's plugin is not activated in this Space",
      body: "Reviewer names the plugin “code-review”, which this Space has not activated. Activate it in the Space's plugin settings, or remove it from the Agent, then run again.",
    })
    const suspended = explainRunFailure(pluginRefusal("suspended"), manager)!
    expect(failureText(suspended, t).body).toContain("whose activation in this Space is suspended. Resume it")
  })

  it("tells a member who can fix a refused plugin, with nothing to open", () => {
    const explained = explainRunFailure(pluginRefusal("suspended"), member)!
    expect(explained.primary).toBe("askSpaceAdmin")
    expect(failureText(explained, t).body).toMatch(/suspended\. Only a Space owner or admin can fix this/)
  })

  it("says which quota was spent and that a System Administrator changes it, whoever reads it", () => {
    for (const reader of [manager, member]) {
      const explained = explainRunFailure(tokenQuota, { ...reader, now: Date.parse(refusedAt) + DAY })!
      expect(explained.primary).toBe("askSystemAdmin")
      expect(explained.leadsWithFix).toBe(true)
      expect(failureText(explained, t)).toEqual({
        title: "The Space's usage quota is spent",
        body: "This Space has used its allowance of 1,000,000 tokens in a rolling 30-day window, so the model call was refused. A System Administrator sets the Space's quota: ask one to raise it, or run again once usage over the last 30 days falls below the limit.",
      })
    }
    const runs = explainRunFailure(
      failed({ failureClass: "space_configuration", failureCause: { kind: "usage_quota", quota_limit: "runs", quota_max: 1, quota_period_days: 7 } }),
      member,
    )!
    expect(failureText(runs, t).body).toMatch(/^This Space has used its allowance of 1 run in a rolling 7-day window/)
  })

  it("leads with running again once a whole quota window has passed since the refusal", () => {
    const explained = explainRunFailure(tokenQuota, { ...member, now: Date.parse(refusedAt) + 31 * DAY })!
    expect(explained.primary).toBe("retry")
    expect(explained.leadsWithFix).toBe(false)
    expect(failureText(explained, t).body).toContain("More than 30 days have passed since")
  })

  it("explains a quota refusal whose limit was not recorded", () => {
    const explained = explainRunFailure(failed({ failureClass: "space_configuration", failureCause: { kind: "usage_quota" } }), member)!
    expect(explained.primary).toBe("askSystemAdmin")
    expect(failureText(explained, t).body).toBe(
      "This Space has used its whole usage allowance, so the model call was refused. A System Administrator sets the Space's quota: ask one to raise it, or run again later.",
    )
  })

  it("leads to the Agent for a Space configuration failure with no recorded cause", () => {
    const explained = explainRunFailure(failed({ failureClass: "space_configuration" }), manager)!
    expect(explained.primary).toBe("openAgent")
    expect(explained.leadsWithFix).toBe(true)
    // Without an Agent there is nothing to open; the details are the next step.
    const noAgent = explainRunFailure(failed({ failureClass: "space_configuration", agentId: null }), manager)!
    expect(noAgent.primary).toBe("runDetails")
    expect(noAgent.leadsWithFix).toBe(false)
    // A member cannot change the Space's setup, so is told who can.
    expect(explainRunFailure(failed({ failureClass: "space_configuration" }), member)!.primary).toBe("askSpaceAdmin")
  })

  it("leads with a retry for failures that are transient or the platform's", () => {
    for (const failureClass of ["model", "dispatch", "worker_lost", "abandoned", "interrupted", "infrastructure"]) {
      const explained = explainRunFailure(failed({ failureClass }), member)!
      expect(explained.primary, failureClass).toBe("retry")
      expect(explained.leadsWithFix, failureClass).toBe(false)
    }
  })

  it("leads with what the run did when it failed inside the run", () => {
    expect(explainRunFailure(failed({ failureClass: "run" }), manager)!.primary).toBe("runDetails")
  })

  it("says the cause was not recorded for an unclassified or older run", () => {
    for (const failureClass of ["unclassified", undefined, "something_new"]) {
      const explained = explainRunFailure(failed({ failureClass }), manager)!
      expect(failureText(explained, t).title).toBe("This run failed")
      expect(explained.primary).toBe("retry")
    }
  })

  it("explains in Chinese, keeping entity names in English", () => {
    const text = failureText(explainRunFailure(disabledGrant, { ...manager, agent: "QA Blocked Agent", secret: "deploy-token" })!, zh)
    expect(text.title).toBe("该 Agent 的密钥授权已被禁用")
    expect(text.body).toBe("QA Blocked Agent 需要密钥“deploy-token”，但该密钥已被禁用。请重新启用该密钥或修改 Agent 的授权，然后再次运行。")
    expect(failureText(explainRunFailure(disabledGrant, manager)!, zh).body).toMatch(/^该 Agent 需要密钥“sec_1”/)
    expect(failureText(explainRunFailure(disabledGrant, member)!, zh).body).toBe(
      "该 Agent 需要授予它的某个密钥，但该密钥已被禁用。只有 Space 所有者或管理员可以修复此问题，请联系他们修复后再次运行。",
    )
    expect(failureText(explainRunFailure(pluginRefusal("not_activated"), manager)!, zh).title).toBe("该 Agent 的插件未在此 Space 中启用")
    expect(failureText(explainRunFailure(tokenQuota, { ...member, now: Date.parse(refusedAt) })!, zh).body).toContain(
      "滚动 30 天内 1,000,000 个 token 的额度",
    )
  })
})
