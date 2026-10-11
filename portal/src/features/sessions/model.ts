import type { MessageKey } from "../../i18n"
import type { AccountSession } from "./api"

/** What signed in, as a person names it. An unknown value is shown as unknown, not guessed. */
export function sessionPlatformKey(platform: string | undefined): MessageKey {
  switch (platform) {
    case "portal":
      return "account.sessions.platform.portal"
    case "cli":
      return "account.sessions.platform.cli"
    case "desktop":
      return "account.sessions.platform.desktop"
    default:
      return "account.sessions.platform.unknown"
  }
}

/** The sign-in behind the session, or null for a method this Portal does not know. */
export function sessionMethodKey(method: string | undefined): MessageKey | null {
  switch (method) {
    case "password":
      return "account.sessions.method.password"
    case "login_code":
      return "account.sessions.method.loginCode"
    case "oidc":
      return "account.sessions.method.oidc"
    default:
      return null
  }
}

/**
 * When the session was last used, or null before its first use. The server
 * sends Go's zero time for "never" rather than omitting the field.
 */
export function lastSeenAt(session: AccountSession): string | null {
  const at = new Date(session.last_seen_at)
  return Number.isNaN(at.getTime()) || at.getUTCFullYear() <= 1 ? null : session.last_seen_at
}

/** This browser first, then the server's newest-first order. */
export function orderSessions(sessions: AccountSession[]): AccountSession[] {
  return [...sessions.filter((s) => s.current), ...sessions.filter((s) => !s.current)]
}
