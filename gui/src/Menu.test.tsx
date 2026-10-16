import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { Menu, MenuGroup, MenuItem, MenuItemRadio, MenuSection, MenuSeparator } from "./Menu"

afterEach(cleanup)

interface HarnessProps {
  onAccount?: () => void
  onSignOut?: () => void
  onLanguage?: (locale: string) => void
}

function Harness({ onAccount = () => {}, onSignOut = () => {}, onLanguage = () => {} }: HarnessProps) {
  return (
    <div>
      <button type="button">before</button>
      <Menu
        trigger={(props) => (
          <button type="button" aria-label="User menu" {...props}>
            Ada
          </button>
        )}
      >
        <MenuSection>ada@example.test</MenuSection>
        <MenuItem onSelect={onAccount}>Account</MenuItem>
        <MenuItem onSelect={() => {}} disabled>
          Billing
        </MenuItem>
        <MenuSeparator />
        <MenuGroup label="Language">
          <MenuItemRadio checked onSelect={() => onLanguage("en")}>
            English
          </MenuItemRadio>
          <MenuItemRadio checked={false} lang="zh-CN" onSelect={() => onLanguage("zh-CN")}>
            简体中文
          </MenuItemRadio>
        </MenuGroup>
        <MenuItem onSelect={onSignOut}>Sign out</MenuItem>
      </Menu>
      <button type="button">after</button>
    </div>
  )
}

function trigger() {
  return screen.getByRole("button", { name: "User menu" })
}

describe("Menu", () => {
  it("opens from its button, names itself after it, and focuses the first item", () => {
    render(<Harness />)
    expect(trigger().getAttribute("aria-haspopup")).toBe("menu")
    expect(trigger().getAttribute("aria-expanded")).toBe("false")

    fireEvent.click(trigger())
    const menu = screen.getByRole("menu", { name: "User menu" })
    expect(trigger().getAttribute("aria-expanded")).toBe("true")
    expect(trigger().getAttribute("aria-controls")).toBe(menu.id)
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Account" }))
  })

  it("moves with the arrow keys, wraps, skips disabled items, and jumps with Home and End", () => {
    render(<Harness />)
    fireEvent.click(trigger())
    const menu = screen.getByRole("menu")
    const focused = () => document.activeElement?.textContent?.replace("✓", "")

    fireEvent.keyDown(menu, { key: "ArrowDown" })
    expect(focused()).toBe("English")
    fireEvent.keyDown(menu, { key: "End" })
    expect(focused()).toBe("Sign out")
    fireEvent.keyDown(menu, { key: "ArrowDown" })
    expect(focused()).toBe("Account")
    fireEvent.keyDown(menu, { key: "ArrowUp" })
    expect(focused()).toBe("Sign out")
    fireEvent.keyDown(menu, { key: "Home" })
    expect(focused()).toBe("Account")
  })

  it("opens on the last item with ArrowUp on the button", () => {
    render(<Harness />)
    fireEvent.keyDown(trigger(), { key: "ArrowUp" })
    expect(document.activeElement?.textContent).toBe("Sign out")
  })

  it("closes on Escape, returns focus to its button, and keeps the key from an enclosing dialog", () => {
    const documentEscape = vi.fn()
    document.addEventListener("keydown", documentEscape)
    try {
      render(<Harness />)
      fireEvent.click(trigger())
      fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" })
      expect(screen.queryByRole("menu")).toBeNull()
      expect(document.activeElement).toBe(trigger())
      expect(documentEscape).not.toHaveBeenCalled()
    } finally {
      document.removeEventListener("keydown", documentEscape)
    }
  })

  it("runs the chosen item after returning focus to its button", () => {
    let focusedDuringAction: Element | null = null
    const onAccount = vi.fn(() => {
      focusedDuringAction = document.activeElement
    })
    render(<Harness onAccount={onAccount} />)
    fireEvent.click(trigger())
    fireEvent.click(screen.getByRole("menuitem", { name: "Account" }))
    expect(onAccount).toHaveBeenCalledTimes(1)
    expect(focusedDuringAction).toBe(trigger())
    expect(screen.queryByRole("menu")).toBeNull()
  })

  it("reports the checked choice and its language", () => {
    const onLanguage = vi.fn()
    render(<Harness onLanguage={onLanguage} />)
    fireEvent.click(trigger())
    expect(screen.getByRole("group", { name: "Language" })).toBeTruthy()
    expect(screen.getByRole("menuitemradio", { name: "English" }).getAttribute("aria-checked")).toBe("true")
    const chinese = screen.getByRole("menuitemradio", { name: "简体中文" })
    expect(chinese.getAttribute("aria-checked")).toBe("false")
    expect(chinese.getAttribute("lang")).toBe("zh-CN")
    fireEvent.click(chinese)
    expect(onLanguage).toHaveBeenCalledWith("zh-CN")
  })

  it("closes on Tab from its button, so focus continues past it", () => {
    render(<Harness />)
    fireEvent.click(trigger())
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Tab" })
    expect(screen.queryByRole("menu")).toBeNull()
    expect(document.activeElement).toBe(trigger())
  })

  it("closes on a pointer press outside without moving focus there", () => {
    render(<Harness />)
    fireEvent.click(trigger())
    fireEvent.pointerDown(screen.getByRole("button", { name: "after" }))
    expect(screen.queryByRole("menu")).toBeNull()
  })

  it("stays open for a pointer press inside", () => {
    render(<Harness />)
    fireEvent.click(trigger())
    fireEvent.pointerDown(screen.getByText("ada@example.test"))
    expect(screen.getByRole("menu")).toBeTruthy()
  })

  it("toggles closed from its button", () => {
    render(<Harness />)
    fireEvent.click(trigger())
    fireEvent.click(trigger())
    expect(screen.queryByRole("menu")).toBeNull()
  })
})
