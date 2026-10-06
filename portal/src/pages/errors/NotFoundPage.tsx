import { Button } from "@buildmax/gui"
import { navigate } from "../../router"
import { useSpace } from "../../contexts/SpaceContext"
import { useT } from "../../i18n"

/**
 * The address itself matched no route -- a typo, a stale bookmark from
 * before a rename, or a route name the shell has no case for. Rendered
 * instead of silently falling through to Chat (see
 * docs/design/portal-navigation-and-space-context.md), with the one safe
 * action every reader has regardless of what they typed: back to their
 * current Space.
 */
export function NotFoundPage() {
  const t = useT()
  const { currentSpaceId } = useSpace()

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{t("common.notFound.title")}</h1>
          <p className="page-activity__subtitle">{t("common.notFound.body")}</p>
        </div>
      </div>
      <div className="page-activity__actions">
        <Button
          variant="primary"
          onClick={() => currentSpaceId && navigate({ name: "chat", spaceId: currentSpaceId })}
        >
          {t("common.notFound.back")}
        </Button>
      </div>
    </div>
  )
}
