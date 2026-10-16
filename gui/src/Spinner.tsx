import { useGuiT } from "./messages"

export interface SpinnerProps {
  /** What is loading, when that says more than "Loading…". */
  label?: string
  className?: string
}

/** Progress for a request with no authoritative answer yet; never a count or an empty state. */
export function Spinner({ label, className }: SpinnerProps) {
  const t = useGuiT()
  return (
    <span className={["bm-spinner", className].filter(Boolean).join(" ")} role="status">
      <span className="bm-spinner__mark" aria-hidden="true" />
      {label ?? t("gui.loading")}
    </span>
  )
}

export interface SkeletonProps {
  /** Placeholder rows, standing in for the content's own shape. */
  lines?: number
  label?: string
  className?: string
}

/** The shape of content still loading, so the frame does not jump when it arrives. */
export function Skeleton({ lines = 3, label, className }: SkeletonProps) {
  const t = useGuiT()
  return (
    <div className={["bm-skeleton", className].filter(Boolean).join(" ")} role="status">
      <span className="bm-visually-hidden">{label ?? t("gui.loading")}</span>
      {Array.from({ length: lines }, (_, index) => (
        <span key={index} className="bm-skeleton__line" aria-hidden="true" />
      ))}
    </div>
  )
}
