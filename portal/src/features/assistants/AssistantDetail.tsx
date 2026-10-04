import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiAssistant, ApiAssistantDefinition, ApiAssistantStatement, ApiSpaceMember } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { buildHash, navigate } from "../../router"
import { useApp } from "../../contexts/AppContext"
import { Alert } from "../../components/state/Alert"
import { classifyError, type RequestError } from "../../state/resourceState"
import {
  StatementRequiredError,
  bindAssistant,
  deleteAssistant,
  getAssistant,
  setAssistantState,
  unbindAssistant,
  updateAssistant,
} from "./api"
import { AssistantEditor } from "./AssistantEditor"
import { AvailabilityBadge } from "./AvailabilityBadge"
import { StatementDialog, StatementSummary } from "./StatementDialog"
import { describeAudience, describeAvailability, draftFromAssistant, platformName } from "./model"

/** A change waiting on the owner's confirmation of what it discloses. */
interface PendingConfirmation {
  statement: ApiAssistantStatement
  confirmLabel: string
  notice: string | null
  run: (digest: string) => Promise<ApiAssistant>
}

export function AssistantDetail({
  token,
  spaceId,
  assistantId,
  members,
  currentUserId,
  canManage,
}: {
  token: string | null
  spaceId: string
  assistantId: string
  members: ApiSpaceMember[]
  currentUserId?: string
  canManage: boolean
}) {
  const { setEntityLabel } = useApp()
  const [assistant, setAssistant] = useState<ApiAssistant | null>(null)
  const [loadError, setLoadError] = useState<RequestError | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [pending, setPending] = useState<PendingConfirmation | null>(null)
  const [confirmError, setConfirmError] = useState<string | null>(null)
  const [botToken, setBotToken] = useState("")

  const load = useCallback(async () => {
    if (!token) return
    setLoadError(null)
    try {
      setAssistant(await getAssistant(spaceId, assistantId, token))
    } catch (err) {
      setLoadError(classifyError(err, "Failed to load the assistant"))
    }
  }, [token, spaceId, assistantId])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (assistant) setEntityLabel(assistant.id, assistant.name)
  }, [assistant, setEntityLabel])

  // Keyed by revision: a saved edit resets the form to what the server holds.
  const initialDraft = useMemo(() => (assistant ? draftFromAssistant(assistant) : null), [assistant])

  const nameOf = (userId?: string) => {
    if (!userId) return "nobody"
    if (userId === currentUserId) return "you"
    const member = members.find((m) => m.user_id === userId)
    return member?.user_name || member?.user_email || userId
  }

  /**
   * Runs a change that may need a confirmed statement. A refusal carrying one
   * opens the dialog; confirming resends with its digest.
   */
  async function attempt(
    what: string,
    run: (digest?: string) => Promise<ApiAssistant>,
    confirmLabel: string,
    onError: (message: string) => void
  ) {
    setBusy(what)
    onError("")
    try {
      setAssistant(await run())
    } catch (err) {
      if (err instanceof StatementRequiredError) {
        setConfirmError(null)
        setPending({ statement: err.statement, confirmLabel, notice: null, run: (digest) => run(digest) })
      } else {
        onError(getErrorMessage(err, `Failed to ${what}`))
      }
    } finally {
      setBusy(null)
    }
  }

  async function confirm() {
    if (!pending) return
    setBusy("confirm")
    setConfirmError(null)
    try {
      setAssistant(await pending.run(pending.statement.digest))
      setPending(null)
    } catch (err) {
      if (err instanceof StatementRequiredError) {
        // What it discloses changed between showing and confirming; show the new one.
        setPending({
          ...pending,
          statement: err.statement,
          notice: "What this assistant discloses changed since this statement was shown. Review it again.",
        })
      } else {
        setConfirmError(getErrorMessage(err, "Failed to publish"))
      }
    } finally {
      setBusy(null)
    }
  }

  if (loadError) {
    return (
      <section className="sec asst" aria-labelledby="assistant-title">
        <BackLink spaceId={spaceId} />
        <h2 className="sec__title" id="assistant-title">
          Assistant
        </h2>
        <Alert tone={loadError.kind} message={loadError.message} retry={{ label: "Retry", onClick: () => void load() }} />
      </section>
    )
  }
  if (!assistant || !initialDraft) {
    return (
      <section className="sec asst" aria-label="Assistant">
        <BackLink spaceId={spaceId} />
        <p className="page-activity__empty">Loading assistant...</p>
      </section>
    )
  }

  const a = assistant
  const availability = describeAvailability(a.availability)
  const t = token ?? ""
  const isSponsor = a.sponsor_user_id === currentUserId

  const publish = () => {
    // The statement already on the page is what the owner confirms; the server
    // refuses if it changed since, and the dialog then shows the new one.
    setConfirmError(null)
    setPending({
      statement: a.statement,
      confirmLabel: "Publish",
      notice: null,
      run: (digest) => setAssistantState(spaceId, a.id, "active", t, digest),
    })
  }

  const save = (definition: ApiAssistantDefinition) =>
    void attempt(
      "save",
      (digest) => updateAssistant(spaceId, a.id, { definition, ...(digest ? { confirm_statement: digest } : {}) }, t),
      "Save and keep published",
      (m) => setSaveError(m || null)
    )

  const act = (what: string, run: () => Promise<ApiAssistant>) =>
    void attempt(what, run, "Confirm", (m) => setActionError(m || null))

  async function remove() {
    if (!window.confirm(`Delete the assistant "${a.name}"? Its bot stops answering. Its service account stays.`)) return
    setBusy("delete")
    setActionError(null)
    try {
      await deleteAssistant(spaceId, a.id, t)
      navigate({ name: "space", spaceId, section: "assistants" })
    } catch (err) {
      setActionError(getErrorMessage(err, "Failed to delete the assistant"))
      setBusy(null)
    }
  }

  return (
    <section className="sec asst" aria-labelledby="assistant-title">
      <BackLink spaceId={spaceId} />
      <div className="sec__head">
        <div>
          <h2 className="sec__title" id="assistant-title">
            {a.name}
          </h2>
          <p className="sec__copy">
            Revision {a.revision} · {describeAudience(a.audience)} can ask · Sponsored by {nameOf(a.sponsor_user_id)}
          </p>
        </div>
        <AvailabilityBadge availability={a.availability} />
      </div>
      <p className="sec__copy" data-testid="assistant-availability-reason">
        {availability.reason}
      </p>

      {canManage ? (
        <div className="sec-card__actions" role="group" aria-label="Assistant actions">
          {a.state === "active" ? (
            <Button busy={busy === "pause"} onClick={() => act("pause", () => setAssistantState(spaceId, a.id, "paused", t))}>
              Pause
            </Button>
          ) : (
            <Button variant="primary" disabled={busy !== null} onClick={publish}>
              Publish
            </Button>
          )}
          {!isSponsor || a.availability === "needs_sponsor" ? (
            <Button
              busy={busy === "take sponsorship"}
              onClick={() =>
                act("take sponsorship", () => updateAssistant(spaceId, a.id, { sponsor_user_id: currentUserId }, t))
              }
            >
              Take sponsorship
            </Button>
          ) : null}
          <Button variant="danger" className="sec-card__destroy" busy={busy === "delete"} onClick={() => void remove()}>
            Delete
          </Button>
        </div>
      ) : null}
      {actionError ? (
        <p className="sec__error" role="alert">
          {actionError}
        </p>
      ) : null}

      <section className="sec-card" aria-labelledby="assistant-statement-heading">
        <h3 className="sec-form__title" id="assistant-statement-heading">
          What publishing discloses
        </h3>
        <StatementSummary statement={a.statement} />
      </section>

      <section className="sec-card" aria-labelledby="assistant-bot-heading">
        <h3 className="sec-form__title" id="assistant-bot-heading">
          Chat bot
        </h3>
        {a.binding ? (
          <>
            <p className="sec__copy">
              {a.binding.bot_handle} on {platformName(a.binding.platform)}, bound{" "}
              {new Date(a.binding.created_at).toLocaleDateString()}. Whoever holds its token can also read its messages
              through {platformName(a.binding.platform)}.
            </p>
            {canManage ? (
              <div className="sec-card__actions">
                <Button busy={busy === "unbind"} onClick={() => act("unbind", () => unbindAssistant(spaceId, a.id, t))}>
                  Unbind bot
                </Button>
              </div>
            ) : null}
          </>
        ) : canManage ? (
          <form
            className="sec-field"
            onSubmit={(e) => {
              e.preventDefault()
              if (!botToken.trim()) return
              act("bind", async () => {
                const next = await bindAssistant(spaceId, a.id, { platform: "telegram", token: botToken.trim() }, t)
                setBotToken("")
                return next
              })
            }}
          >
            <p className="sec__copy">
              Give it its own Telegram bot: create one with @BotFather and paste its token. The token is checked with
              Telegram, stored sealed, and never shown again.
            </p>
            <label className="modal__label" htmlFor="assistant-bot-token">
              Telegram bot token
            </label>
            <div className="sec-item">
              <input
                id="assistant-bot-token"
                type="password"
                autoComplete="off"
                className="modal__input sec-item__val"
                value={botToken}
                onChange={(e) => setBotToken(e.target.value)}
              />
              <Button type="submit" variant="primary" busy={busy === "bind"} disabled={!botToken.trim()}>
                Bind bot
              </Button>
            </div>
          </form>
        ) : (
          <p className="sec__copy">No bot is bound.</p>
        )}
      </section>

      <AssistantEditor
        key={a.revision}
        spaceId={spaceId}
        token={token}
        initial={initialDraft}
        mode="edit"
        readOnly={!canManage}
        busy={busy === "save"}
        error={saveError}
        onSubmit={save}
      />

      <StatementDialog
        open={pending !== null}
        statement={pending?.statement ?? null}
        confirmLabel={pending?.confirmLabel ?? "Confirm"}
        notice={pending?.notice}
        busy={busy === "confirm"}
        error={confirmError}
        onCancel={() => setPending(null)}
        onConfirm={() => void confirm()}
      />
    </section>
  )
}

function BackLink({ spaceId }: { spaceId: string }) {
  return (
    <a className="asst__back" href={buildHash({ name: "space", spaceId, section: "assistants" })}>
      ← All assistants
    </a>
  )
}
