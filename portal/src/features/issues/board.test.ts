import { describe, expect, it } from "vitest"
import type { Issue } from "../../lib/types"
import { appendPage, collectionFilter, LANE_PAGE_SIZE, LANE_RELOAD_MAX, reloadLimit } from "./board"

function issue(id: string): Issue {
  return {
    id,
    userId: "u_1",
    title: id,
    description: "",
    status: "todo",
    createdBy: "u_1",
    createdAt: "",
    updatedAt: "",
    updatedLabel: "",
    version: 1,
    childCount: 0,
    doneChildCount: 0,
    commentCount: 0,
  }
}

describe("collectionFilter", () => {
  it("always lists top-level Issues only", () => {
    expect(collectionFilter({})).toEqual({ parentId: "none" })
  })

  it("maps the URL's owner and executor onto the server's filters", () => {
    expect(collectionFilter({ owner: "me", executor: "workflow:w_1" })).toEqual({
      parentId: "none",
      owner: "me",
      executorKind: "workflow",
      executorId: "w_1",
    })
  })

  it("ignores an executor it cannot split into a kind and an id", () => {
    expect(collectionFilter({ executor: "person:u_1" })).toEqual({ parentId: "none" })
    expect(collectionFilter({ executor: "agent" })).toEqual({ parentId: "none" })
  })
})

describe("appendPage", () => {
  it("skips an Issue already shown, so a shifted offset never duplicates a card", () => {
    const merged = appendPage([issue("a"), issue("b")], [issue("b"), issue("c")])
    expect(merged.map((item) => item.id)).toEqual(["a", "b", "c"])
  })
})

describe("reloadLimit", () => {
  it("keeps what the reader expanded, within one page and the server's ceiling", () => {
    expect(reloadLimit(0)).toBe(LANE_PAGE_SIZE)
    expect(reloadLimit(LANE_PAGE_SIZE * 2)).toBe(LANE_PAGE_SIZE * 2)
    expect(reloadLimit(LANE_RELOAD_MAX + 40)).toBe(LANE_RELOAD_MAX)
  })
})
