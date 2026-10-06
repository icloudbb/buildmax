import { describe, expect, it } from "vitest"
import { translate, type MessageKey } from "../../i18n"
import { signInOptions, ssoErrorMessage } from "./loginView"

const t = (key: MessageKey) => translate("en", key)

describe("signInOptions", () => {
  it("shows only local login when SSO is off", () => {
    const o = signInOptions({ local_login: "all", oidc: { enabled: false } }, t)
    expect(o).toEqual({ showLocal: true, showSSO: false, providerName: "single sign-on" })
  })

  it("shows both, labelled by the provider, when SSO is enabled", () => {
    const o = signInOptions({ local_login: "all", oidc: { enabled: true, display_name: "Okta" } }, t)
    expect(o.showLocal).toBe(true)
    expect(o.showSSO).toBe(true)
    expect(o.providerName).toBe("Okta")
  })

  it("keeps the local form under system_admins as a break-glass path", () => {
    const o = signInOptions({ local_login: "system_admins", oidc: { enabled: true, display_name: "Okta" } }, t)
    expect(o.showLocal).toBe(true)
    expect(o.showSSO).toBe(true)
  })

  it("hides the local form only when native login is off", () => {
    const o = signInOptions({ local_login: "off", oidc: { enabled: true, display_name: "Okta" } }, t)
    expect(o.showLocal).toBe(false)
    expect(o.showSSO).toBe(true)
  })

  it("falls back to a generic provider name when none is given", () => {
    const o = signInOptions({ local_login: "off", oidc: { enabled: true } }, t)
    expect(o.providerName).toBe("single sign-on")
  })
})

describe("ssoErrorMessage", () => {
  it("maps each coarse class to a message", () => {
    expect(ssoErrorMessage("unavailable", t)).toMatch(/unavailable/i)
    expect(ssoErrorMessage("expired", t)).toMatch(/expired|interrupted/i)
    expect(ssoErrorMessage("not_authorized", t)).toMatch(/not authorized/i)
    expect(ssoErrorMessage("disabled", t)).toMatch(/disabled/i)
  })

  it("is null for no code or an unknown one, so the page shows no error", () => {
    expect(ssoErrorMessage(null, t)).toBeNull()
    expect(ssoErrorMessage("something-else", t)).toBeNull()
  })
})
