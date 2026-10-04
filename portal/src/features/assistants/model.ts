import type {
  ApiAssistant,
  ApiAssistantAudience,
  ApiAssistantAvailability,
  ApiAssistantDefinition,
  ApiAssistantRosterEntry,
} from "../../lib/api/types"
import { parseDefinition, parseNodeOutputSource } from "../workflows/steps"

/**
 * The top-level property names of an object schema: the units a release
 * contract can release. Null when the schema is not an object schema, the one
 * shape the server accepts for a result (mirrors coreassistant.ObjectProperties).
 */
export function schemaProperties(schema: unknown): string[] | null {
  if (typeof schema !== "object" || schema === null || Array.isArray(schema)) return null
  const record = schema as { type?: unknown; properties?: unknown }
  if (record.type !== "object") return null
  const props = record.properties
  if (props === undefined) return []
  if (typeof props !== "object" || props === null || Array.isArray(props)) return null
  return Object.keys(props).sort()
}

/** Reads an Agent entry's output_schema text, as typed into the editor. */
export function parseOutputSchema(text: string): { schema: unknown; properties: string[] } | { error: string } {
  if (!text.trim()) return { error: "An Agent on the roster needs an output schema." }
  let schema: unknown
  try {
    schema = JSON.parse(text)
  } catch {
    return { error: "The output schema is not valid JSON." }
  }
  const properties = schemaProperties(schema)
  if (properties === null) return { error: 'The output schema must be an object schema ({"type": "object", ...}).' }
  return { schema, properties }
}

/**
 * The releasable fields a published Workflow offers: the top-level properties
 * of the output_schema on the node whose structured output its result selects
 * (pointer "/structured"). Null when that
 * cannot be read from the definition, in which case the server is the judge
 * and the editor falls back to free text.
 */
export function workflowResultProperties(definition: string): string[] | null {
  const parsed = parseDefinition(definition)
  if (!parsed?.result) return null
  let result: { source?: unknown; pointer?: unknown }
  try {
    result = JSON.parse(parsed.result) as typeof result
  } catch {
    return null
  }
  if (typeof result.source !== "string" || result.pointer !== "/structured") return null
  const nodeId = parseNodeOutputSource(result.source)
  const node = parsed.steps.find((step) => step.id === nodeId)
  if (!node?.outputSchema) return null
  try {
    return schemaProperties(JSON.parse(node.outputSchema))
  } catch {
    return null
  }
}

export interface AvailabilityView {
  /** Short badge text. */
  label: string
  /** Why it is not answering, or what answering means. */
  reason: string
  /** Matches the shared `.sec-status--*` tones. */
  tone: "active" | "suspended" | "blocked"
}

export function describeAvailability(availability: ApiAssistantAvailability): AvailabilityView {
  switch (availability) {
    case "available":
      return { label: "Available", reason: "Published and answering its audience.", tone: "active" }
    case "paused":
      return { label: "Paused", reason: "Not published. It answers nobody until an owner or admin publishes it.", tone: "blocked" }
    case "service_account_disabled":
      return {
        label: "Service account disabled",
        reason: "Published, but paused automatically: its service account is disabled or gone.",
        tone: "suspended",
      }
    case "needs_sponsor":
      return {
        label: "Needs a sponsor",
        reason: "Published, but paused automatically: nobody accountable sponsors it or its service account.",
        tone: "suspended",
      }
    default:
      // A newer server's reason is shown verbatim rather than guessed at.
      return { label: String(availability), reason: "", tone: "blocked" }
  }
}

export function describeAudience(audience: ApiAssistantAudience): string {
  return audience === "all_users" ? "Every active user of this deployment" : "Members of this space"
}

export function platformName(platform: string): string {
  return platform === "telegram" ? "Telegram" : platform
}

/** One roster row as the editor holds it: the schema is still text. */
export interface RosterDraft {
  /** A stable list key, so removing one entry does not shift another's state. */
  key?: string
  kind: "agent" | "workflow"
  id: string
  /** Agent entries only. */
  schemaText: string
  releasable: string[]
}

/** The editor's working copy of a definition. */
export interface AssistantDraft {
  name: string
  description: string
  instructions: string
  model: string
  audience: ApiAssistantAudience
  roster: RosterDraft[]
  readableFiles: string[]
  /** An existing service account id, or "" to make one named after the Assistant. */
  serviceAccountId: string
}

let rosterKeys = 0

export function newRosterEntry(kind: RosterDraft["kind"]): RosterDraft {
  rosterKeys += 1
  return { key: `new-${rosterKeys}`, kind, id: "", schemaText: "", releasable: [] }
}

export function emptyDraft(): AssistantDraft {
  return {
    name: "",
    description: "",
    instructions: "",
    model: "",
    audience: "space_members",
    roster: [],
    readableFiles: [],
    serviceAccountId: "",
  }
}

export function draftFromAssistant(a: ApiAssistant): AssistantDraft {
  return {
    name: a.name,
    description: a.description,
    instructions: a.instructions,
    model: a.model ?? "",
    audience: a.audience,
    roster: a.roster.map((entry) => ({
      key: `${entry.kind}:${entry.id}`,
      kind: entry.kind,
      id: entry.id,
      schemaText: entry.output_schema !== undefined ? JSON.stringify(entry.output_schema, null, 2) : "",
      releasable: [...entry.releasable],
    })),
    readableFiles: [...a.readable_files],
    serviceAccountId: a.service_account_id,
  }
}

/** A free-text releasable list, for a Workflow whose result shape the Portal cannot read. */
export function parseFieldList(text: string): string[] {
  const out: string[] = []
  for (const part of text.split(",")) {
    const name = part.trim()
    if (name && !out.includes(name)) out.push(name)
  }
  return out
}

/**
 * Builds the request definition from a draft, or names what is wrong. An Agent
 * entry's releasable fields are kept only while its schema still has them, so
 * editing the schema cannot leave a stale field the server would refuse.
 */
export function draftToDefinition(draft: AssistantDraft): { definition: ApiAssistantDefinition } | { error: string } {
  const name = draft.name.trim()
  if (!name) return { error: "An assistant needs a name." }
  const roster: ApiAssistantRosterEntry[] = []
  for (const entry of draft.roster) {
    if (!entry.id) return { error: `Choose the ${entry.kind} for every roster entry.` }
    if (entry.kind === "agent") {
      const parsed = parseOutputSchema(entry.schemaText)
      if ("error" in parsed) return { error: parsed.error }
      roster.push({
        kind: "agent",
        id: entry.id,
        output_schema: parsed.schema,
        releasable: entry.releasable.filter((field) => parsed.properties.includes(field)),
      })
    } else {
      roster.push({ kind: "workflow", id: entry.id, releasable: [...entry.releasable] })
    }
  }
  const definition: ApiAssistantDefinition = {
    name,
    description: draft.description.trim(),
    instructions: draft.instructions.trim(),
    model: draft.model,
    roster,
    readable_files: [...draft.readableFiles],
    audience: draft.audience,
  }
  if (draft.serviceAccountId) definition.service_account_id = draft.serviceAccountId
  return { definition }
}
