import { Button } from "@buildmax/gui"

export type ResourceUnavailableKind = "notFound" | "forbidden" | "error"

interface ResourceUnavailableProps {
  /** The resource family's noun, e.g. "Agent", "Workflow", "Issue". */
  resourceLabel: string
  kind: ResourceUnavailableKind
  /** The caught message, shown only for kind === "error". */
  errorMessage?: string | null
  onRetry: () => void
  backLabel: string
  onBack: () => void
}

/**
 * The shared shell for a detail page's load failure, extending the pattern
 * ArtifactDetail already used inline for its own single case. Distinguishes
 * not-found from forbidden from a transient error, per
 * docs/design/portal-navigation-and-space-context.md's "a missing Space,
 * missing resource, and forbidden resource have distinct page states" --
 * confirmed against the server for Agent, Workflow, Workflow Run, Task, and
 * Issue: unlike Artifact (which the API deliberately conflates, so its own
 * page still reads a single vague message), each of these returns a genuine,
 * distinguishable 404 for "no such resource" versus 403 for "no access to
 * its Space." A 403 does not say whether the Space itself doesn't exist or
 * the reader simply isn't a member -- that half stays deliberately vague,
 * the same privacy reason Artifact's page states already document.
 */
export function ResourceUnavailable({
  resourceLabel,
  kind,
  errorMessage,
  onRetry,
  backLabel,
  onBack,
}: ResourceUnavailableProps) {
  const subtitle =
    kind === "notFound"
      ? `No ${resourceLabel.toLowerCase()} with this reference exists. It may have been deleted, or the link may be wrong.`
      : kind === "forbidden"
        ? `This ${resourceLabel.toLowerCase()} belongs to a Space you don't have access to. The Space may not exist, or you may not be a member.`
        : `This ${resourceLabel.toLowerCase()} could not be loaded.`

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{resourceLabel}</h1>
          <p className="page-activity__subtitle">{subtitle}</p>
        </div>
      </div>
      {kind === "error" && errorMessage ? (
        <p className="settings-section__error" role="alert">
          {errorMessage}
        </p>
      ) : null}
      <div className="page-activity__actions">
        {kind === "error" ? (
          <Button variant="primary" onClick={onRetry}>
            Try again
          </Button>
        ) : null}
        <Button variant="secondary" onClick={onBack}>
          {backLabel}
        </Button>
      </div>
    </div>
  )
}
