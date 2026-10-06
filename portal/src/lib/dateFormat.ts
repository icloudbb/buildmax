import { useCallback } from "react"
import { useLocale, type Locale } from "@buildmax/gui"
import { translate } from "../i18n"

// English keeps the browser's regional date and clock format, as the Portal
// always has; Chinese uses the Chinese one so the words and numbers agree.
export function intlLocale(locale: Locale): string | undefined {
  return locale === "en" ? undefined : locale
}

/**
 * An RFC 3339 instant as "Today HH:MM", "Yesterday HH:MM", or a full date and
 * time, in the given interface language.
 */
export function formatRelativeTime(rfc3339: string, locale: Locale = "en", now: Date = new Date()): string {
  const d = new Date(rfc3339)
  const time = () => d.toLocaleTimeString(intlLocale(locale), { hour: "2-digit", minute: "2-digit" })
  if (d.toDateString() === now.toDateString()) {
    return translate(locale, "common.time.today", { time: time() })
  }
  const yesterday = new Date(now)
  yesterday.setDate(yesterday.getDate() - 1)
  if (d.toDateString() === yesterday.toDateString()) {
    return translate(locale, "common.time.yesterday", { time: time() })
  }
  return d.toLocaleString(intlLocale(locale))
}

/**
 * An RFC 3339 instant as a full date and time in the given interface language;
 * a dash when there is no instant, rather than the epoch or "Invalid Date".
 */
export function formatTimestamp(rfc3339: string | null | undefined, locale: Locale = "en"): string {
  if (!rfc3339) return "—"
  const d = new Date(rfc3339)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString(intlLocale(locale))
}

/** {@link formatRelativeTime} in the interface language, formatted at render time. */
export function useRelativeTime(): (rfc3339: string) => string {
  const { locale } = useLocale()
  return useCallback((rfc3339: string) => formatRelativeTime(rfc3339, locale), [locale])
}

/** {@link formatTimestamp} in the interface language, formatted at render time. */
export function useTimestamp(): (rfc3339: string | null | undefined) => string {
  const { locale } = useLocale()
  return useCallback((rfc3339: string | null | undefined) => formatTimestamp(rfc3339, locale), [locale])
}
