import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { LocaleProvider } from "./i18n"
import { Skeleton, Spinner } from "./Spinner"

afterEach(() => {
  cleanup()
  localStorage.clear()
})

describe("Spinner", () => {
  it("is a status that says what is loading", () => {
    render(<Spinner label="Loading members…" />)
    expect(screen.getByRole("status").textContent).toBe("Loading members…")
  })

  it("says Loading in the interface language by default", () => {
    localStorage.setItem("buildmax_locale", "zh-CN")
    render(
      <LocaleProvider>
        <Spinner />
      </LocaleProvider>,
    )
    expect(screen.getByRole("status").textContent).toBe("加载中…")
  })
})

describe("Skeleton", () => {
  it("names itself for assistive technology and hides its placeholder rows", () => {
    render(<Skeleton lines={4} />)
    const status = screen.getByRole("status")
    expect(status.textContent).toBe("Loading…")
    const rows = status.querySelectorAll(".bm-skeleton__line")
    expect(rows).toHaveLength(4)
    rows.forEach((row) => expect(row.getAttribute("aria-hidden")).toBe("true"))
  })
})
