import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { Button, ButtonLink, IconButton } from "./Button"

describe("Button", () => {
  it("prevents repeated submission while busy and keeps the action named", () => {
    const submit = vi.fn()
    const { rerender } = render(<Button variant="primary" onClick={submit}>Create issue</Button>)
    fireEvent.click(screen.getByRole("button", { name: "Create issue" }))
    expect(submit).toHaveBeenCalledTimes(1)

    rerender(<Button variant="primary" busy onClick={submit}>Create issue</Button>)
    const button = screen.getByRole("button", { name: "Create issue" })
    expect(button.hasAttribute("disabled")).toBe(true)
    expect(button.getAttribute("aria-busy")).toBe("true")
    fireEvent.click(button)
    expect(submit).toHaveBeenCalledTimes(1)
  })

  it("requires a name for an icon-only action", () => {
    render(<IconButton aria-label="Close dialog">×</IconButton>)
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeTruthy()
  })

  it("keeps navigation as a link with the same visual role", () => {
    render(<ButtonLink href="#/spaces/test/issues" variant="tertiary">Back to issues</ButtonLink>)
    const link = screen.getByRole("link", { name: "Back to issues" })
    expect(link.getAttribute("href")).toBe("#/spaces/test/issues")
    expect(link.className).toContain("bm-button--tertiary")
  })
})
