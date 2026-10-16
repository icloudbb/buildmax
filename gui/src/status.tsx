import { useCallback } from "react"
import { useLocale, type Locale } from "./i18n"
import { guiTranslate, type GuiMessageKey } from "./messages"

/**
 * The coarse meaning a status carries, independent of which object it
 * belongs to. Text always names the status; the tone only adds a mark.
 */
export type StatusTone = "neutral" | "active" | "success" | "danger" | "attention"

// One vocabulary for business, run, and lifecycle status. Keys are the
// persisted values lowercased, so the execution plane's upper-case run status
// (SUCCEEDED) and the Issue pages' lower-case words (succeeded) read the same.
// `needs_answer` is not persisted: a finished run whose questions wait on a
// person is presented as one more status rather than a second label path.
const STATUS_TONES: Record<string, StatusTone> = {
  todo: "neutral",
  in_progress: "active",
  done: "success",
  pending: "neutral",
  scheduled: "active",
  running: "active",
  failing: "danger",
  canceling: "active",
  success: "success",
  succeeded: "success",
  failed: "danger",
  canceled: "neutral",
  blocked: "attention",
  retry_wait: "attention",
  waiting: "attention",
  needs_answer: "attention",
  draft: "neutral",
  published: "success",
  archived: "neutral",
  no_runs: "neutral",
  enabled: "success",
  paused: "neutral",
}

function normalize(status: string): string {
  return status.trim().toLowerCase()
}

/**
 * The label for a status in the given language. A value outside the
 * vocabulary is humanized rather than shown as a raw enum.
 */
export function statusLabel(status: string, locale: Locale = "en"): string {
  const key = normalize(status)
  if (key in STATUS_TONES) return guiTranslate(locale, `gui.status.${key}` as GuiMessageKey)
  return key.replace(/_/g, " ").replace(/^./, (first) => first.toUpperCase())
}

export function statusTone(status: string): StatusTone {
  return STATUS_TONES[normalize(status)] ?? "neutral"
}

/** {@link statusLabel} in the interface language. */
export function useStatusLabel(): (status: string) => string {
  const { locale } = useLocale()
  return useCallback((status: string) => statusLabel(status, locale), [locale])
}

export interface StatusLabelProps {
  status: string
  className?: string
}

/** A status as text plus a tone mark; the mark is decoration, the text is the meaning. */
export function StatusLabel({ status, className }: StatusLabelProps) {
  const label = useStatusLabel()
  const tone = statusTone(status)
  return (
    <span className={["bm-status", `bm-status--${tone}`, className].filter(Boolean).join(" ")} data-status={normalize(status)}>
      <span className="bm-status__mark" aria-hidden="true" />
      {label(status)}
    </span>
  )
}
