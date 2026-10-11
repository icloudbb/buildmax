import { describe, expect, it } from "vitest"
import { currentMembership, planSpaceSelection, type MembershipLookup } from "./spaceSelection"

describe("planSpaceSelection", () => {
  it("selects a Space the list already holds", () => {
    expect(planSpaceSelection(["s1", "s2"], "s2", null)).toBe("select")
  })

  it("lists again for a Space joined after the list loaded", () => {
    // An accepted invitation, or a link to a Space created elsewhere: the
    // list was read before it existed, so a reload must not be the only way
    // to reach it.
    expect(planSpaceSelection(["s1"], "joined", null)).toBe("relist")
  })

  it("does not list again for a Space that a fresh list still lacks", () => {
    // The caller cannot reach it; asking again on every render would loop.
    expect(planSpaceSelection(["s1"], "elsewhere", "elsewhere")).toBe("ignore")
    expect(planSpaceSelection(["s1"], "other", "elsewhere")).toBe("relist")
  })

  it("leaves the decision to the first load while no list has loaded", () => {
    expect(planSpaceSelection(null, "s1", null)).toBe("ignore")
  })
})

describe("currentMembership", () => {
  const owner = { user_id: "u1", role: "owner" }
  const lookup = (spaceId: string, over: Partial<MembershipLookup<typeof owner>> = {}): MembershipLookup<typeof owner> => ({
    spaceId,
    members: [owner],
    loading: false,
    error: null,
    ...over,
  })

  it("reads the roster fetched for the current Space", () => {
    expect(currentMembership(lookup("s1"), "s1")).toEqual({ members: [owner], loading: false, error: null })
  })

  it("never lends one Space's roster to another", () => {
    // Right after a switch, the previous Space's roster is all there is. It
    // must read as unresolved, or the old role would gate the new Space.
    expect(currentMembership(lookup("previous"), "joined")).toEqual({ members: [], loading: true, error: null })
    expect(currentMembership(null, "joined")).toEqual({ members: [], loading: true, error: null })
  })

  it("keeps a failed lookup distinct from an empty roster", () => {
    const error = { kind: "error" as const, message: "boom" }
    expect(currentMembership(lookup("s1", { members: null, error }), "s1")).toEqual({ members: [], loading: false, error })
  })

  it("is empty and settled with no current Space", () => {
    expect(currentMembership(lookup("s1"), null)).toEqual({ members: [], loading: false, error: null })
  })
})
