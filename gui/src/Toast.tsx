import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react"
import { useGuiT } from "./messages"

interface ToastItem {
  id: number
  message: string
}

type ShowToast = (message: string) => void

const ToastContext = createContext<ShowToast | null>(null)

// A person reading or pointing at a toast is still using it; it waits.
const DEFAULT_DURATION_MS = 6000
// Older confirmations give way rather than stacking up the screen.
const MAX_TOASTS = 3

export interface ToastProviderProps {
  children: ReactNode
  /** How long a toast stays when nobody is hovering or focused in it. */
  duration?: number
}

/**
 * Transient confirmation of an effect the page already shows. It is never the
 * only evidence of a result: failures and anything a person must act on stay
 * inline, attached to their object.
 */
export function ToastProvider({ children, duration = DEFAULT_DURATION_MS }: ToastProviderProps) {
  const t = useGuiT()
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const [paused, setPaused] = useState(false)
  const nextId = useRef(0)

  const show = useCallback<ShowToast>((message) => {
    nextId.current += 1
    const id = nextId.current
    setToasts((prev) => [...prev, { id, message }].slice(-MAX_TOASTS))
  }, [])

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id))
  }, [])

  return (
    <ToastContext.Provider value={show}>
      {children}
      {/* The live region exists before any toast does, so the first one is announced. */}
      <div
        className="bm-toast-region"
        role="status"
        aria-label={t("gui.toast.region")}
        onMouseEnter={() => setPaused(true)}
        onMouseLeave={() => setPaused(false)}
        onFocus={() => setPaused(true)}
        onBlur={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setPaused(false)
        }}
      >
        {toasts.map((toast) => (
          <Toast key={toast.id} toast={toast} paused={paused} duration={duration} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  )
}

function Toast({
  toast,
  paused,
  duration,
  onDismiss,
}: {
  toast: ToastItem
  paused: boolean
  duration: number
  onDismiss: (id: number) => void
}) {
  const t = useGuiT()
  useEffect(() => {
    if (paused) return
    const timer = window.setTimeout(() => onDismiss(toast.id), duration)
    return () => window.clearTimeout(timer)
  }, [paused, duration, onDismiss, toast.id])

  return (
    <div className="bm-toast">
      <p className="bm-toast__message">{toast.message}</p>
      <button type="button" className="bm-toast__dismiss" aria-label={t("gui.toast.dismiss")} onClick={() => onDismiss(toast.id)}>
        <span aria-hidden="true">×</span>
      </button>
    </div>
  )
}

/** Show a toast from anywhere under a {@link ToastProvider}. */
export function useToast(): ShowToast {
  const show = useContext(ToastContext)
  if (!show) throw new Error("useToast needs a ToastProvider")
  return show
}
