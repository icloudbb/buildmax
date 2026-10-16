import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { Button } from "./Button"
import { CollectionFrame, DetailHeader, EditorActions, PageHeader } from "./Page"
import { StatusLabel } from "./status"

afterEach(cleanup)

describe("PageHeader", () => {
  it("is the page's one heading, with its actions", () => {
    render(<PageHeader title="Issues" subtitle="Work in this Space" actions={<Button variant="primary">New Issue</Button>} />)
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1)
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Issues")
    expect(screen.getByText("Work in this Space")).toBeTruthy()
    expect(screen.getByRole("button", { name: "New Issue" })).toBeTruthy()
  })
})

describe("DetailHeader", () => {
  it("reads the object's name, then its status, then who owns it", () => {
    render(<DetailHeader title="Fix the build" status={<StatusLabel status="in_progress" />} meta="Owner: Ada" />)
    const heading = screen.getByRole("heading", { level: 1 })
    expect(heading.textContent).toBe("Fix the build")
    const meta = heading.nextElementSibling
    expect(meta?.textContent).toBe("In progressOwner: Ada")
  })
})

describe("CollectionFrame", () => {
  it("claims no count, rows, or pages while loading", () => {
    render(
      <CollectionFrame label="Issues list" loading toolbar={<span>0 issues</span>} footer={<span>Page 1 of 1</span>}>
        <ul>
          <li>row</li>
        </ul>
      </CollectionFrame>,
    )
    const frame = screen.getByRole("region", { name: "Issues list" })
    expect(frame.getAttribute("aria-busy")).toBe("true")
    expect(screen.getByRole("status").textContent).toBe("Loading…")
    expect(screen.queryByText("0 issues")).toBeNull()
    expect(screen.queryByText("row")).toBeNull()
    expect(screen.queryByText("Page 1 of 1")).toBeNull()
  })

  it("shows the empty state instead of rows and pages", () => {
    render(
      <CollectionFrame label="Issues list" loading={false} empty={<p>No issues yet.</p>} footer={<span>Page 1 of 1</span>}>
        <ul>
          <li>row</li>
        </ul>
      </CollectionFrame>,
    )
    expect(screen.getByText("No issues yet.")).toBeTruthy()
    expect(screen.queryByText("row")).toBeNull()
    expect(screen.queryByText("Page 1 of 1")).toBeNull()
  })

  it("shows the count, rows, and pages once loaded", () => {
    render(
      <CollectionFrame label="Issues list" loading={false} toolbar={<span>1 issue</span>} footer={<span>Page 1 of 1</span>}>
        <ul>
          <li>row</li>
        </ul>
      </CollectionFrame>,
    )
    expect(screen.getByRole("region", { name: "Issues list" }).getAttribute("aria-busy")).toBeNull()
    expect(screen.getByText("1 issue")).toBeTruthy()
    expect(screen.getByText("row")).toBeTruthy()
    expect(screen.getByText("Page 1 of 1")).toBeTruthy()
  })
})

describe("EditorActions", () => {
  it("keeps Cancel and Save together in reading order", () => {
    render(
      <EditorActions>
        <Button>Cancel</Button>
        <Button variant="primary">Save changes</Button>
      </EditorActions>,
    )
    expect(screen.getAllByRole("button").map((button) => button.textContent)).toEqual(["Cancel", "Save changes"])
  })
})
