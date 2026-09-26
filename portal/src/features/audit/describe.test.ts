import { describe, expect, it } from "vitest"
import type { ApiAuditEvent } from "../../lib/api/types"
import { actorLabel, describeEvent, formatEventTime } from "./describe"

function event(partial: Partial<ApiAuditEvent>): ApiAuditEvent {
  return {
    id: "ae_1",
    space_id: "tm_1",
    actor_type: "user",
    actor_id: "u_1",
    action: "user.login",
    created_at: "2026-08-16T10:10:00Z",
    ...partial,
  }
}

describe("describeEvent", () => {
  it("marks a refusal apart from the actions around it", () => {
    // A denial is what shows someone probing at a boundary. Rendering it like
    // a successful action would bury the one entry an owner is looking for.
    const got = describeEvent(event({ action: "access.denied", target_id: "manage_agents" }))
    expect(got.denied).toBe(true)
    expect(got.summary).toContain("manage_agents")
  })

  it("shows an action it does not recognise verbatim", () => {
    // Action strings are permanent and a newer server may write one this
    // Portal predates. Dropping the row, or relabelling it "unknown", hides an
    // audit entry — worse than showing a name the reader can search for.
    const got = describeEvent(event({ action: "something.added.later" }))
    expect(got.summary).toBe("something.added.later")
    expect(got.denied).toBe(false)
  })

  it("uses the detail when there is one to use", () => {
    expect(describeEvent(event({ action: "space.member_added", detail: "admin" })).summary)
      .toContain("admin")
    expect(describeEvent(event({ action: "space.member_added" })).summary)
      .toBe("Added a member")
  })

  it("reads the deployment-scoped actions the admin trail added", () => {
    // These have no space, so a space owner can never see them. They are only
    // readable in the administration area, and they should read as sentences
    // there rather than as raw action strings.
    expect(describeEvent(event({ action: "system.admin_granted", detail: "system_admin" })).summary)
      .toBe("Granted system_admin over the deployment")
    expect(describeEvent(event({ action: "system.admin_revoked", detail: "system_admin" })).summary)
      .toBe("Revoked system_admin over the deployment")
    expect(describeEvent(event({ action: "user.created" })).summary).toBe("Created an account")
    expect(describeEvent(event({ action: "user.disabled" })).summary).toBe("Disabled an account")
    expect(describeEvent(event({ action: "user.disabled", detail: "cleanup incomplete: runs" })).summary)
      .toBe("Disabled an account — cleanup incomplete: runs")
    expect(describeEvent(event({ action: "user.enabled" })).summary).toBe("Enabled an account")
    expect(describeEvent(event({ action: "user.login_code_issued" })).summary)
      .toBe("Issued a login code")
    expect(describeEvent(event({ action: "user.sessions_revoked" })).summary)
      .toBe("Revoked every session of an account")
  })

  it("marks a reused refresh token as a refusal", () => {
    // It is not a user's intent — it is the server reporting a credential in
    // two places — so it gets the treatment a denial gets, not the treatment an
    // ordinary action gets.
    expect(describeEvent(event({ action: "auth.refresh_reuse" })).denied).toBe(true)
  })

  it("gives retention the same treatment as a denial", () => {
    // It is the one action that removes evidence. A reader scanning the trail
    // must not skim past the row that explains why the trail starts where it
    // does, so it is called out rather than shown as ordinary housekeeping.
    const got = describeEvent(event({ action: "audit.pruned", detail: "12 events from A to B" }))
    expect(got.denied).toBe(true)
    expect(got.summary).toContain("12 events")
  })

  it("names an export and what left in it", () => {
    const got = describeEvent(event({ action: "audit.exported", detail: "40 events" }))
    expect(got.denied).toBe(false)
    expect(got.summary).toContain("40 events")
  })

  it("separates approaching a quota from being stopped by one", () => {
    // One is a heads-up, the other is work not happening.
    expect(describeEvent(event({ action: "quota.threshold_reached", detail: "runs 80% of 10" })).denied)
      .toBe(false)
    expect(describeEvent(event({ action: "quota.exceeded", detail: "runs limit reached at 10 of 10" })).denied)
      .toBe(true)
  })

  it("reads the invitation and role-change actions space-membership-lifecycle added", () => {
    expect(describeEvent(event({ action: "space.member_invited", detail: "admin" })).summary)
      .toContain("admin")
    expect(describeEvent(event({ action: "space.invitation_accepted", detail: "member" })).summary)
      .toContain("member")
    expect(describeEvent(event({ action: "space.invitation_revoked" })).summary)
      .toBe("Revoked a pending invitation")
    expect(describeEvent(event({ action: "space.member_role_changed", detail: "owner" })).summary)
      .toContain("owner")
    expect(describeEvent(event({ action: "space.ownership_transferred" })).summary)
      .toBe("Transferred ownership")
    expect(describeEvent(event({ action: "space.member_login_code_issued" })).summary)
      .toBe("Issued a login code for a member")
  })

  it("treats an accept found past its expiry as a denial, the same as access.denied", () => {
    // An invitation names a specific, already-resolved account before anyone
    // acts on it, so its outcome is worth calling out either way -- unlike a
    // failed login, which says nothing about who the actor was.
    expect(describeEvent(event({ action: "space.invitation_expired" })).denied).toBe(true)
  })

  it("does not show a target for events whose target is already in the summary", () => {
    // A login's target is the platform, which the sentence already names.
    expect(describeEvent(event({ action: "user.login", target_id: "cli" })).target).toBeNull()
    expect(describeEvent(event({ action: "space.member_removed", target_id: "u_2" })).target).toBe("u_2")
  })
})

describe("actorLabel", () => {
  it("separates a person from the deployment itself", () => {
    // Model catalog changes are recorded as the system, because they run from a
    // shell on the server that no user id was verified for.
    expect(actorLabel(event({ actor_type: "system", actor_id: "buildmax-server" })))
      .toBe("buildmax-server (system)")
    expect(actorLabel(event({ actor_type: "worker", actor_id: "r_1" }))).toBe("r_1 (worker)")
  })

  it("names the reader as themselves", () => {
    expect(actorLabel(event({ actor_id: "u_1" }), "u_1")).toBe("You")
    expect(actorLabel(event({ actor_id: "u_2" }), "u_1")).toBe("u_2")
  })
})

describe("formatEventTime", () => {
  it("shows a dash rather than the epoch when there is no time", () => {
    expect(formatEventTime("")).toBe("—")
  })
})
