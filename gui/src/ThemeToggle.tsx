import { useTheme } from "./ThemeContext"
import { useGuiT } from "./messages"
import { MoonIcon, SunIcon } from "./icons"

export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme()
  const t = useGuiT()

  return (
    <button
      type="button"
      className="theme-toggle"
      onClick={toggleTheme}
      aria-label={theme === "light" ? t("gui.theme.toDark") : t("gui.theme.toLight")}
      title={theme === "light" ? t("gui.theme.dark") : t("gui.theme.light")}
    >
      {theme === "light" ? (
        <MoonIcon className="theme-toggle__icon" />
      ) : (
        <SunIcon className="theme-toggle__icon" />
      )}
    </button>
  )
}
