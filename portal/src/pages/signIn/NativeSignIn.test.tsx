import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import type { SignInPhase } from "../../features/nativeSignIn/model"
import { NativeSignInView } from "./NativeSignIn"

// The page in each of its states, rendered to markup. The view holds no data
// fetching, so what a state shows is exactly what these assert.

const noop = () => {}

function render(phase: SignInPhase): string {
  return renderToStaticMarkup(
    <NativeSignInView
      phase={phase}
      email="ada@example.com"
      onLookUp={noop}
      onConfirm={noop}
      onDeny={noop}
      onSignInAgain={noop}
      onEnterAnother={noop}
    />
  )
}

function text(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/&#x27;/g, "'").replace(/\s+/g, " ")
}

const pending: Extract<SignInPhase, { kind: "request" }> = {
  kind: "request",
  code: "wdjbmjht",
  request: {
    platform: "cli",
    client_label: "alice-mbp · macOS",
    created_at: "2026-03-04T15:25:00Z",
    expires_at: "2026-03-04T15:35:00Z",
    session_expires_at: "2026-03-05T03:25:00Z",
  },
  endsSoon: false,
  deciding: null,
  error: null,
}

describe("NativeSignInView", () => {
  it("asks for a code when the address carries none", () => {
    const html = render({ kind: "entry" })
    expect(text(html)).toContain("Enter the code your BuildMax CLI or Desktop shows")
    expect(html).toContain('placeholder="XXXX-XXXX"')
    expect(text(html)).toContain("Continue")
    expect(text(html)).not.toContain("Confirm sign-in")
  })

  it("says it is looking the request up", () => {
    expect(text(render({ kind: "loading" }))).toContain("Looking up the sign-in")
  })

  it("shows who is asking, the code, the account, and the warning before both choices", () => {
    const shown = text(render(pending))
    expect(shown).toContain("BuildMax CLI is asking to sign in to your account")
    expect(shown).toContain("alice-mbp · macOS")
    expect(shown).toContain("Reported by the client, not verified")
    expect(shown).toContain("WDJB-MJHT")
    expect(shown).toContain("Check that this matches the code your CLI or Desktop shows.")
    expect(shown).toContain("ada@example.com")
    expect(shown).toContain("The session it creates ends")
    expect(shown).toContain(
      "Only confirm if you started this sign-in yourself, just now. If someone sent you this link or code, deny it."
    )
    expect(shown).toContain("Confirm sign-in")
    expect(shown).toContain("Deny")
    // A full-length Portal session needs no fresh sign-in.
    expect(shown).not.toContain("Sign in again")
  })

  it("names the Desktop and says when the client reported no name", () => {
    const shown = text(render({ ...pending, request: { ...pending.request, platform: "desktop", client_label: "" } }))
    expect(shown).toContain("BuildMax Desktop is asking to sign in")
    expect(shown).toContain("The client reported no name")
    expect(shown).not.toContain("not verified")
  })

  it("offers a fresh sign-in when under an hour of the Portal session is left", () => {
    const shown = text(render({ ...pending, endsSoon: true }))
    expect(shown).toContain("Your Portal session ends in less than an hour")
    expect(shown).toContain("Sign in again")
    expect(shown).toContain("Confirm sign-in")
  })

  it("holds both choices while one is being recorded", () => {
    const html = render({ ...pending, deciding: "confirm" })
    expect(html.match(/<button[^>]*disabled=""/g)?.length).toBe(2)
    expect(html).toContain('aria-busy="true"')
  })

  it("keeps the request with the reason, and a way forward, when a confirmation is refused", () => {
    const shown = text(
      render({
        ...pending,
        error: { message: "sign in to the Portal again to confirm this sign-in", reauthenticate: true },
      })
    )
    expect(shown).toContain("sign in to the Portal again to confirm this sign-in")
    expect(shown).toContain("Sign in again")
    expect(shown).toContain("Confirm sign-in")
  })

  it("has one not-valid state, which does not say why", () => {
    const shown = text(render({ kind: "invalid" }))
    expect(shown).toContain("This code is not valid any more.")
    expect(shown).toContain("Enter another code")
    expect(shown).not.toContain("Confirm sign-in")
  })

  it("gives too many tried codes its own message", () => {
    const shown = text(render({ kind: "throttled" }))
    expect(shown).toContain("Too many codes were tried from this account.")
    expect(shown).not.toContain("not valid any more")
  })

  it("offers a retry for any other failure", () => {
    const shown = text(render({ kind: "failed", code: "WDJB-MJHT", message: "native sign-in not configured" }))
    expect(shown).toContain("native sign-in not configured")
    expect(shown).toContain("Try again")
  })

  it("sends the person back to the client after a confirmation", () => {
    const shown = text(render({ kind: "decided", decision: "confirmed", platform: "cli" }))
    expect(shown).toContain("Sign-in confirmed. Return to BuildMax CLI")
    expect(shown).not.toContain("WDJB")
    expect(shown).not.toContain("Confirm sign-in")
  })

  it("says the client will report a denial", () => {
    const shown = text(render({ kind: "decided", decision: "denied", platform: "desktop" }))
    expect(shown).toContain("Sign-in denied. BuildMax Desktop will report that the sign-in was not allowed.")
  })
})
