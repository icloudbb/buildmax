import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, getInitials } from "@buildmax/gui"
import type { ApiAssistant, ApiAssistantDefinition, ApiSpaceMember } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT } from "../../i18n"
import { buildHash, navigate } from "../../router"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed, type PermissionState } from "../../state/permissionState"
import { createAssistant, listAssistants } from "./api"
import { AssistantEditor } from "./AssistantEditor"
import { AssistantDetail } from "./AssistantDetail"
import { describeAudience, describeAvailability, emptyDraft, platformName } from "./model"
import { AvailabilityBadge } from "./AvailabilityBadge"

/**
 * SpaceAssistants is the Space's service front doors: each an Assistant with
 * its own bot, a bounded roster, readable files, and an audience, published
 * only after its owner confirms what it discloses. Owners and admins manage;
 * members read. See docs/design/space-assistants.md.
 */
export function SpaceAssistants({
  token,
  spaceId,
  assistantId,
  isPersonalSpace,
  members,
  currentUserId,
  manageState,
}: {
  token: string | null
  spaceId: string
  /** The open Assistant, when the route names one. */
  assistantId?: string
  isPersonalSpace: boolean
  members: ApiSpaceMember[]
  currentUserId?: string
  /** Owner-or-admin capability state. */
  manageState: PermissionState
}) {
  const t = useT()
  const canManage = isAllowed(manageState) && !isPersonalSpace

  if (isPersonalSpace) {
    return (
      <section className="sec" aria-labelledby="assistants-title">
        <div className="sec__head">
          <div>
            <h2 className="sec__title" id="assistants-title">
              {t("assistants.title")}
            </h2>
            <p className="sec__copy">{t("assistants.personal")}</p>
          </div>
        </div>
      </section>
    )
  }

  if (assistantId) {
    return (
      <AssistantDetail
        token={token}
        spaceId={spaceId}
        assistantId={assistantId}
        members={members}
        currentUserId={currentUserId}
        canManage={canManage}
      />
    )
  }
  return <AssistantList token={token} spaceId={spaceId} canManage={canManage} />
}

function AssistantList({ token, spaceId, canManage }: { token: string | null; spaceId: string; canManage: boolean }) {
  const t = useT()
  const stableT = useStableT()
  const [data, setData] = useState<ApiAssistant[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<RequestError | null>(null)
  const [creating, setCreating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const initial = useMemo(() => emptyDraft(), [])

  const load = useCallback(async () => {
    if (!token) return
    setLoading(true)
    setLoadError(null)
    try {
      setData(await listAssistants(spaceId, token))
    } catch (err) {
      setLoadError(classifyError(err, stableT("assistants.error.loadList")))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, stableT])

  useEffect(() => {
    void load()
  }, [load])

  const state = useMemo(
    () => deriveResourceState({ loading, data, error: loadError, isEmpty: (d) => d.length === 0 }),
    [loading, data, loadError]
  )

  async function create(definition: ApiAssistantDefinition) {
    if (!token) return
    setSaving(true)
    setCreateError(null)
    try {
      const created = await createAssistant(spaceId, definition, token)
      navigate({ name: "space", spaceId, section: "assistants", assistantId: created.id })
    } catch (err) {
      setCreateError(getErrorMessage(err, t("assistants.error.create")))
    } finally {
      setSaving(false)
    }
  }

  const assistants = data ?? []

  return (
    <section className="sec asst" aria-labelledby="assistants-title">
      <div className="sec__head">
        <div>
          <h2 className="sec__title" id="assistants-title">
            {t("assistants.title")}
          </h2>
          <p className="sec__copy">{t("assistants.intro")}</p>
        </div>
        {canManage && !creating ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            {t("assistants.new")}
          </Button>
        ) : null}
      </div>

      {(state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" || state.kind === "stale") && (
        <Alert
          tone={state.kind === "stale" ? "stale" : state.kind}
          message={state.error.message}
          retry={{ label: t("shell.retry"), onClick: () => void load() }}
        />
      )}

      {creating ? (
        <AssistantEditor
          spaceId={spaceId}
          token={token}
          initial={initial}
          mode="create"
          readOnly={false}
          busy={saving}
          error={createError}
          onSubmit={(definition) => void create(definition)}
          onCancel={() => {
            setCreating(false)
            setCreateError(null)
          }}
        />
      ) : null}

      {state.kind === "loading" ? (
        <p className="page-activity__empty">{t("assistants.loadingList")}</p>
      ) : state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" ? null : assistants.length === 0 ? (
        <p className="page-activity__empty">
          {canManage ? t("assistants.emptyManage") : t("assistants.empty")}
        </p>
      ) : (
        <ul className="sec-list" aria-label={t("assistants.title")}>
          {assistants.map((a) => (
            <li key={a.id}>
              <div className="sec-card" data-testid="assistant">
                <div className="sec-card__head">
                  <span className="sec-card__logo" aria-hidden>
                    {getInitials(a.name)}
                  </span>
                  <div className="sec-card__ident">
                    <a
                      className="sec-card__name asst__link"
                      href={buildHash({ name: "space", spaceId, section: "assistants", assistantId: a.id })}
                    >
                      {a.name}
                    </a>
                    <span className="sec-card__desc">
                      {a.binding
                        ? t("assistants.card.withBot", {
                            audience: describeAudience(a.audience, t),
                            handle: a.binding.bot_handle,
                            platform: platformName(a.binding.platform),
                          })
                        : t("assistants.card.noBot", { audience: describeAudience(a.audience, t) })}
                    </span>
                  </div>
                  <AvailabilityBadge availability={a.availability} />
                </div>
                {a.availability !== "available" ? (
                  <p className="sec-edit__hint">{describeAvailability(a.availability, t).reason}</p>
                ) : null}
                {a.description ? <p className="sec__copy">{a.description}</p> : null}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
