import type { MessageVars, Translate } from "@buildmax/gui"
import type { ApiFailureCause, ApiFailureClass } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

/**
 * What a person should do first about a failed run.
 *
 * - `fixAgent`: open the Agent to fix the Secret grant the server refused.
 * - `openAgent`: open the Agent, whose Space configuration blocked the run.
 * - `runDetails`: read what the run did; the failure was inside it.
 * - `retry`: run it again; the failure was transient or not the person's.
 */
export type FailureAction = "fixAgent" | "openAgent" | "runDetails" | "retry"

/** What a failed run's surfaces read to explain it. */
export interface RunFailureFacts {
  status: string
  failureClass?: ApiFailureClass | string | null
  failureCause?: ApiFailureCause | null
  agentId?: string | null
  errorMessage?: string | null
}

/** Names the explanation can use when the page knows them. */
export interface RunFailureNames {
  agent?: string | null
  /** Only an owner or admin may read Secret names; others see its id. */
  secret?: string | null
}

type Message = [MessageKey, MessageVars?]

export interface RunFailureExplanation {
  title: Message
  body: Message
  primary: FailureAction
  /** True when the primary action is a fix, so retry is not the lead. */
  leadsWithFix: boolean
  /** The server's own text, kept for the details disclosure. */
  raw: string | null
}

const PLATFORM: Partial<Record<string, [MessageKey, MessageKey]>> = {
  dispatch: ["runs.failure.dispatch.title", "runs.failure.dispatch.body"],
  worker_lost: ["runs.failure.workerLost.title", "runs.failure.workerLost.body"],
  abandoned: ["runs.failure.abandoned.title", "runs.failure.abandoned.body"],
  interrupted: ["runs.failure.interrupted.title", "runs.failure.interrupted.body"],
  infrastructure: ["runs.failure.infrastructure.title", "runs.failure.infrastructure.body"],
}

const SECRET: Record<NonNullable<ApiFailureCause["secret_problem"]>, [MessageKey, MessageKey]> = {
  disabled: ["runs.failure.secret.disabled.title", "runs.failure.secret.disabled.body"],
  unavailable: ["runs.failure.secret.unavailable.title", "runs.failure.secret.unavailable.body"],
  item_missing: ["runs.failure.secret.itemMissing.title", "runs.failure.secret.itemMissing.body"],
}

/**
 * explainRunFailure maps a failed run's class and cause to what the person
 * reads and does first. It is the one place that decides this, so the Task
 * page, the Issue's latest run, and Run details cannot disagree. Null for a
 * run that did not fail.
 */
export function explainRunFailure(facts: RunFailureFacts, names: RunFailureNames = {}): RunFailureExplanation | null {
  if (facts.status.toUpperCase() !== "FAILED") return null
  const raw = facts.errorMessage?.trim() ? facts.errorMessage.trim() : null
  const hasAgent = Boolean(facts.agentId)
  const agentVar = names.agent ?? null
  const make = (title: Message, body: Message, primary: FailureAction): RunFailureExplanation => ({
    title,
    body,
    primary,
    leadsWithFix: primary === "fixAgent" || primary === "openAgent",
    raw,
  })

  switch (facts.failureClass) {
    case "space_configuration": {
      const cause = facts.failureCause
      if (cause?.kind === "secret_grant" && cause.secret_problem && hasAgent) {
        const [title, body] = SECRET[cause.secret_problem]
        const secret = names.secret ?? cause.secret_id ?? ""
        return make(
          [title],
          [body, { agent: agentVar ?? "", secret, item: cause.secret_item ?? "" }],
          "fixAgent",
        )
      }
      return make(
        ["runs.failure.space.title"],
        [hasAgent ? "runs.failure.space.body" : "runs.failure.space.bodyNoAgent", { agent: agentVar ?? "" }],
        hasAgent ? "openAgent" : "runDetails",
      )
    }
    case "model":
      return make(["runs.failure.model.title"], ["runs.failure.model.body"], "retry")
    case "run":
      return make(["runs.failure.run.title"], ["runs.failure.run.body"], "runDetails")
    default: {
      const platform = facts.failureClass ? PLATFORM[facts.failureClass] : undefined
      if (platform) return make([platform[0]], [platform[1]], "retry")
      return make(["runs.failure.unknown.title"], ["runs.failure.unknown.body"], "retry")
    }
  }
}

/**
 * Renders an explanation's text. An unnamed Agent reads as "This Agent", so a
 * sentence is whole on a page that has not loaded the Agent's name.
 */
export function failureText(
  explanation: RunFailureExplanation,
  t: Translate<MessageKey>,
): { title: string; body: string } {
  const [titleKey, titleVars] = explanation.title
  const [bodyKey, bodyVars] = explanation.body
  const vars = { ...bodyVars, agent: bodyVars?.agent || t("runs.failure.thisAgent") }
  return { title: t(titleKey, titleVars), body: t(bodyKey, vars) }
}
