import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ThemeProvider, useTheme } from "./ThemeContext"

const THEME_KEY = "buildmax_theme"

/** A stand-in for the OS color-scheme preference, which jsdom does not have. */
function emulateColorScheme(initial: "light" | "dark") {
  let dark = initial === "dark"
  const listeners = new Set<() => void>()
  const query = {
    get matches() {
      return dark
    },
    media: "(prefers-color-scheme: dark)",
    addEventListener: (_: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
  }
  vi.stubGlobal("matchMedia", (media: string) => {
    expect(media).toBe("(prefers-color-scheme: dark)")
    return query
  })
  return {
    set(next: "light" | "dark") {
      dark = next === "dark"
      act(() => listeners.forEach((listener) => listener()))
    },
  }
}

function Probe() {
  const { theme, toggleTheme } = useTheme()
  return (
    <button type="button" onClick={toggleTheme}>
      {theme}
    </button>
  )
}

function renderProbe() {
  render(
    <ThemeProvider>
      <Probe />
    </ThemeProvider>,
  )
  return screen.getByRole("button")
}

beforeEach(() => {
  localStorage.removeItem(THEME_KEY)
  document.documentElement.removeAttribute("data-theme")
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  localStorage.removeItem(THEME_KEY)
})

describe("ThemeProvider", () => {
  it("follows the OS preference when nothing is stored, without storing it", () => {
    emulateColorScheme("dark")
    expect(renderProbe().textContent).toBe("dark")
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark")
    expect(localStorage.getItem(THEME_KEY)).toBeNull()
  })

  it("keeps following the OS while the person has not chosen", () => {
    const os = emulateColorScheme("light")
    const probe = renderProbe()
    expect(probe.textContent).toBe("light")
    os.set("dark")
    expect(probe.textContent).toBe("dark")
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark")
  })

  it("lets a stored choice win over the OS preference", () => {
    localStorage.setItem(THEME_KEY, "light")
    const os = emulateColorScheme("dark")
    const probe = renderProbe()
    expect(probe.textContent).toBe("light")
    os.set("light")
    os.set("dark")
    expect(probe.textContent).toBe("light")
  })

  it("stores a choice made with the toggle, and the OS no longer moves it", () => {
    const os = emulateColorScheme("dark")
    const probe = renderProbe()
    fireEvent.click(probe)
    expect(probe.textContent).toBe("light")
    expect(localStorage.getItem(THEME_KEY)).toBe("light")
    os.set("light")
    os.set("dark")
    expect(probe.textContent).toBe("light")
  })

  it("starts light where the OS preference cannot be read", () => {
    vi.stubGlobal("matchMedia", undefined)
    expect(renderProbe().textContent).toBe("light")
  })
})
