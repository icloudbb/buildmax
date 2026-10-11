import { describe, expect, it } from "vitest"
import { inviteeLabel, raisesAuthority } from "./members"

describe("raisesAuthority", () => {
  it("is true only for a move up the role order", () => {
    expect(raisesAuthority("member", "admin")).toBe(true)
    expect(raisesAuthority("admin", "owner")).toBe(true)
    expect(raisesAuthority("admin", "member")).toBe(false)
    expect(raisesAuthority("member", "member")).toBe(false)
  })

  it("reads a member with no stored role as a member", () => {
    expect(raisesAuthority("", "admin")).toBe(true)
    expect(raisesAuthority("", "member")).toBe(false)
  })
})

describe("inviteeLabel", () => {
  it("prefers the name, then the email, then the account ID", () => {
    expect(inviteeLabel({ user_id: "u1", user_name: "Nora", user_email: "nora@example.com" })).toBe("Nora")
    expect(inviteeLabel({ user_id: "u1", user_name: "", user_email: "nora@example.com" })).toBe("nora@example.com")
    expect(inviteeLabel({ user_id: "u1" })).toBe("u1")
  })
})
