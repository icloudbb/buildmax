import { describe, expect, it } from "vitest"
import { signInOptions, ssoErrorMessage } from "./loginView"

describe("signInOptions", () => {
  it("shows only local login when SSO is off", () => {
    const o = signInOptions({ local_login: "all", oidc: { enabled: false } })
    expect(o).toEqual({ showLocal: true, showSSO: false, providerName: "single sign-on" })
  })

  it("shows both, labelled by the provider, when SSO is enabled", () => {
    const o = signInOptions({ local_login: "all", oidc: { enabled: true, display_name: "Okta" } })
    expect(o.showLocal).toBe(true)
    expect(o.showSSO).toBe(true)
    expect(o.providerName).toBe("Okta")
  })

  it("keeps the local form under system_admins as a break-glass path", () => {
    const o = signInOptions({ local_login: "system_admins", oidc: { enabled: true, display_name: "Okta" } })
    expect(o.showLocal).toBe(true)
    expect(o.showSSO).toBe(true)
  })

  it("hides the local form only when native login is off", () => {
    const o = signInOptions({ local_login: "off", oidc: { enabled: true, display_name: "Okta" } })
    expect(o.showLocal).toBe(false)
    expect(o.showSSO).toBe(true)
  })

  it("falls back to a generic provider name when none is given", () => {
    const o = signInOptions({ local_login: "off", oidc: { enabled: true } })
    expect(o.providerName).toBe("single sign-on")
  })
})

describe("ssoErrorMessage", () => {
  it("maps each coarse class to a message", () => {
    expect(ssoErrorMessage("unavailable")).toMatch(/unavailable/i)
    expect(ssoErrorMessage("expired")).toMatch(/expired|interrupted/i)
    expect(ssoErrorMessage("not_authorized")).toMatch(/not authorized/i)
    expect(ssoErrorMessage("disabled")).toMatch(/disabled/i)
  })

  it("is null for no code or an unknown one, so the page shows no error", () => {
    expect(ssoErrorMessage(null)).toBeNull()
    expect(ssoErrorMessage("something-else")).toBeNull()
  })
})
