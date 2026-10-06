import { useRef, type ReactNode } from "react"
import { useOverlayA11y } from "./useOverlayA11y"
import { useGuiT } from "./messages"

export interface BaseModalProps {
  open: boolean
  title: string
  titleId: string
  onClose: () => void
  /** Optional class name(s) applied to the modal container (e.g. modal--large). */
  className?: string
  /** When true, do not render the header (title + close). Caller renders close elsewhere. */
  hideHeader?: boolean
  children: ReactNode
}

export function BaseModal({
  open,
  title,
  titleId,
  onClose,
  className,
  hideHeader,
  children,
}: BaseModalProps) {
  const t = useGuiT()
  const focusRef = useRef<HTMLDivElement>(null)

  useOverlayA11y({ open, onClose, containerRef: focusRef })

  if (!open) return null

  return (
    // The backdrop is a pointer shortcut for closing; keyboard users close with
    // Escape (useOverlayA11y), so it is presentational, not a control. Clicks
    // inside the dialog stop here so they never reach a clickable ancestor.
    <div
      className="modal-overlay"
      role="presentation"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
        else e.stopPropagation()
      }}
    >
      <div
        ref={focusRef}
        className={className ? `modal ${className}` : "modal"}
        role="dialog"
        aria-modal="true"
        aria-labelledby={hideHeader ? undefined : titleId}
        aria-label={hideHeader ? title : undefined}
      >
        {!hideHeader && (
          <div className="modal__header">
            <h2 className="modal__title" id={titleId}>
              {title}
            </h2>
            <button
              type="button"
              className="modal__close"
              onClick={onClose}
              aria-label={t("gui.close")}
            >
              &times;
            </button>
          </div>
        )}
        <div className="modal__scroll-body">{children}</div>
      </div>
    </div>
  )
}
