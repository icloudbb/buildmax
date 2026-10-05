import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import {
  createTranslator,
  formatMessage,
  LocaleProvider,
  mergeMessages,
  missingKeys,
  useLocale,
  type Messages,
} from "./i18n"
import { guiMessages } from "./messages"
import { BaseModal } from "./BaseModal"

const sample = {
  en: { "s.hello": "Hello, {name}", "s.items": { one: "{count} item", other: "{count} items" }, "s.only": "English only" },
  "zh-CN": { "s.hello": "你好，{name}", "s.items": "{count} 项" },
} satisfies Messages<string>

const { translate, useT, useStableT } = createTranslator(sample)

function Greeting() {
  const t = useT()
  const { setLocale } = useLocale()
  return (
    <>
      <p>{t("s.hello", { name: "Ada" })}</p>
      <button type="button" onClick={() => setLocale("zh-CN")}>zh</button>
    </>
  )
}

afterEach(() => {
  cleanup()
  localStorage.clear()
})

describe("formatMessage", () => {
  it("fills placeholders and leaves unknown ones visible", () => {
    expect(formatMessage("{a} and {b}", { a: 1 })).toBe("1 and {b}")
  })

  it("chooses the English plural form by count", () => {
    expect(translate("en", "s.items", { count: 1 })).toBe("1 item")
    expect(translate("en", "s.items", { count: 2 })).toBe("2 items")
  })
})

describe("translate", () => {
  it("falls back to English for a key Chinese lacks", () => {
    expect(translate("zh-CN", "s.only")).toBe("English only")
    expect(translate("zh-CN", "s.hello", { name: "Ada" })).toBe("你好，Ada")
  })
})

describe("missingKeys", () => {
  it("lists keys without a Chinese translation", () => {
    expect(missingKeys(sample)).toEqual(["s.only"])
  })

  it("finds none in the shared components' own catalog", () => {
    expect(missingKeys(guiMessages)).toEqual([])
  })
})

describe("mergeMessages", () => {
  it("rejects a key defined by two areas", () => {
    expect(() => mergeMessages(sample, sample)).toThrow(/duplicate message key/)
  })
})

describe("LocaleProvider", () => {
  it("renders English outside a provider", () => {
    render(<Greeting />)
    expect(screen.getByText("Hello, Ada")).toBeTruthy()
  })

  it("switches language in place and remembers the choice", () => {
    render(
      <LocaleProvider>
        <Greeting />
      </LocaleProvider>,
    )
    act(() => fireEvent.click(screen.getByRole("button", { name: "zh" })))
    expect(screen.getByText("你好，Ada")).toBeTruthy()
    expect(document.documentElement.lang).toBe("zh-CN")
    expect(localStorage.getItem("buildmax_locale")).toBe("zh-CN")
  })

  it("starts from a stored choice", () => {
    localStorage.setItem("buildmax_locale", "zh-CN")
    render(
      <LocaleProvider>
        <BaseModal open title="T" titleId="t" onClose={() => {}}>
          body
        </BaseModal>
      </LocaleProvider>,
    )
    expect(screen.getByRole("button", { name: "关闭" })).toBeTruthy()
  })
})

describe("useStableT", () => {
  it("keeps one identity across a language switch and translates in the current language", () => {
    const seen: unknown[] = []
    let latest: ((key: "s.hello", vars: { name: string }) => string) | null = null
    function Probe() {
      const stable = useStableT()
      const { setLocale } = useLocale()
      seen.push(stable)
      latest = stable
      return <button type="button" onClick={() => setLocale("zh-CN")}>zh</button>
    }
    render(
      <LocaleProvider>
        <Probe />
      </LocaleProvider>,
    )
    act(() => fireEvent.click(screen.getByRole("button", { name: "zh" })))
    expect(new Set(seen).size).toBe(1)
    expect(latest!("s.hello", { name: "Ada" })).toBe("你好，Ada")
  })
})
