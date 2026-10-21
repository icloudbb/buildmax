import { describe, expect, it } from "vitest"

import { agentOptionLabels } from "./optionLabel"

describe("agentOptionLabels", () => {
  it("lists an Agent by name alone when its name is unique", () => {
    const labels = agentOptionLabels([
      { id: "agt_a", name: "Researcher", description: "Finds sources" },
      { id: "agt_b", name: "Writer" },
    ])
    expect(labels.get("agt_a")).toBe("Researcher")
    expect(labels.get("agt_b")).toBe("Writer")
  })

  it("tells Agents that share a name apart by description, else by id", () => {
    const labels = agentOptionLabels([
      { id: "agt_a", name: "Summarizer", description: "Onboarding notes\nmore" },
      { id: "agt_b", name: "Summarizer", description: "Release notes" },
      { id: "agt_c", name: "Summarizer", description: "" },
      { id: "agt_d", name: "Reviewer", description: "Same" },
      { id: "agt_e", name: "Reviewer", description: "Same" },
    ])
    expect(labels.get("agt_a")).toBe("Summarizer · Onboarding notes")
    expect(labels.get("agt_b")).toBe("Summarizer · Release notes")
    expect(labels.get("agt_c")).toBe("Summarizer · agt_c")
    expect(labels.get("agt_d")).toBe("Reviewer · agt_d")
    expect(labels.get("agt_e")).toBe("Reviewer · agt_e")
  })

  it("shortens a long description", () => {
    const labels = agentOptionLabels([
      { id: "agt_a", name: "Same", description: "x".repeat(80) },
      { id: "agt_b", name: "Same", description: "y" },
    ])
    expect(labels.get("agt_a")).toBe(`Same · ${"x".repeat(47)}…`)
  })
})
