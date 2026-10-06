import { useCallback } from "react"
import { useLocale, type Locale } from "@buildmax/gui"
import { translate, type MessageKey } from "../i18n"
import { statusMessages } from "../i18n/status"

/** Human-facing labels for persisted status values. Keep technical values in APIs. */
export function statusLabel(value: string, locale: Locale = "en"): string {
  const key = `status.${value}`
  if (key in statusMessages.en) return translate(locale, key as MessageKey)
  return value.replace(/_/g, " ").replace(/^./, (first: string) => first.toUpperCase())
}

/** {@link statusLabel} in the interface language. */
export function useStatusLabel(): (value: string) => string {
  const { locale } = useLocale()
  return useCallback((value: string) => statusLabel(value, locale), [locale])
}

/**
 * Where a conversation came from, or null for Portal chat itself. Only the
 * channels that arrive from elsewhere (a chat app such as Telegram, a webhook)
 * are worth marking in a list of mostly Portal conversations.
 */
export function conversationSourceLabel(channel: string): string | null {
  if (!channel || channel === "portal") return null
  return channel.replace(/^./, (first: string) => first.toUpperCase())
}
