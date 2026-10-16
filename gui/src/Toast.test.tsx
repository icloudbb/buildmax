import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ToastProvider, useToast } from "./Toast"

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function Saver({ message = "Saved the Issue." }: { message?: string }) {
  const toast = useToast()
  return (
    <button type="button" onClick={() => toast(message)}>
      save
    </button>
  )
}

function region() {
  return screen.getByRole("status", { name: "Notifications" })
}

describe("Toast", () => {
  it("announces through a live region that exists before the first toast", () => {
    render(
      <ToastProvider>
        <Saver />
      </ToastProvider>,
    )
    expect(region().childElementCount).toBe(0)
    fireEvent.click(screen.getByRole("button", { name: "save" }))
    expect(region().textContent).toContain("Saved the Issue.")
  })

  it("leaves after its duration", () => {
    render(
      <ToastProvider duration={1000}>
        <Saver />
      </ToastProvider>,
    )
    fireEvent.click(screen.getByRole("button", { name: "save" }))
    act(() => {
      vi.advanceTimersByTime(999)
    })
    expect(screen.queryByText("Saved the Issue.")).not.toBeNull()
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(screen.queryByText("Saved the Issue.")).toBeNull()
  })

  it("waits while the pointer or focus is on it", () => {
    render(
      <ToastProvider duration={1000}>
        <Saver />
      </ToastProvider>,
    )
    fireEvent.click(screen.getByRole("button", { name: "save" }))
    fireEvent.mouseEnter(region())
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(screen.queryByText("Saved the Issue.")).not.toBeNull()

    fireEvent.mouseLeave(region())
    fireEvent.focus(screen.getByRole("button", { name: "Dismiss" }))
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(screen.queryByText("Saved the Issue.")).not.toBeNull()
  })

  it("can be dismissed by its named button", () => {
    render(
      <ToastProvider>
        <Saver />
      </ToastProvider>,
    )
    fireEvent.click(screen.getByRole("button", { name: "save" }))
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByText("Saved the Issue.")).toBeNull()
  })

  it("keeps only the latest few", () => {
    render(
      <ToastProvider>
        <Saver message="one" />
      </ToastProvider>,
    )
    for (let i = 0; i < 5; i++) fireEvent.click(screen.getByRole("button", { name: "save" }))
    expect(screen.getAllByText("one")).toHaveLength(3)
  })

  it("refuses to run without a provider", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {})
    expect(() => render(<Saver />)).toThrow("useToast needs a ToastProvider")
    error.mockRestore()
  })
})
