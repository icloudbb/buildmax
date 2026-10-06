import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import type { AuthMethods } from "../../lib/api/types"

/**
 * The pure decisions the sign-in page makes from the advertised methods and the
 * callback's error code. Kept out of the component so they can be tested without
 * a DOM: what the page renders is just these plus JSX.
 */

export interface SignInOptions {
  /** Show the email/password/login-code form. Hidden only when native login is off. */
  showLocal: boolean
  /** Show the "Sign in with <provider>" button. */
  showSSO: boolean
  /** Label for the SSO button and headings. */
  providerName: string
}

export function signInOptions(methods: AuthMethods, t: Translate<MessageKey>): SignInOptions {
  return {
    // system_admins still shows the local form: an operator signs in that way,
    // and the server refuses everyone else. Only "off" hides it.
    showLocal: methods.local_login !== "off",
    showSSO: methods.oidc.enabled === true,
    providerName: methods.oidc.display_name?.trim() || t("auth.genericProvider"),
  }
}

/**
 * Turn the ?sso_error= code the OIDC callback may redirect back with into a
 * message. The code is a coarse, non-sensitive class; the raw provider error
 * never leaves the server log.
 */
export function ssoErrorMessage(code: string | null, t: Translate<MessageKey>): string | null {
  switch (code) {
    case "unavailable":
      return t("auth.sso.unavailable")
    case "expired":
      return t("auth.sso.expired")
    case "not_authorized":
      return t("auth.sso.notAuthorized")
    case "disabled":
      return t("auth.sso.disabled")
    default:
      return null
  }
}
