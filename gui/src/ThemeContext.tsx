import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react"

const THEME_KEY = "buildmax_theme"
const DARK_QUERY = "(prefers-color-scheme: dark)"

export type Theme = "light" | "dark"

interface ThemeContextValue {
  theme: Theme
  setTheme: (theme: Theme) => void
  toggleTheme: () => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

/** The person's own choice on this device, or null when they have not made one. */
function loadStoredTheme(): Theme | null {
  try {
    const stored = localStorage.getItem(THEME_KEY)
    if (stored === "dark" || stored === "light") return stored
  } catch {
    /* ignore */
  }
  return null
}

function darkQuery(): MediaQueryList | null {
  return typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia(DARK_QUERY)
    : null
}

function systemTheme(): Theme {
  return darkQuery()?.matches ? "dark" : "light"
}

/**
 * The theme is the operating system's until the person picks one. Only a pick
 * is stored: storing the OS value on first load would freeze it, and the app
 * would stop following a later OS switch the person never overrode.
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [choice, setChoice] = useState<Theme | null>(loadStoredTheme)
  const [system, setSystem] = useState<Theme>(systemTheme)
  const theme = choice ?? system

  useEffect(() => {
    const query = darkQuery()
    if (!query) return
    const onChange = () => setSystem(query.matches ? "dark" : "light")
    query.addEventListener("change", onChange)
    return () => query.removeEventListener("change", onChange)
  }, [])

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme)
  }, [theme])

  const setTheme = useCallback((next: Theme) => {
    setChoice(next)
    try {
      localStorage.setItem(THEME_KEY, next)
    } catch {
      /* ignore */
    }
  }, [])

  const toggleTheme = useCallback(() => {
    setTheme(theme === "light" ? "dark" : "light")
  }, [theme, setTheme])

  const value: ThemeContextValue = { theme, setTheme, toggleTheme }

  return (
    <ThemeContext.Provider value={value}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider")
  return ctx
}
