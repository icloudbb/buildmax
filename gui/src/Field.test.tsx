import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { Field, Input, Select, Textarea } from "./Field"
import { LocaleProvider } from "./i18n"

afterEach(() => {
  cleanup()
  localStorage.clear()
})

describe("Field", () => {
  it("names its control by the label", () => {
    render(
      <Field label="Title">
        <Input />
      </Field>,
    )
    const input = screen.getByRole("textbox", { name: "Title" })
    expect(input.className).toContain("bm-input")
  })

  it("keeps a caller's id on both the label and the control", () => {
    render(
      <Field id="issue-title" label="Title">
        <Input />
      </Field>,
    )
    expect(screen.getByLabelText("Title").id).toBe("issue-title")
  })

  it("describes the control with its hint and its error, and marks it invalid", () => {
    render(
      <Field label="Name" hint="Shown to members" error="Name is required">
        <Input />
      </Field>,
    )
    const input = screen.getByRole("textbox", { name: "Name" })
    expect(input.getAttribute("aria-invalid")).toBe("true")
    const describedBy = (input.getAttribute("aria-describedby") ?? "").split(" ")
    const texts = describedBy.map((id) => document.getElementById(id)?.textContent)
    expect(texts).toEqual(["Shown to members", "Name is required"])
    expect(screen.getByRole("alert").textContent).toBe("Name is required")
  })

  it("leaves a valid control undescribed and not invalid", () => {
    render(
      <Field label="Name">
        <Input aria-describedby="outside-help" />
      </Field>,
    )
    const input = screen.getByRole("textbox", { name: "Name" })
    expect(input.getAttribute("aria-invalid")).toBeNull()
    expect(input.getAttribute("aria-describedby")).toBe("outside-help")
  })

  it("pairs a select and a textarea the same way", () => {
    render(
      <>
        <Field label="Status">
          <Select defaultValue="todo">
            <option value="todo">To do</option>
          </Select>
        </Field>
        <Field label="Description">
          <Textarea />
        </Field>
      </>,
    )
    expect(screen.getByRole("combobox", { name: "Status" }).className).toContain("bm-select")
    expect(screen.getByRole("textbox", { name: "Description" }).className).toContain("bm-textarea")
  })

  it("marks an optional field in the interface language", () => {
    localStorage.setItem("buildmax_locale", "zh-CN")
    render(
      <LocaleProvider>
        <Field label="描述" optional>
          <Input />
        </Field>
      </LocaleProvider>,
    )
    expect(screen.getByRole("textbox", { name: /^描述\s*\(可选\)$/ })).toBeTruthy()
  })

  it("names a group of controls with a legend", () => {
    render(
      <Field group label="Notify" hint="Pick any">
        <label>
          <input type="checkbox" /> Email
        </label>
      </Field>,
    )
    const group = screen.getByRole("group", { name: "Notify" })
    expect(group.tagName).toBe("FIELDSET")
    expect(document.getElementById(group.getAttribute("aria-describedby") ?? "")?.textContent).toBe("Pick any")
    expect(screen.getByRole("checkbox", { name: "Email" })).toBeTruthy()
  })

  it("is an ordinary control outside a Field", () => {
    render(<Input aria-label="Search" />)
    const input = screen.getByRole("textbox", { name: "Search" })
    expect(input.id).toBe("")
  })
})
