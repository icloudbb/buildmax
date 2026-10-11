import { ButtonLink } from "@buildmax/gui"
import { useT } from "../../i18n"
import { buildHash } from "../../router"
import { failureText, type RunFailureExplanation } from "./failure"

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
 * whose configuration blocked the run. Nothing when the failure's first step
 * is not a fix.
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
  if (!explanation?.leadsWithFix || !agentId) return null
  return (
    <ButtonLink
      variant={variant}
      size={size}
      href={buildHash({ name: "agent", spaceId, agentId })}
      onClick={onNavigate}
    >
      {t(explanation.primary === "fixAgent" ? "runs.failure.fixAgent" : "runs.failure.openAgent")}
    </ButtonLink>
  )
}
