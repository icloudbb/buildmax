import { CopyButton } from "./CopyButton"
import { useT } from "../i18n"

interface CopyableIdProps {
  /** What the ID names, such as "Task ID". It names the copy action too. */
  label: string
  id: string
  /** False where a surrounding term already shows the label, as in a `<dl>`. */
  showLabel?: boolean
}

/**
 * An opaque ID shown as secondary metadata: muted, monospace, and copyable,
 * never the label a person reads first (docs/design/portal-frontend-page-system.md,
 * the work/run detail anatomy).
 */
export function CopyableId({ label, id, showLabel = true }: CopyableIdProps) {
  const t = useT()
  return (
    <span className="copyable-id">
      {showLabel ? <span className="copyable-id__label">{label}</span> : null}
      <code className="copyable-id__value">{id}</code>
      <CopyButton value={id} accessibleLabel={t("common.copyLabeled", { label })} />
    </span>
  )
}
