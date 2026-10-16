import type { ComponentType } from "react"
import { afterEach, describe, expect, it } from "vitest"
import { cleanup, render } from "@testing-library/react"
import * as icons from "./icons"

afterEach(cleanup)

const entries = Object.entries(icons).filter(([name]) => name.endsWith("Icon")) as [
  string,
  ComponentType<icons.IconProps>,
][]

describe("icons", () => {
  it.each(entries)("%s is a decorative line icon that takes the caller's class and size", (_name, Icon) => {
    const { container } = render(<Icon className="probe" width={12} height={12} />)
    const svg = container.querySelector("svg") as SVGSVGElement
    expect(svg.getAttribute("class")).toBe("bm-icon probe")
    expect(svg.getAttribute("aria-hidden")).toBe("true")
    expect(svg.getAttribute("viewBox")).toBe("0 0 24 24")
    expect(svg.getAttribute("width")).toBe("12")
    expect(svg.getAttribute("stroke")).toBe("currentColor")
    expect(svg.childElementCount).toBeGreaterThan(0)
  })
})
