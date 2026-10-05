import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"

// The interface language is a per-device preference, stored beside the theme.
// See docs/design/ui-experience-program.md (D5).
const LOCALE_KEY = "buildmax_locale"

export type Locale = "en" | "zh-CN"

export const LOCALES: readonly Locale[] = ["en", "zh-CN"]

/** Each locale's name in its own language, for a language switch. */
export const LOCALE_NAMES: Record<Locale, string> = {
  en: "English",
  "zh-CN": "简体中文",
}

/**
 * A message is text with optional `{name}` placeholders, or English singular
 * and plural forms chosen by the `count` variable.
 */
export type Message = string | { one: string; other: string }

export type MessageVars = Record<string, string | number>

/**
 * One area's messages. English is complete and authoritative; a key missing
 * from Chinese falls back to English at runtime and is reported by
 * {@link missingKeys}.
 */
export interface Messages<K extends string> {
  en: Record<K, Message>
  "zh-CN": Partial<Record<K, Message>>
}

function isLocale(value: unknown): value is Locale {
  return value === "en" || value === "zh-CN"
}

/** The stored choice, else the browser's language, else English. */
export function detectLocale(): Locale {
  try {
    const stored = localStorage.getItem(LOCALE_KEY)
    if (isLocale(stored)) return stored
  } catch {
    /* storage unavailable: fall through to the browser language */
  }
  if (typeof navigator !== "undefined" && navigator.language?.toLowerCase().startsWith("zh")) {
    return "zh-CN"
  }
  return "en"
}

interface LocaleContextValue {
  locale: Locale
  setLocale: (locale: Locale) => void
}

// Outside a provider (a component test, a standalone page) everything renders
// in English rather than throwing.
const fallbackContext: LocaleContextValue = { locale: "en", setLocale: () => {} }

const LocaleContext = createContext<LocaleContextValue | null>(null)

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>(detectLocale)

  useEffect(() => {
    document.documentElement.lang = locale
    try {
      localStorage.setItem(LOCALE_KEY, locale)
    } catch {
      /* the choice still applies for this page */
    }
  }, [locale])

  const value = useMemo(() => ({ locale, setLocale }), [locale])
  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
}

export function useLocale(): LocaleContextValue {
  return useContext(LocaleContext) ?? fallbackContext
}

/** Fill `{name}` placeholders and pick the plural form for `count`. */
export function formatMessage(message: Message, vars?: MessageVars): string {
  const text =
    typeof message === "string" ? message : vars?.count === 1 ? message.one : message.other
  if (!vars) return text
  return text.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
    name in vars ? String(vars[name]) : placeholder,
  )
}

type KeysOf<M> = M extends Messages<infer K> ? K : never

/** Combine per-area message sets into one catalog. Keys must not collide. */
export function mergeMessages<M extends Messages<string>[]>(
  ...parts: M
): Messages<KeysOf<M[number]>> {
  const merged: Messages<string> = { en: {}, "zh-CN": {} }
  for (const part of parts) {
    for (const key of Object.keys(part.en)) {
      if (key in merged.en) throw new Error(`duplicate message key: ${key}`)
    }
    Object.assign(merged.en, part.en)
    Object.assign(merged["zh-CN"], part["zh-CN"])
  }
  return merged as Messages<KeysOf<M[number]>>
}

/** Keys that have English text but no Chinese translation. */
export function missingKeys(messages: Messages<string>): string[] {
  return Object.keys(messages.en).filter((key) => !(key in messages["zh-CN"]))
}

export type Translate<K extends string> = (key: K, vars?: MessageVars) => string

/**
 * Bind a catalog to the locale context. `useT` serves rendering; `useStableT`
 * serves text composed inside effects and async callbacks, whose dependencies
 * must not change with the language or a switch would reload data; `translate`
 * serves code that already knows the locale.
 */
export function createTranslator<K extends string>(messages: Messages<K>) {
  function translate(locale: Locale, key: K, vars?: MessageVars): string {
    const message = messages[locale][key] ?? messages.en[key]
    return message === undefined ? key : formatMessage(message, vars)
  }

  function useT(): Translate<K> {
    const { locale } = useLocale()
    return useCallback((key: K, vars?: MessageVars) => translate(locale, key, vars), [locale])
  }

  function useStableT(): Translate<K> {
    const { locale } = useLocale()
    const current = useRef(locale)
    useEffect(() => {
      current.current = locale
    }, [locale])
    return useCallback((key: K, vars?: MessageVars) => translate(current.current, key, vars), [])
  }

  return { translate, useT, useStableT }
}
