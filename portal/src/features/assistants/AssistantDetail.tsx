import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, useLocale } from "@buildmax/gui"
import type { ApiAssistant, ApiAssistantDefinition, ApiAssistantStatement, ApiSpaceMember } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { intlLocale } from "../../lib/dateFormat"
import { useStableT, useT, type MessageKey } from "../../i18n"
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
  const t = useT()
  const stableT = useStableT()
  const { locale } = useLocale()
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
      setLoadError(classifyError(err, stableT("assistants.error.load")))
    }
  }, [token, spaceId, assistantId, stableT])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (assistant) setEntityLabel(assistant.id, assistant.name)
  }, [assistant, setEntityLabel])

  // Keyed by revision: a saved edit resets the form to what the server holds.
  const initialDraft = useMemo(() => (assistant ? draftFromAssistant(assistant) : null), [assistant])

  const nameOf = (userId?: string) => {
    if (!userId) return t("assistants.nobody")
    if (userId === currentUserId) return t("assistants.you")
    const member = members.find((m) => m.user_id === userId)
    return member?.user_name || member?.user_email || userId
  }

  /**
   * Runs a change that may need a confirmed statement. A refusal carrying one
   * opens the dialog; confirming resends with its digest.
   */
  async function attempt(
    what: string,
    failure: MessageKey,
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
        onError(getErrorMessage(err, t(failure)))
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
          notice: t("assistants.detail.statementChanged"),
        })
      } else {
        setConfirmError(getErrorMessage(err, t("assistants.error.publish")))
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
          {t("assistants.assistant")}
        </h2>
        <Alert tone={loadError.kind} message={loadError.message} retry={{ label: t("shell.retry"), onClick: () => void load() }} />
      </section>
    )
  }
  if (!assistant || !initialDraft) {
    return (
      <section className="sec asst" aria-label={t("assistants.assistant")}>
        <BackLink spaceId={spaceId} />
        <p className="page-activity__empty">{t("assistants.loading")}</p>
      </section>
    )
  }

  const a = assistant
  const availability = describeAvailability(a.availability, t)
  const authToken = token ?? ""
  const isSponsor = a.sponsor_user_id === currentUserId

  const publish = () => {
    // The statement already on the page is what the owner confirms; the server
    // refuses if it changed since, and the dialog then shows the new one.
    setConfirmError(null)
    setPending({
      statement: a.statement,
      confirmLabel: t("assistants.detail.publish"),
      notice: null,
      run: (digest) => setAssistantState(spaceId, a.id, "active", authToken, digest),
    })
  }

  const save = (definition: ApiAssistantDefinition) =>
    void attempt(
      "save",
      "assistants.error.save",
      (digest) =>
        updateAssistant(spaceId, a.id, { definition, ...(digest ? { confirm_statement: digest } : {}) }, authToken),
      t("assistants.detail.saveKeepPublished"),
      (m) => setSaveError(m || null)
    )

  const act = (what: string, failure: MessageKey, run: () => Promise<ApiAssistant>) =>
    void attempt(what, failure, run, t("assistants.detail.confirm"), (m) => setActionError(m || null))

  async function remove() {
    if (!window.confirm(t("assistants.detail.deleteConfirm", { name: a.name }))) return
    setBusy("delete")
    setActionError(null)
    try {
      await deleteAssistant(spaceId, a.id, authToken)
      navigate({ name: "space", spaceId, section: "assistants" })
    } catch (err) {
      setActionError(getErrorMessage(err, t("assistants.error.delete")))
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
            {t("assistants.detail.summary", {
              revision: a.revision,
              audience: describeAudience(a.audience, t),
              sponsor: nameOf(a.sponsor_user_id),
            })}
          </p>
        </div>
        <AvailabilityBadge availability={a.availability} />
      </div>
      <p className="sec__copy" data-testid="assistant-availability-reason">
        {availability.reason}
      </p>

      {canManage ? (
        <div className="sec-card__actions" role="group" aria-label={t("assistants.detail.actions")}>
          {a.state === "active" ? (
            <Button
              busy={busy === "pause"}
              onClick={() =>
                act("pause", "assistants.error.pause", () => setAssistantState(spaceId, a.id, "paused", authToken))
              }
            >
              {t("assistants.detail.pause")}
            </Button>
          ) : (
            <Button variant="primary" disabled={busy !== null} onClick={publish}>
              {t("assistants.detail.publish")}
            </Button>
          )}
          {!isSponsor || a.availability === "needs_sponsor" ? (
            <Button
              busy={busy === "take sponsorship"}
              onClick={() =>
                act("take sponsorship", "assistants.error.takeSponsorship", () =>
                  updateAssistant(spaceId, a.id, { sponsor_user_id: currentUserId }, authToken)
                )
              }
            >
              {t("assistants.detail.takeSponsorship")}
            </Button>
          ) : null}
          <Button variant="danger" className="sec-card__destroy" busy={busy === "delete"} onClick={() => void remove()}>
            {t("assistants.detail.delete")}
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
          {t("assistants.detail.discloses")}
        </h3>
        <StatementSummary statement={a.statement} />
      </section>

      <section className="sec-card" aria-labelledby="assistant-bot-heading">
        <h3 className="sec-form__title" id="assistant-bot-heading">
          {t("assistants.detail.chatBot")}
        </h3>
        {a.binding ? (
          <>
            <p className="sec__copy">
              {t("assistants.detail.bound", {
                handle: a.binding.bot_handle,
                platform: platformName(a.binding.platform),
                date: new Date(a.binding.created_at).toLocaleDateString(intlLocale(locale)),
              })}
            </p>
            {canManage ? (
              <div className="sec-card__actions">
                <Button
                  busy={busy === "unbind"}
                  onClick={() =>
                    act("unbind", "assistants.error.unbind", () => unbindAssistant(spaceId, a.id, authToken))
                  }
                >
                  {t("assistants.detail.unbind")}
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
              act("bind", "assistants.error.bind", async () => {
                const next = await bindAssistant(spaceId, a.id, { platform: "telegram", token: botToken.trim() }, authToken)
                setBotToken("")
                return next
              })
            }}
          >
            <p className="sec__copy">{t("assistants.detail.bindHint")}</p>
            <label className="modal__label" htmlFor="assistant-bot-token">
              {t("assistants.detail.botToken")}
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
                {t("assistants.detail.bind")}
              </Button>
            </div>
          </form>
        ) : (
          <p className="sec__copy">{t("assistants.detail.noBot")}</p>
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
        confirmLabel={pending?.confirmLabel ?? t("assistants.detail.confirm")}
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
  const t = useT()
  return (
    <a className="asst__back" href={buildHash({ name: "space", spaceId, section: "assistants" })}>
      {t("assistants.back")}
    </a>
  )
}
