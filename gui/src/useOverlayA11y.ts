import { useLayoutEffect, useRef, type RefObject } from "react"
import { lockBodyScroll, unlockBodyScroll } from "./bodyScrollLock"

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

function focusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
}

export interface UseOverlayA11yOptions {
  open: boolean
  onClose: () => void
  containerRef: RefObject<HTMLElement | null>
}

/**
 * Shared dialog/drawer behavior: traps Tab focus inside the container, moves
 * focus in on open and restores it to the opener on close, locks body scroll
 * while open, and closes on Escape. Used by BaseModal and Drawer so both get
 * the same accessible overlay contract from one implementation.
 */
export function useOverlayA11y({ open, onClose, containerRef }: UseOverlayA11yOptions) {
  const onCloseRef = useRef(onClose)
  useLayoutEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  useLayoutEffect(() => {
    if (!open) return

    const opener = document.activeElement as HTMLElement | null
    lockBodyScroll()

    // The effect runs after the dialog has committed to the DOM. Focus now so
    // a delayed opener focus cannot steal focus from the user's first keypress.
    const container = containerRef.current
    if (container) {
      const [first] = focusableElements(container)
      if (first) {
        first.focus()
      } else {
        container.focus()
      }
    }

    function handleKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        onCloseRef.current()
        return
      }
      if (e.key !== "Tab") return
      const container = containerRef.current
      if (!container) return
      const focusable = focusableElements(container)
      if (focusable.length === 0) {
        e.preventDefault()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement
      if (e.shiftKey) {
        if (active === first || !container.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (active === last || !container.contains(active)) {
          e.preventDefault()
          first.focus()
        }
      }
    }

    document.addEventListener("keydown", handleKey)

    return () => {
      document.removeEventListener("keydown", handleKey)
      unlockBodyScroll()
      opener?.focus?.()
    }
  }, [open, containerRef])
}
