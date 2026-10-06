import { useRef, type ReactNode } from "react"
import { useOverlayA11y } from "./useOverlayA11y"
import { useGuiT } from "./messages"

export interface DrawerProps {
  id?: string
  open: boolean
  title: string
  titleId: string
  onClose: () => void
  children: ReactNode
}

/** An accessible, left-edge slide-in overlay for narrow-width navigation. */
export function Drawer({ id, open, title, titleId, onClose, children }: DrawerProps) {
  const t = useGuiT()
  const containerRef = useRef<HTMLDivElement>(null)

  useOverlayA11y({ open, onClose, containerRef })

  if (!open) return null

  return (
    // Presentational backdrop, as in BaseModal: Escape is the keyboard close,
    // and clicks inside the drawer stop here.
    <div
      className="drawer-overlay"
      role="presentation"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
        else e.stopPropagation()
      }}
    >
      <div
        id={id}
        ref={containerRef}
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <div className="drawer__header">
          <h2 className="drawer__title" id={titleId}>
            {title}
          </h2>
          <button type="button" className="drawer__close" onClick={onClose} aria-label={t("gui.close")}>
            &times;
          </button>
        </div>
        <div className="drawer__body">{children}</div>
      </div>
    </div>
  )
}
