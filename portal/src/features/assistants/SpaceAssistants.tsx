import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, getInitials } from "@buildmax/gui"
import type { ApiAssistant, ApiAssistantDefinition, ApiSpaceMember } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
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
  const canManage = isAllowed(manageState) && !isPersonalSpace

  if (isPersonalSpace) {
    return (
      <section className="sec" aria-labelledby="assistants-title">
        <div className="sec__head">
          <div>
            <h2 className="sec__title" id="assistants-title">
              Assistants
            </h2>
            <p className="sec__copy">
              A personal space cannot publish assistants: an assistant runs as a service account, which only a team
              space can have. Switch to a team space to create one.
            </p>
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
      setLoadError(classifyError(err, "Failed to load this space's assistants"))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId])

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
      setCreateError(getErrorMessage(err, "Failed to create the assistant"))
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
            Assistants
          </h2>
          <p className="sec__copy">
            Service front doors this space publishes on its own chat bot. Each one answers a chosen audience, runs only
            the Agents and Workflows on its roster, and works as a service account rather than as you.
          </p>
        </div>
        {canManage && !creating ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            New assistant
          </Button>
        ) : null}
      </div>

      {(state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" || state.kind === "stale") && (
        <Alert
          tone={state.kind === "stale" ? "stale" : state.kind}
          message={state.error.message}
          retry={{ label: "Retry", onClick: () => void load() }}
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
        <p className="page-activity__empty">Loading assistants...</p>
      ) : state.kind === "error" || state.kind === "forbidden" || state.kind === "notFound" ? null : assistants.length === 0 ? (
        <p className="page-activity__empty">
          No assistants yet.{canManage ? " A new one starts paused; publishing it is a separate, confirmed step." : ""}
        </p>
      ) : (
        <ul className="sec-list" aria-label="Assistants">
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
                      {describeAudience(a.audience)} can ask
                      {a.binding ? ` · @${a.binding.bot_handle} on ${platformName(a.binding.platform)}` : " · no bot bound"}
                    </span>
                  </div>
                  <AvailabilityBadge availability={a.availability} />
                </div>
                {a.availability !== "available" ? (
                  <p className="sec-edit__hint">{describeAvailability(a.availability).reason}</p>
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
