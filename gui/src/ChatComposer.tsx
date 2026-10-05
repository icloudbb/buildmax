import type { KeyboardEvent } from "react"
import { useGuiT } from "./messages"

export interface ChatComposerProps {
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  onKeyDown?: (e: KeyboardEvent<HTMLTextAreaElement>) => void
  disabled?: boolean
  loading?: boolean
  error?: string | null
  placeholder?: string
  ariaLabel?: string
  submitLabel?: string
  loadingLabel?: string
  rows?: number
  allowShiftEnter?: boolean
  // onCancel, when set, replaces the loading-state Send button with an active
  // Stop button. Callers wire this to a cooperative cancellation of the in-flight
  // run. When omitted, loading-state behavior is unchanged.
  onCancel?: () => void
  cancelLabel?: string
  // queueWhileLoading keeps the input editable during loading and lets submit
  // through, for surfaces that queue a message typed mid-run instead of refusing
  // it. The caller decides what submit means then — this component only stops
  // getting in the way. When onCancel is also set the button stays Stop, because
  // one button cannot be both, and enter is the way to queue.
  queueWhileLoading?: boolean
  queueLabel?: string
  queuePlaceholder?: string
  // ghost is a predicted next message offered to the user, accepted with Tab.
  // It rides on the placeholder because the two have the same rule — shown only
  // while the input is empty — so typing withdraws the offer without this
  // component having to watch for it. onAcceptGhost is what Tab calls; without
  // it Tab keeps moving focus, which is what a keyboard user expects when
  // nothing is on offer.
  ghost?: string
  onAcceptGhost?: () => void
}


// The action buttons are icon-only to keep the composer compact; the state's
// text still rides on aria-label and title, so screen readers and hover keep it.
function SendIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 19V5M12 5l-6 6M12 5l6 6"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function StopIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor" />
    </svg>
  )
}

function SpinnerIcon() {
  return (
    <svg
      className="bm-chat-composer__spinner"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" opacity="0.25" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  )
}

export function ChatComposer({
  value,
  onChange,
  onSubmit,
  onKeyDown: onKeyDownProp,
  disabled = false,
  loading = false,
  error,
  placeholder: placeholderProp,
  ariaLabel: ariaLabelProp,
  submitLabel: submitLabelProp,
  loadingLabel: loadingLabelProp,
  rows = 2,
  allowShiftEnter = true,
  onCancel,
  cancelLabel: cancelLabelProp,
  queueWhileLoading = false,
  queueLabel: queueLabelProp,
  queuePlaceholder,
  ghost,
  onAcceptGhost,
}: ChatComposerProps) {
  const t = useGuiT()
  const placeholder = placeholderProp ?? t("gui.composer.placeholder")
  const ariaLabel = ariaLabelProp ?? t("gui.composer.message")
  const submitLabel = submitLabelProp ?? t("gui.composer.send")
  const loadingLabel = loadingLabelProp ?? t("gui.composer.sending")
  const cancelLabel = cancelLabelProp ?? t("gui.composer.stop")
  const queueLabel = queueLabelProp ?? t("gui.composer.queue")
  const queueing = loading && queueWhileLoading
  const showCancel = loading && !!onCancel
  const inputDisabled = disabled || (loading && !queueing)
  const isSubmitDisabled = disabled || !value.trim() || (loading && !queueing)
  // On offer only while the input is empty and nothing is running: what the
  // user is about to send is whatever they have typed, if they have typed.
  const ghostOffered = !!ghost && !value && !loading && !disabled && !!onAcceptGhost

  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    // Let the parent intercept first (e.g. for slash command navigation).
    if (onKeyDownProp) {
      onKeyDownProp(e)
      if (e.defaultPrevented) return
    }
    if (e.key === "Tab" && ghostOffered) {
      e.preventDefault()
      onAcceptGhost?.()
      return
    }
    if (e.key !== "Enter") return
    if (allowShiftEnter && e.shiftKey) return

    e.preventDefault()
    if (!isSubmitDisabled) onSubmit()
  }

  return (
    <>
      <div className="bm-chat-composer">
        <textarea
          className={`bm-chat-composer__input${ghostOffered ? " bm-chat-composer__input--ghost" : ""}`}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={
            ghostOffered ? ghost : queueing && queuePlaceholder ? queuePlaceholder : placeholder
          }
          rows={rows}
          disabled={inputDisabled}
          aria-label={ariaLabel}
        />
        {ghostOffered && (
          <span className="bm-chat-composer__ghost-hint" aria-hidden>
            Tab
          </span>
        )}
        {showCancel ? (
          <button
            type="button"
            className="bm-chat-composer__button bm-chat-composer__button--cancel"
            onClick={onCancel}
            aria-label={cancelLabel}
            title={cancelLabel}
          >
            <StopIcon />
          </button>
        ) : (
          <button
            type="button"
            className="bm-chat-composer__button"
            onClick={onSubmit}
            disabled={isSubmitDisabled}
            aria-label={queueing ? queueLabel : loading ? loadingLabel : submitLabel}
            title={queueing ? queueLabel : loading ? loadingLabel : submitLabel}
          >
            {loading && !queueing ? <SpinnerIcon /> : <SendIcon />}
          </button>
        )}
      </div>
      {error ? (
        <p className="bm-chat-composer__error" role="alert">
          {error}
        </p>
      ) : null}
    </>
  )
}
