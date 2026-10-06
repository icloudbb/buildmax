import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import type { ApiSecret } from "../lib/api/types"
import { translate, type MessageKey } from "../i18n"
import { consumptionHealthCount, grantHealth } from "./SecretConsumptionEditor"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function secret(over: Partial<ApiSecret>): ApiSecret {
  return {
    id: "sec_1",
    space_id: "tm_1",
    name: "gh",
    description: "",
    provider: "embedded",
    state: "active",
    item_names: ["token"],
    created_by: "u1",
    created_at: "",
    updated_at: "",
    ...over,
  }
}

describe("grantHealth", () => {
  const secrets = [
    secret({ id: "sec_gh", name: "gh", item_names: ["token"] }),
    secret({ id: "sec_off", name: "off", state: "disabled", item_names: ["k"] }),
    secret({ id: "sec_dead", name: "dead", state: "destroyed", item_names: [] }),
  ]

  it("passes a resolvable grant", () => {
    expect(grantHealth({ secret: "sec_gh", item: "token", env_name: "GH" }, secrets, t)).toBeNull()
  })
  it("passes a whole-group grant", () => {
    expect(grantHealth({ secret: "sec_gh" }, secrets, t)).toBeNull()
  })
  it("ignores an unfinished row with no secret", () => {
    expect(grantHealth({ secret: "", item: "x", env_name: "X" }, secrets, t)).toBeNull()
  })
  it("flags a missing secret", () => {
    expect(grantHealth({ secret: "sec_gone", item: "token", env_name: "X" }, secrets, t)).toMatch(
      /no longer exists/,
    )
  })
  it("flags a disabled secret", () => {
    expect(grantHealth({ secret: "sec_off", item: "k", env_name: "K" }, secrets, t)).toMatch(/disabled/)
  })
  it("flags a destroyed secret", () => {
    expect(grantHealth({ secret: "sec_dead" }, secrets, t)).toMatch(/destroyed/)
  })
  it("flags a missing item", () => {
    expect(grantHealth({ secret: "sec_gh", item: "gone", env_name: "X" }, secrets, t)).toMatch(
      /no longer has an item/,
    )
  })
})

describe("consumptionHealthCount", () => {
  const secrets = [secret({ id: "sec_gh", item_names: ["token"] })]
  it("counts only unresolvable grants", () => {
    const consumption = {
      env: [
        { secret: "sec_gh", item: "token", env_name: "OK" }, // fine
        { secret: "sec_gh", item: "gone", env_name: "BAD" }, // missing item
        { secret: "sec_missing", item: "x", env_name: "GONE" }, // missing secret
      ],
    }
    expect(consumptionHealthCount(consumption, secrets)).toBe(2)
  })
  it("is zero for empty consumption", () => {
    expect(consumptionHealthCount(undefined, secrets)).toBe(0)
    expect(consumptionHealthCount({ env: [] }, secrets)).toBe(0)
  })
})
