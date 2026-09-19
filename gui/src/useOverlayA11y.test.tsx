import { useRef, useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useOverlayA11y } from "./useOverlayA11y"

afterEach(cleanup)

function Harness({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  useOverlayA11y({ open, onClose, containerRef })

  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        opener
      </button>
      {open && (
        <div ref={containerRef} tabIndex={-1}>
          <button type="button">first</button>
          <button type="button">last</button>
        </div>
      )}
    </div>
  )
}

describe("useOverlayA11y", () => {
  it("moves focus into the container on open", async () => {
    render(<Harness onClose={() => {}} />)
    fireEvent.click(screen.getByText("opener"))
    expect(document.activeElement).toBe(screen.getByText("first"))
  })

  it("wraps Tab from the last focusable back to the first", async () => {
    render(<Harness onClose={() => {}} />)
    fireEvent.click(screen.getByText("opener"))
    await new Promise((resolve) => setTimeout(resolve, 0))
    screen.getByText("last").focus()
    fireEvent.keyDown(document, { key: "Tab" })
    expect(document.activeElement).toBe(screen.getByText("first"))
  })

  it("wraps Shift+Tab from the first focusable back to the last", async () => {
    render(<Harness onClose={() => {}} />)
    fireEvent.click(screen.getByText("opener"))
    await new Promise((resolve) => setTimeout(resolve, 0))
    screen.getByText("first").focus()
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true })
    expect(document.activeElement).toBe(screen.getByText("last"))
  })

  it("calls onClose on Escape", async () => {
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)
    fireEvent.click(screen.getByText("opener"))
    await new Promise((resolve) => setTimeout(resolve, 0))
    fireEvent.keyDown(document, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("keeps focus in an open overlay when its close callback changes", async () => {
    const firstClose = vi.fn()
    const latestClose = vi.fn()
    const { rerender } = render(<Harness onClose={firstClose} />)
    fireEvent.click(screen.getByText("opener"))
    await new Promise((resolve) => setTimeout(resolve, 0))
    screen.getByText("last").focus()

    rerender(<Harness onClose={latestClose} />)
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(document.activeElement).toBe(screen.getByText("last"))

    fireEvent.keyDown(document, { key: "Escape" })
    expect(latestClose).toHaveBeenCalledTimes(1)
    expect(firstClose).not.toHaveBeenCalled()
  })

  it("restores focus to the opener once open goes false", async () => {
    function Wrapper() {
      const [open, setOpen] = useState(false)
      const containerRef = useRef<HTMLDivElement>(null)
      useOverlayA11y({ open, onClose: () => setOpen(false), containerRef })
      return (
        <div>
          <button type="button" onClick={() => setOpen(true)}>
            opener
          </button>
          <button type="button" onClick={() => setOpen(false)}>
            close
          </button>
          {open && (
            <div ref={containerRef} tabIndex={-1}>
              <button type="button">first</button>
            </div>
          )}
        </div>
      )
    }
    render(<Wrapper />)
    screen.getByText("opener").focus()
    fireEvent.click(screen.getByText("opener"))
    await new Promise((resolve) => setTimeout(resolve, 0))
    fireEvent.click(screen.getByText("close"))
    expect(document.activeElement).toBe(screen.getByText("opener"))
  })
})
