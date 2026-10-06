import type { ReactNode, RefObject } from "react"

function cn(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(" ")
}

export interface ChatThreadItem {
  id: string
  role: string
  label?: string
  avatar?: ReactNode
  body?: ReactNode
  hideAvatar?: boolean
  collapsed?: boolean
}

export interface ChatThreadProps {
  historyRef?: RefObject<HTMLElement | null>
  ariaLabel: string
  items: ChatThreadItem[]
  loadingText?: string | null
  errorText?: string | null
  emptyText?: string | null
}

export function ChatThread({
  historyRef,
  ariaLabel,
  items,
  loadingText,
  errorText,
  emptyText,
}: ChatThreadProps) {
  return (
    // Focusable because it scrolls: a keyboard user has no other way to read
    // back through a long thread (WCAG 2.1.1, axe scrollable-region-focusable).
    // jsx-a11y treats every non-widget as unfocusable and has no scroll case.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
    <section ref={historyRef} className="bm-chat-thread" aria-label={ariaLabel} tabIndex={0}>
      {loadingText ? (
        <p className="bm-chat-thread__text bm-chat-thread__text--muted">{loadingText}</p>
      ) : null}
      {errorText ? (
        <p className="bm-chat-thread__text bm-chat-thread__text--error" role="alert">
          {errorText}
        </p>
      ) : null}
      {!loadingText && !errorText && items.length === 0 && emptyText ? (
        <p className="bm-chat-thread__text bm-chat-thread__text--muted">{emptyText}</p>
      ) : null}
      {!loadingText && !errorText
        ? items.map((item) => (
            <div
              key={item.id}
              className={cn("bm-chat-thread__row", `bm-chat-thread__row--${item.role}`)}
              role="article"
              aria-label={item.label ?? item.role}
            >
              {!item.hideAvatar ? (
                <span className="bm-chat-thread__icon" aria-hidden>
                  {item.avatar}
                </span>
              ) : null}
              <div
                className={cn(
                  "bm-chat-thread__bubble",
                  `bm-chat-thread__bubble--${item.role}`,
                  item.collapsed && "bm-chat-thread__bubble--collapsed"
                )}
              >
                {item.body}
              </div>
            </div>
          ))
        : null}
    </section>
  )
}
