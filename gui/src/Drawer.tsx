import { useRef, type ReactNode } from "react"
import { useOverlayA11y } from "./useOverlayA11y"
import { useGuiT } from "./messages"

export interface DrawerProps {
  id?: string
  open: boolean
  title: string
  titleId: string
  onClose: () => void
  /** The edge it slides in from: "start" (left) for navigation, "end" (right) for a side panel. */
  side?: "start" | "end"
  /** Optional class name(s) on the drawer panel, e.g. to set a wider width. */
  className?: string
  children: ReactNode
}

/** An accessible slide-in overlay: navigation from the start edge, a side panel from the end. */
export function Drawer({ id, open, title, titleId, onClose, side = "start", className, children }: DrawerProps) {
  const t = useGuiT()
  const containerRef = useRef<HTMLDivElement>(null)

  useOverlayA11y({ open, onClose, containerRef })

  if (!open) return null

  return (
    // Presentational backdrop, as in BaseModal: Escape is the keyboard close,
    // and clicks inside the drawer stop here.
    <div
      className={side === "end" ? "drawer-overlay drawer-overlay--end" : "drawer-overlay"}
      role="presentation"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
        else e.stopPropagation()
      }}
    >
      <div
        id={id}
        ref={containerRef}
        className={["drawer", side === "end" ? "drawer--end" : "", className].filter(Boolean).join(" ")}
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
