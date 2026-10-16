import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { LocaleProvider } from "./i18n"
import { guiMessages } from "./messages"
import { StatusLabel, statusLabel, statusTone } from "./status"

afterEach(() => {
  cleanup()
  localStorage.clear()
})

describe("statusLabel", () => {
  it("reads a run status the same whichever case the server stored it in", () => {
    expect(statusLabel("SUCCEEDED")).toBe("Succeeded")
    expect(statusLabel("succeeded")).toBe("Succeeded")
    expect(statusLabel("success")).toBe("Succeeded")
    expect(statusLabel("CANCELED")).toBe("Canceled")
    expect(statusLabel("PENDING")).toBe("Pending")
    expect(statusLabel("SCHEDULED")).toBe("Starting")
  })

  it("keeps a business status apart from a run outcome", () => {
    expect(statusLabel("done")).toBe("Done")
    expect(statusLabel("succeeded")).not.toBe(statusLabel("done"))
  })

  it("labels every status in Chinese", () => {
    expect(statusLabel("in_progress", "zh-CN")).toBe("进行中")
    expect(statusLabel("needs_answer", "zh-CN")).toBe("等待你的回答")
    const statusKeys = Object.keys(guiMessages.en).filter((key) => key.startsWith("gui.status."))
    for (const key of statusKeys) expect(key in guiMessages["zh-CN"]).toBe(true)
  })

  it("humanizes a value outside the vocabulary instead of showing the raw enum", () => {
    expect(statusLabel("workflow_step")).toBe("Workflow step")
  })
})

describe("statusTone", () => {
  it("groups statuses by meaning", () => {
    expect(statusTone("RUNNING")).toBe("active")
    expect(statusTone("succeeded")).toBe("success")
    expect(statusTone("failed")).toBe("danger")
    expect(statusTone("needs_answer")).toBe("attention")
    expect(statusTone("something_new")).toBe("neutral")
  })
})

describe("StatusLabel", () => {
  it("shows the label as text, with the tone as a hidden mark", () => {
    render(<StatusLabel status="FAILED" />)
    const label = screen.getByText("Failed")
    expect(label.className).toContain("bm-status--danger")
    expect(label.querySelector(".bm-status__mark")?.getAttribute("aria-hidden")).toBe("true")
  })

  it("follows the interface language", () => {
    localStorage.setItem("buildmax_locale", "zh-CN")
    render(
      <LocaleProvider>
        <StatusLabel status="todo" />
      </LocaleProvider>,
    )
    expect(screen.getByText("待办")).toBeTruthy()
  })
})
