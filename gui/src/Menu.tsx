import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
} from "react"

export interface MenuTriggerProps {
  ref: RefObject<HTMLButtonElement | null>
  id: string
  "aria-haspopup": "menu"
  "aria-expanded": boolean
  "aria-controls": string | undefined
  onClick: () => void
  onKeyDown: (e: KeyboardEvent<HTMLButtonElement>) => void
}

export interface MenuProps {
  /** Renders the button that opens the menu; spread the props onto a `<button>`. */
  trigger: (props: MenuTriggerProps) => ReactNode
  /** The menu's accessible name; defaults to the trigger's. */
  label?: string
  /** Opens above the trigger, for a trigger at the bottom of the screen. */
  placement?: "below" | "above"
  /** Extra class on the menu panel, for placement only. */
  className?: string
  /** Extra class on the element that anchors the panel. */
  anchorClassName?: string
  children: ReactNode
}

interface MenuContextValue {
  select: (action: () => void) => void
}

const MenuContext = createContext<MenuContextValue | null>(null)

const ITEM_SELECTOR = '[role="menuitem"]:not(:disabled), [role="menuitemradio"]:not(:disabled), [role="menuitemcheckbox"]:not(:disabled)'

/**
 * A menu button: arrow keys move between items, Home and End jump to the ends,
 * Escape closes and returns focus to the trigger, and Tab or a click outside
 * closes it. Choosing an item returns focus to the trigger before the item's
 * action runs, so an action that moves focus elsewhere still wins.
 */
export function Menu({ trigger, label, placement = "below", className, anchorClassName, children }: MenuProps) {
  const [open, setOpen] = useState(false)
  const [initialFocus, setInitialFocus] = useState<"first" | "last">("first")
  const anchorRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const triggerId = useId()
  const menuId = useId()

  const items = useCallback(
    () => Array.from(menuRef.current?.querySelectorAll<HTMLElement>(ITEM_SELECTOR) ?? []),
    [],
  )

  useLayoutEffect(() => {
    if (!open) return
    const list = items()
    const target = initialFocus === "last" ? list[list.length - 1] : list[0]
    ;(target ?? menuRef.current)?.focus()
  }, [open, initialFocus, items])

  useEffect(() => {
    if (!open) return
    function handlePointerDown(e: PointerEvent) {
      if (anchorRef.current && !anchorRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("pointerdown", handlePointerDown)
    return () => document.removeEventListener("pointerdown", handlePointerDown)
  }, [open])

  function openAt(focus: "first" | "last") {
    setInitialFocus(focus)
    setOpen(true)
  }

  function closeToTrigger() {
    triggerRef.current?.focus()
    setOpen(false)
  }

  function handleTriggerKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault()
      openAt(e.key === "ArrowUp" ? "last" : "first")
    }
  }

  function handleMenuKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const list = items()
    const index = list.indexOf(document.activeElement as HTMLElement)
    let next: HTMLElement | undefined
    switch (e.key) {
      case "ArrowDown":
        next = list[(index + 1) % list.length]
        break
      case "ArrowUp":
        next = list[(index - 1 + list.length) % list.length]
        break
      case "Home":
        next = list[0]
        break
      case "End":
        next = list[list.length - 1]
        break
      case "Escape":
        // Only this menu closes: an enclosing dialog or drawer stays open.
        e.preventDefault()
        e.stopPropagation()
        closeToTrigger()
        return
      case "Tab":
        // From the trigger, so the browser's own Tab lands next to it rather
        // than at the top of the document once the menu unmounts.
        closeToTrigger()
        return
      default:
        return
    }
    e.preventDefault()
    next?.focus()
  }

  const context: MenuContextValue = {
    select(action) {
      closeToTrigger()
      action()
    },
  }

  return (
    <div ref={anchorRef} className={["bm-menu-anchor", anchorClassName].filter(Boolean).join(" ")}>
      {trigger({
        ref: triggerRef,
        id: triggerId,
        "aria-haspopup": "menu",
        "aria-expanded": open,
        "aria-controls": open ? menuId : undefined,
        onClick: () => (open ? setOpen(false) : openAt("first")),
        onKeyDown: handleTriggerKeyDown,
      })}
      {open ? (
        <div
          ref={menuRef}
          id={menuId}
          role="menu"
          tabIndex={-1}
          aria-label={label}
          aria-labelledby={label ? undefined : triggerId}
          className={["bm-menu", `bm-menu--${placement}`, className].filter(Boolean).join(" ")}
          onKeyDown={handleMenuKeyDown}
        >
          <MenuContext.Provider value={context}>{children}</MenuContext.Provider>
        </div>
      ) : null}
    </div>
  )
}

function useMenu(): MenuContextValue {
  const menu = useContext(MenuContext)
  if (!menu) throw new Error("menu items belong inside a Menu")
  return menu
}

export interface MenuItemProps {
  onSelect: () => void
  /** A decorative icon before the label. */
  icon?: ReactNode
  disabled?: boolean
  children: ReactNode
}

export function MenuItem({ onSelect, icon, disabled, children }: MenuItemProps) {
  const menu = useMenu()
  return (
    <button
      type="button"
      role="menuitem"
      tabIndex={-1}
      className="bm-menu__item"
      disabled={disabled}
      onClick={() => menu.select(onSelect)}
    >
      <span className="bm-menu__icon" aria-hidden="true">
        {icon}
      </span>
      {children}
    </button>
  )
}

export interface MenuItemRadioProps {
  checked: boolean
  onSelect: () => void
  /** The language the label is written in, when it differs from the page's. */
  lang?: string
  children: ReactNode
}

export function MenuItemRadio({ checked, onSelect, lang, children }: MenuItemRadioProps) {
  const menu = useMenu()
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={checked}
      tabIndex={-1}
      lang={lang}
      className="bm-menu__item"
      onClick={() => menu.select(onSelect)}
    >
      <span className="bm-menu__icon" aria-hidden="true">
        {checked ? "✓" : ""}
      </span>
      {children}
    </button>
  )
}

/** Items that belong together, under a visible name. */
export function MenuGroup({ label, children }: { label: string; children: ReactNode }) {
  const labelId = useId()
  return (
    <div role="group" aria-labelledby={labelId} className="bm-menu__group">
      <span id={labelId} className="bm-menu__group-label">
        {label}
      </span>
      {children}
    </div>
  )
}

export function MenuSeparator() {
  return <div role="separator" className="bm-menu__separator" />
}

/** Non-interactive context at the top of a menu, such as who is signed in. */
export function MenuSection({ children }: { children: ReactNode }) {
  return (
    <div role="none" className="bm-menu__section">
      {children}
    </div>
  )
}
