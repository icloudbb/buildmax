import { afterEach, describe, expect, it, vi } from "vitest"
import { getApiBase, parseErrorBody, throwIfNotOk, ApiRequestError } from "./client"

// The published image ships one bundle for every deployment, so this precedence
// is what makes the image usable at all. A regression here is invisible in dev
// (where VITE_API_BASE or the default answers) and total in a container.
describe("getApiBase", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
  })

  it("falls back to the local server when nothing is configured", () => {
    vi.stubGlobal("window", {})
    expect(getApiBase()).toBe("http://localhost:5678")
  })

  it("uses the build-time value when there is no runtime config", () => {
    vi.stubGlobal("window", { __BUILDMAX_CONFIG__: {} })
    vi.stubEnv("VITE_API_BASE", "http://build.example.com")
    expect(getApiBase()).toBe("http://build.example.com")
  })

  it("prefers the runtime config over the build-time value", () => {
    vi.stubGlobal("window", { __BUILDMAX_CONFIG__: { apiBase: "https://runtime.example.com" } })
    vi.stubEnv("VITE_API_BASE", "http://build.example.com")
    expect(getApiBase()).toBe("https://runtime.example.com")
  })

  it("ignores an empty runtime value, which is what an unset env var writes", () => {
    vi.stubGlobal("window", { __BUILDMAX_CONFIG__: { apiBase: "" } })
    vi.stubEnv("VITE_API_BASE", "http://build.example.com")
    expect(getApiBase()).toBe("http://build.example.com")
  })

  it("trims trailing slashes so request paths do not double up", () => {
    vi.stubGlobal("window", { __BUILDMAX_CONFIG__: { apiBase: "https://api.example.com/" } })
    expect(getApiBase()).toBe("https://api.example.com")
  })

  it('turns "/" into a same-origin base, for a reverse proxy in front of both', () => {
    vi.stubGlobal("window", { __BUILDMAX_CONFIG__: { apiBase: "/" } })
    expect(getApiBase()).toBe("")
  })
})

// A coded refusal is worded by Portal from its code, so the code has to reach
// the thrown error; the message stays the server's for every other caller.
describe("error bodies", () => {
  it("reads the message and, when present, the code", () => {
    expect(parseErrorBody('{"error":"no account","code":"invitee_account_required"}', "x")).toEqual({
      message: "no account",
      code: "invitee_account_required",
    })
    expect(parseErrorBody('{"error":"nope"}', "x")).toEqual({ message: "nope", code: undefined })
    expect(parseErrorBody("plain text", "x")).toEqual({ message: "plain text" })
    expect(parseErrorBody("", "fallback")).toEqual({ message: "fallback" })
  })

  it("throws an ApiRequestError carrying status and code", async () => {
    const res = new Response('{"error":"no account","code":"invitee_account_required"}', { status: 400 })
    const err = await throwIfNotOk(res).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiRequestError)
    expect((err as ApiRequestError).status).toBe(400)
    expect((err as ApiRequestError).code).toBe("invitee_account_required")
    expect((err as ApiRequestError).message).toBe("no account")
  })
})
