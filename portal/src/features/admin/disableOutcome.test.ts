import type { Translate } from "@buildmax/gui"
import { describe, expect, it } from "vitest"
import { translate, type MessageKey } from "../../i18n"
import type { ApiAdminUserAfterDisable } from "../../lib/api/types"
import { describeDisableOutcome } from "./disableOutcome"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function after(partial: Partial<ApiAdminUserAfterDisable>): ApiAdminUserAfterDisable {
  return {
    id: "u_1",
    email: "gone@corp.com",
    kind: "human",
    has_password: true,
    created_at: "2026-09-01T00:00:00Z",
    disabled_at: "2026-09-26T00:00:00Z",
    sessions_revoked: 2,
    ...partial,
  }
}

describe("describeDisableOutcome", () => {
  it("reports a complete cleanup as done", () => {
    const got = describeDisableOutcome(after({ runs_canceled: 1 }), t)
    expect(got.incomplete).toBe(false)
    expect(got.message).toBe("gone@corp.com is disabled. 2 sessions revoked, 1 run canceled.")
  })

  it("says the account is disabled even when cleanup partly failed", () => {
    // The gate committed before cleanup ran. Reading this as a failed disable
    // would send the operator to repeat or reverse a change that took effect.
    const got = describeDisableOutcome(after({ cleanup_failed: ["schedules", "runs"] }), t)
    expect(got.incomplete).toBe(true)
    expect(got.message).toContain("gone@corp.com is disabled")
    expect(got.message).toContain("pausing schedules, canceling runs")
    expect(got.message).toContain("Retrying is safe")
  })

  it("names a step it does not know verbatim", () => {
    const got = describeDisableOutcome(after({ cleanup_failed: ["something_new"] }), t)
    expect(got.message).toContain("something_new")
  })

  it("reads in Chinese", () => {
    const zh: Translate<MessageKey> = (key, vars) => translate("zh-CN", key, vars)
    const got = describeDisableOutcome(after({ runs_canceled: 1 }), zh)
    expect(got.message).toBe("gone@corp.com 已停用。已撤销 2 个会话、已取消 1 个运行。")
  })
})
