import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { Drawer } from "./Drawer"

afterEach(cleanup)

function Harness({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        open drawer
      </button>
      <Drawer
        open={open}
        title="Navigation"
        titleId="nav-drawer-title"
        onClose={() => {
          setOpen(false)
          onClose()
        }}
      >
        <button type="button">nav item</button>
      </Drawer>
    </div>
  )
}

describe("Drawer", () => {
  it("renders nothing when closed", () => {
    render(<Harness onClose={() => {}} />)
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("opens as a labeled dialog containing its children", async () => {
    render(<Harness onClose={() => {}} />)
    fireEvent.click(screen.getByText("open drawer"))
    const dialog = await screen.findByRole("dialog")
    expect(dialog.getAttribute("aria-labelledby")).toBe("nav-drawer-title")
    expect(screen.getByText("nav item")).toBeTruthy()
  })

  it("closes on Escape", async () => {
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)
    fireEvent.click(screen.getByText("open drawer"))
    await screen.findByRole("dialog")
    fireEvent.keyDown(document, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("closes when the backdrop is clicked", async () => {
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)
    fireEvent.click(screen.getByText("open drawer"))
    const dialog = await screen.findByRole("dialog")
    // The overlay is the dialog's parent; clicking it (not the dialog itself)
    // should close, while clicking inside the dialog must not.
    fireEvent.click(dialog.parentElement as HTMLElement)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("does not close when content inside the drawer is clicked", async () => {
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)
    fireEvent.click(screen.getByText("open drawer"))
    await screen.findByRole("dialog")
    fireEvent.click(screen.getByText("nav item"))
    expect(onClose).not.toHaveBeenCalled()
  })

  it("slides in from the end edge for a side panel and keeps its class", () => {
    render(
      <Drawer open title="Jobs" titleId="jobs-title" onClose={() => {}} side="end" className="jobs-panel">
        <p>job</p>
      </Drawer>
    )
    const dialog = screen.getByRole("dialog", { name: "Jobs" })
    expect(dialog.className).toBe("drawer drawer--end jobs-panel")
    expect((dialog.parentElement as HTMLElement).className).toBe("drawer-overlay drawer-overlay--end")
  })
})
