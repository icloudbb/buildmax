import type { ApiRunProvenance, ApiTaskRun } from "../../lib/api/types"

/** How a run's origin should read to someone asking why it exists. */
export interface OriginDescription {
  /** One sentence naming what put this run in flight. */
  text: string
  /**
   * Whether the run repeats an earlier one. A repeat carries no new
   * instruction, so "nothing was said" is expected rather than missing.
   */
  isRepeat: boolean
  /**
   * How to read the absence of a quoted message.
   *
   * quoted — the message is there to compare against the run input.
   * none-expected — this origin never has one.
   * none-recorded — this origin should have one and does not, because the run
   *   predates the record or its message could not be resolved.
   *
   * The last two are deliberately distinct: an old run with no attribution and
   * an origin that never had any look identical if they are folded together,
   * and only one of them is a gap.
   */
  quote: "quoted" | "none-expected" | "none-recorded"
}

const triggerText: Record<string, string> = {
  task_create: "Created with the task.",
  task_rerun: "Run again with new instructions.",
  task_retry: "A repeat of an earlier run, with the same input.",
  portal_conversation: "Asked for in a conversation.",
  portal_task_create: "Created from the Portal.",
  portal_task_rerun: "Continued from the Portal with a new message.",
  issue_agent_run: "Started by an issue's agent.",
  workflow_step: "Dispatched by a workflow step.",
  webhook: "Started by an inbound webhook.",
}

/** Origins where nobody typed anything, so no message can be quoted. */
const messagelessTriggers = new Set(["task_retry", "workflow_step", "issue_agent_run"])

export function describeOrigin(provenance: ApiRunProvenance): OriginDescription {
  const trigger = provenance.trigger_source ?? ""
  const isRepeat = trigger === "task_retry" || !!provenance.retry_of_task_run_id
  const parts: string[] = [triggerText[trigger] ?? "Started by an unrecorded path."]
  if (provenance.created_by_type === "system") {
    parts.push("The runtime started it, not a person.")
  } else if (provenance.created_by_type === "webhook") {
    parts.push("An external caller started it, not a person.")
  }
  let quote: OriginDescription["quote"] = "quoted"
  if (!provenance.source_message) {
    quote = messagelessTriggers.has(trigger) ? "none-expected" : "none-recorded"
  }
  return { text: parts.join(" "), isRepeat, quote }
}

// Triggers where the input was never typed by the currently signed-in
// person, keyed to a short label naming what actually started the run. A
// trigger absent from this map, with created_by_type "user", is credited to
// "You" -- every direct or Portal-initiated human trigger (task_create,
// task_rerun, task_retry, portal_conversation, portal_task_create,
// portal_task_rerun).
const nonHumanTrigger: Record<string, string> = {
  workflow_step: "Workflow",
  issue_agent_run: "Issue",
  webhook: "Webhook",
}

/**
 * Who to credit for a run's input, from its trigger and creator type.
 *
 * Takes the lighter `ApiTaskRun` shape rather than a fetched `ApiRunProvenance`
 * so the main task thread can label every turn without a per-run round trip.
 * Never returns "You" for a run this person did not start.
 */
export function runInputLabel(run: Pick<ApiTaskRun, "trigger_source" | "created_by_type">): string {
  if (run.trigger_source && nonHumanTrigger[run.trigger_source]) {
    return nonHumanTrigger[run.trigger_source]
  }
  if (run.created_by_type === "system") return "System"
  if (run.created_by_type === "webhook") return "Webhook"
  if (!run.trigger_source && !run.created_by_type) return "Unknown origin"
  return "You"
}

/**
 * Whether the quoted message and the run's instruction are the same text.
 *
 * Worth saying out loud. Equal means Tier 1 passed the request through
 * verbatim; different is the normal case and the reason both are stored.
 */
export function inputMatchesMessage(provenance: ApiRunProvenance): boolean {
  const said = provenance.source_message
  if (!said || said.truncated) return false
  return said.content.trim() === provenance.input.trim()
}

/** How the agent definition behind a run should read. */
export interface AgentDescription {
  text: string
  /**
   * True when the definition has been edited since this run. Worth flagging on
   * its own: reading the agent's page today would show text this run never saw.
   */
  driftedSinceRun: boolean
}

/**
 * Describe which agent definition a run executed under.
 *
 * An agent's instructions are resolved when its worker asks for the run, so
 * editing an agent changes what its next run does. That is intended. What it
 * costs is reproducibility, which the recorded revision buys back — and only if
 * a reader is told plainly when the two numbers disagree.
 */
export function describeAgent(provenance: ApiRunProvenance): AgentDescription | null {
  const agent = provenance.agent
  if (!agent) return null
  const name = agent.name || agent.id
  const ran = agent.revision ?? 0
  const current = agent.current_revision ?? 0
  const suffix = agent.deleted ? " This agent has since been deleted." : ""
  if (ran === 0) {
    return {
      text: `Ran under ${name}. Which revision of it is not recorded.${suffix}`,
      driftedSinceRun: false,
    }
  }
  if (current > ran) {
    return {
      text: `Ran under ${name}, revision ${ran}. The definition has been edited since — it is now revision ${current}.${suffix}`,
      driftedSinceRun: true,
    }
  }
  return { text: `Ran under ${name}, revision ${ran}.${suffix}`, driftedSinceRun: false }
}

/** How the Space-wide instruction layer behind a run should read. */
export interface SpaceInstructionsDescription {
  text: string
  driftedSinceRun: boolean
}

export function describeSpaceInstructions(
  provenance: ApiRunProvenance
): SpaceInstructionsDescription | null {
  const instructions = provenance.space_instructions
  if (!instructions) return null
  const ran = instructions.revision
  const current = instructions.current_revision ?? 0
  if (ran === 0) {
    if (current > 0) {
      return {
        text: `This run used no Space instructions. They are now revision ${current}.`,
        driftedSinceRun: true,
      }
    }
    return {
      text: "No Space instructions were configured for this run.",
      driftedSinceRun: false,
    }
  }
  if (current > ran) {
    return {
      text: `Used Space instructions revision ${ran}. They have since been edited and are now revision ${current}.`,
      driftedSinceRun: true,
    }
  }
  return {
    text: `Used Space instructions revision ${ran}.`,
    driftedSinceRun: false,
  }
}
