import type { MessageVars, Translate } from "@buildmax/gui"
import type { ApiFailureCause, ApiFailureClass } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

/**
 * What a person should do first about a failed run.
 *
 * - `fixAgent`: open the Agent to fix the Secret grant the server refused.
 * - `openAgent`: open the Agent, whose Space configuration blocked the run.
 * - `openPlugins`: open the Space's plugin settings to activate or resume a plugin.
 * - `askSpaceAdmin`: the fix is a Space owner's or admin's; there is nothing
 *   this person can open to make it.
 * - `askSystemAdmin`: the fix is the deployment's, such as the Space's quota tier.
 * - `runDetails`: read what the run did; the failure was inside it.
 * - `retry`: run it again; the failure was transient or not the person's.
 */
export type FailureAction =
  | "fixAgent"
  | "openAgent"
  | "openPlugins"
  | "askSpaceAdmin"
  | "askSystemAdmin"
  | "runDetails"
  | "retry"

/** What a failed run's surfaces read to explain it. */
export interface RunFailureFacts {
  status: string
  failureClass?: ApiFailureClass | string | null
  failureCause?: ApiFailureCause | null
  agentId?: string | null
  errorMessage?: string | null
  /** When the run ended, which dates a usage quota refusal. */
  endedAt?: string | null
}

/** Who is reading, and the names the page could load for them. */
export interface RunFailureContext {
  agent?: string | null
  /**
   * The Secret's name. Only an owner or admin may read Secret metadata; for
   * anyone else it is absent and the Secret is described, never shown by ID.
   */
  secret?: string | null
  /**
   * An owner or admin, who can change the Agent, its Secret grants, and the
   * Space's plugin activations. Anyone else is told who can.
   */
  canManage: boolean
  /** Now, in milliseconds; injectable for tests. */
  now?: number
}

type Message = [MessageKey, MessageVars?]

export interface RunFailureExplanation {
  title: Message
  /** What went wrong. */
  body: Message
  /** What to do about it, when that depends on who is reading. */
  next?: Message
  primary: FailureAction
  /** True when the first step is a fix, so retry is not the lead. */
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

const SECRET: Record<NonNullable<ApiFailureCause["secret_problem"]>, [MessageKey, MessageKey, MessageKey]> = {
  disabled: ["runs.failure.secret.disabled.title", "runs.failure.secret.disabled.body", "runs.failure.secret.disabled.next"],
  unavailable: ["runs.failure.secret.unavailable.title", "runs.failure.secret.unavailable.body", "runs.failure.secret.unavailable.next"],
  item_missing: ["runs.failure.secret.itemMissing.title", "runs.failure.secret.itemMissing.body", "runs.failure.secret.itemMissing.next"],
}

const PLUGIN: Record<NonNullable<ApiFailureCause["plugin_problem"]>, [MessageKey, MessageKey, MessageKey]> = {
  not_activated: ["runs.failure.plugin.notActivated.title", "runs.failure.plugin.notActivated.body", "runs.failure.plugin.notActivated.next"],
  suspended: ["runs.failure.plugin.suspended.title", "runs.failure.plugin.suspended.body", "runs.failure.plugin.suspended.next"],
}

const DAY_MS = 24 * 60 * 60 * 1000

/**
 * explainRunFailure maps a failed run's class and cause to what the person
 * reads and does first. It is the one place that decides this, so the Task
 * page, the Issue's latest run, and Run details cannot disagree. Null for a
 * run that did not fail.
 */
export function explainRunFailure(facts: RunFailureFacts, context: RunFailureContext): RunFailureExplanation | null {
  if (facts.status.toUpperCase() !== "FAILED") return null
  const raw = facts.errorMessage?.trim() ? facts.errorMessage.trim() : null
  const hasAgent = Boolean(facts.agentId)
  const agent = context.agent ?? ""
  const make = (title: Message, body: Message, primary: FailureAction, next?: Message): RunFailureExplanation => ({
    title,
    body,
    next,
    primary,
    leadsWithFix: primary !== "runDetails" && primary !== "retry",
    raw,
  })
  // A member reads the same problem, then who can fix it instead of how.
  const spaceFix = (next: MessageKey, action: FailureAction): [Message, FailureAction] =>
    context.canManage ? [[next], action] : [["runs.failure.askSpaceAdmin"], "askSpaceAdmin"]

  switch (facts.failureClass) {
    case "space_configuration": {
      const cause = facts.failureCause
      if (cause?.kind === "secret_grant" && cause.secret_problem && hasAgent) {
        const [title, body, next] = SECRET[cause.secret_problem]
        const [nextMessage, action] = spaceFix(next, "fixAgent")
        // An owner or admin can look a Secret up by its ID when its name did
        // not load; anyone else may not read either, so it is described.
        const secretName = context.secret ?? (context.canManage ? cause.secret_id : null) ?? ""
        return make(
          [title],
          [body, { agent, secretName, item: cause.secret_item ?? "" }],
          action,
          nextMessage,
        )
      }
      if (cause?.kind === "plugin" && cause.plugin_problem && cause.plugin_name) {
        const [title, body, next] = PLUGIN[cause.plugin_problem]
        const [nextMessage, action] = spaceFix(next, "openPlugins")
        return make([title], [body, { agent, plugin: cause.plugin_name }], action, nextMessage)
      }
      if (cause?.kind === "usage_quota") return explainQuota(cause, facts.endedAt, context.now ?? Date.now(), make)
      const body: Message = [hasAgent ? "runs.failure.space.body" : "runs.failure.space.bodyNoAgent", { agent }]
      if (!context.canManage) return make(["runs.failure.space.title"], body, "askSpaceAdmin", ["runs.failure.askSpaceAdmin"])
      return make(["runs.failure.space.title"], body, hasAgent ? "openAgent" : "runDetails")
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
 * A spent usage quota is the deployment's to change: a System Administrator
 * assigns the Space's tier, so no Space role can fix it and the explanation
 * says who can. The window rolls, so once a full window has passed since the
 * refusal, everything it counted has aged out and running again is the step.
 */
function explainQuota(
  cause: ApiFailureCause,
  endedAt: string | null | undefined,
  now: number,
  make: (title: Message, body: Message, primary: FailureAction, next?: Message) => RunFailureExplanation,
): RunFailureExplanation {
  const days = cause.quota_period_days ?? 0
  const max = cause.quota_max ?? 0
  const limitKnown = (cause.quota_limit === "runs" || cause.quota_limit === "tokens") && max > 0 && days > 0
  const body: Message = limitKnown
    ? [
        cause.quota_limit === "runs" ? "runs.failure.quota.runs.body" : "runs.failure.quota.tokens.body",
        { count: max, max: max.toLocaleString("en-US"), days },
      ]
    : ["runs.failure.quota.body"]
  const ended = endedAt ? Date.parse(endedAt) : Number.NaN
  if (days > 0 && Number.isFinite(ended) && now - ended >= days * DAY_MS) {
    return make(["runs.failure.quota.title"], body, "retry", ["runs.failure.quota.windowPassed", { days }])
  }
  return make(
    ["runs.failure.quota.title"],
    body,
    "askSystemAdmin",
    days > 0 ? ["runs.failure.quota.next", { days }] : ["runs.failure.quota.nextNoWindow"],
  )
}

/**
 * Renders an explanation's text. An unnamed Agent reads as "This Agent", and a
 * Secret whose name the reader may not see is described rather than shown by
 * ID, so a sentence is whole whatever the page could load.
 */
export function failureText(
  explanation: RunFailureExplanation,
  t: Translate<MessageKey>,
): { title: string; body: string } {
  const [titleKey, titleVars] = explanation.title
  const [bodyKey, bodyVars] = explanation.body
  const vars: MessageVars = { ...bodyVars, agent: bodyVars?.agent || t("runs.failure.thisAgent") }
  if (bodyVars && "secretName" in bodyVars) {
    vars.secret = bodyVars.secretName
      ? t("runs.failure.secret.named", { name: String(bodyVars.secretName) })
      : t("runs.failure.secret.unnamed")
  }
  const sentences = [t(bodyKey, vars)]
  if (explanation.next) sentences.push(t(explanation.next[0], explanation.next[1]))
  return { title: t(titleKey, titleVars), body: sentences.join(t("runs.sentenceSeparator")) }
}
