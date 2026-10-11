import { ButtonLink } from "@buildmax/gui"
import { useT, type MessageKey } from "../../i18n"
import { buildHash } from "../../router"
import { failureText, type FailureAction, type RunFailureExplanation } from "./failure"

/**
 * A failed run explained in the person's terms: what went wrong and what to do,
 * with the server's own text kept under a disclosure for whoever needs it.
 * Actions are the caller's, because each surface already owns its action row.
 */
export function RunFailureNotice({ explanation }: { explanation: RunFailureExplanation }) {
  const t = useT()
  const text = failureText(explanation, t)
  return (
    <div className="run-failure" data-testid="run-failure">
      <p className="run-failure__title">{text.title}</p>
      <p className="run-failure__body">{text.body}</p>
      {explanation.raw ? (
        <details className="run-failure__raw">
          <summary>{t("runs.failure.serverMessage")}</summary>
          <pre>{explanation.raw}</pre>
        </details>
      ) : null}
    </div>
  )
}

/**
 * The fix a failure leads with, as the surface's primary action: the Agent
 * whose configuration blocked the run, or the Space's plugin settings. Nothing
 * when the first step is not a fix, or is one only someone else can make --
 * the explanation then says who.
 */
export function FailureFixLink({
  explanation,
  spaceId,
  agentId,
  size,
  variant = "primary",
  onNavigate,
}: {
  explanation: RunFailureExplanation | null
  spaceId: string
  agentId?: string | null
  size?: "compact"
  /** Secondary where the surface's primary action belongs to something else. */
  variant?: "primary" | "secondary"
  onNavigate?: () => void
}) {
  const t = useT()
  if (!explanation) return null
  const link = failureFixLink(explanation.primary, spaceId, agentId)
  if (!link) return null
  return (
    <ButtonLink variant={variant} size={size} href={link.href} onClick={onNavigate}>
      {t(link.label)}
    </ButtonLink>
  )
}

/** The link a failure's first step opens, or null when there is none to open. */
export function failureFixLink(action: FailureAction, spaceId: string, agentId?: string | null): { href: string; label: MessageKey } | null {
  switch (action) {
    case "fixAgent":
      return agentId ? { href: buildHash({ name: "agent", spaceId, agentId }), label: "runs.failure.fixAgent" } : null
    case "openAgent":
      return agentId ? { href: buildHash({ name: "agent", spaceId, agentId }), label: "runs.failure.openAgent" } : null
    case "openPlugins":
      return { href: buildHash({ name: "space", spaceId, section: "plugins" }), label: "runs.failure.openPlugins" }
    default:
      return null
  }
}
