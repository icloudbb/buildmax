import { ApiRequestError } from "../../lib/api/client"
import { getErrorMessage } from "../../lib/errorMessage"
import type { MessageKey } from "../../i18n"
import type { NativeSignInRequest } from "./api"

/**
 * The states of the sign-in confirmation page, and the pure decisions that move
 * between them. The page is these plus JSX, so each state is testable without
 * a browser. See docs/design/enterprise-identity-and-access.md section 14.4.
 */
export type SignInPhase =
  /** No code yet: the person types the one their client shows. */
  | { kind: "entry" }
  | { kind: "loading" }
  /** A pending request, waiting for an explicit Confirm or Deny. */
  | {
      kind: "request"
      code: string
      request: NativeSignInRequest
      /** Judged when the request arrived; see sessionEndsSoon. */
      endsSoon: boolean
      deciding: "confirm" | "deny" | null
      error: DecisionError | null
    }
  /** Wrong, expired, or already decided: the server does not say which, nor does the page. */
  | { kind: "invalid" }
  /** This account tried too many unknown codes. */
  | { kind: "throttled" }
  | { kind: "failed"; code: string; message: string }
  | { kind: "decided"; decision: "confirmed" | "denied"; platform: string }

/** A decision the server refused without ending the request. */
export interface DecisionError {
  message: string
  /**
   * The server will not take a confirmation from this Portal session, such as
   * one signed in by a way the deployment no longer admits. Signing in again
   * is the way forward.
   */
  reauthenticate: boolean
}

/** Below this much Portal session left, the page offers a fresh sign-in first. */
export const SESSION_ENDING_SOON_MS = 60 * 60 * 1000

/**
 * The code as a client shows it, `XXXX-XXXX`. The server accepts any case,
 * hyphens, and spaces, so this only affects what the person compares.
 */
export function displayCode(raw: string): string {
  const compact = raw.replace(/[\s-]/g, "").toUpperCase()
  return compact.length === 8 ? `${compact.slice(0, 4)}-${compact.slice(4)}` : raw.trim().toUpperCase()
}

/**
 * Whether a session confirmed now would be short: it ends with this Portal
 * session, never later (design section 14.5).
 */
export function sessionEndsSoon(sessionExpiresAt: string, now: number): boolean {
  const ends = new Date(sessionExpiresAt).getTime()
  return Number.isFinite(ends) && ends - now < SESSION_ENDING_SOON_MS
}

/** The product name of the asking client. */
export function clientNameKey(platform: string): MessageKey {
  return platform === "desktop" ? "signIn.client.desktop" : "signIn.client.cli"
}

function status(err: unknown): number | undefined {
  return err instanceof ApiRequestError ? err.status : undefined
}

/** Where a failed look-up leaves the page. */
export function lookUpFailure(err: unknown, code: string, fallback: string): SignInPhase {
  switch (status(err)) {
    case 404:
      return { kind: "invalid" }
    case 429:
      return { kind: "throttled" }
    default:
      return { kind: "failed", code, message: getErrorMessage(err, fallback) }
  }
}

/**
 * Where a refused Confirm or Deny leaves the page. A code that stopped being
 * valid, or a throttled account, replaces the request; any other refusal keeps
 * it on screen with the reason, since the request may still be decided.
 */
export function decisionFailure(
  phase: Extract<SignInPhase, { kind: "request" }>,
  err: unknown,
  fallback: string
): SignInPhase {
  switch (status(err)) {
    case 404:
      return { kind: "invalid" }
    case 429:
      return { kind: "throttled" }
    default:
      return {
        ...phase,
        deciding: null,
        error: { message: getErrorMessage(err, fallback), reauthenticate: status(err) === 403 },
      }
  }
}
