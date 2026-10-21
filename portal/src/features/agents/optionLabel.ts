import type { Agent } from "../../lib/types"

const DETAIL_MAX = 48

function shorten(text: string): string {
  const line = text.trim().split("\n")[0].trim()
  return line.length > DETAIL_MAX ? `${line.slice(0, DETAIL_MAX - 1).trimEnd()}…` : line
}

/**
 * What an Agent picker shows for each Agent, keyed by id. An Agent is listed by
 * its name; only when two Agents share a name does each gain a detail to tell
 * them apart: its description when no namesake shares it, otherwise its id,
 * the one value guaranteed to differ.
 */
export function agentOptionLabels(agents: Pick<Agent, "id" | "name" | "description">[]): Map<string, string> {
  const byName = new Map<string, Pick<Agent, "id" | "name" | "description">[]>()
  for (const agent of agents) {
    const group = byName.get(agent.name) ?? []
    group.push(agent)
    byName.set(agent.name, group)
  }
  const labels = new Map<string, string>()
  for (const [name, group] of byName) {
    if (group.length === 1) {
      labels.set(group[0].id, name)
      continue
    }
    const details = group.map((agent) => shorten(agent.description ?? ""))
    group.forEach((agent, i) => {
      const detail = details[i]
      const distinct = detail !== "" && details.filter((d) => d === detail).length === 1
      labels.set(agent.id, `${name} · ${distinct ? detail : agent.id}`)
    })
  }
  return labels
}
