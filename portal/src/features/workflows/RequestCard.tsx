import { useState } from "react"
import { Button, QuestionForm } from "@buildmax/gui"
import type { WorkflowRequest } from "../../lib/types"
import { useStableT, useT } from "../../i18n"
import { WorkflowRunInputForm } from "./RunInputForm"
import { buildInputValue, type InputFormValues } from "./runInput"
import { answerMode, formatQuestionAnswers, scalarValue } from "./request"

export type RequestResponse = { action: "answer"; response: unknown } | { action: "decline"; reason?: string }

interface RequestCardProps {
  request: WorkflowRequest
  /** Sends the response; a rejection's message is shown on the card. */
  onRespond: (response: RequestResponse) => Promise<void>
  /** Binds the question form's keyboard shortcuts; enable it on one card only. */
  keys?: boolean
}

/**
 * A pending request a workflow run waits on, with the controls to answer it:
 * a form chosen by the request's response schema, or the Agent's questions.
 * Declining stops the step, and with it the run.
 */
export function WorkflowRequestCard({ request, onRespond, keys = false }: RequestCardProps) {
  const t = useT()
  const stableT = useStableT()
  const mode = answerMode(request.responseSchema)
  const [text, setText] = useState("")
  const [values, setValues] = useState<InputFormValues>({})
  const [declining, setDeclining] = useState(false)
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function send(response: RequestResponse) {
    setBusy(true)
    setError(null)
    try {
      await onRespond(response)
    } catch (err) {
      setError(err instanceof Error ? err.message : stableT("workflows.request.sendFailed"))
    } finally {
      setBusy(false)
    }
  }

  function submitAnswer() {
    switch (mode.kind) {
      case "text":
        if (!text.trim()) return setError(t("workflows.request.enterAnswer"))
        return void send({ action: "answer", response: text.trim() })
      case "scalar": {
        const { value, error: problem } = scalarValue(mode.type, text, t)
        if (problem) return setError(problem)
        return void send({ action: "answer", response: value })
      }
      case "fields": {
        const built = buildInputValue(mode.fields, values, t)
        if (built.errors.length > 0) return setError(built.errors.join(" "))
        return void send({ action: "answer", response: built.value })
      }
      case "json":
        try {
          return void send({ action: "answer", response: JSON.parse(text) })
        } catch {
          return setError(t("workflows.request.enterJson"))
        }
    }
  }

  const expires = request.expiresAt ? new Date(request.expiresAt).toLocaleString() : null

  return (
    <section className="workflow-request" aria-label={t("workflows.request.label", { step: request.nodeId })}>
      <div className="workflow-request__head">
        <strong>{request.kind === "question"
            ? t("workflows.request.asks", { step: request.nodeId })
            : t("workflows.request.needsInput", { step: request.nodeId })}</strong>
        {expires ? <span className="page-activity__meta">{t("workflows.request.expires", { time: expires })}</span> : null}
      </div>
      {request.prompt ? <p className="workflow-request__prompt">{request.prompt}</p> : null}

      {request.kind === "question" ? (
        <QuestionForm
          questions={request.questions}
          keys={keys}
          title={t("workflows.request.answerToContinue")}
          onAnswer={(answer) =>
            "declined" in answer
              ? void send({ action: "decline", reason: "The questions were dismissed." })
              : void send({ action: "answer", response: formatQuestionAnswers(request.questions, answer.answers) })
          }
        />
      ) : mode.kind === "boolean" ? (
        <div className="workflow-request__actions">
          <Button busy={busy} disabled={busy} onClick={() => void send({ action: "answer", response: true })}>
            {t("workflows.request.yes")}
          </Button>
          <Button variant="secondary" disabled={busy} onClick={() => void send({ action: "answer", response: false })}>
            {t("workflows.request.no")}
          </Button>
        </div>
      ) : (
        <>
          {mode.kind === "fields" ? (
            <WorkflowRunInputForm
              title={t("workflows.request.yourAnswer")}
              fields={mode.fields}
              values={values}
              disabled={busy}
              onChange={(name, value) => setValues((prev) => ({ ...prev, [name]: value }))}
            />
          ) : (
            <label className="issues-page__field">
              <span className="issues-page__field-label">{mode.kind === "json" ? t("workflows.request.yourAnswerJson") : t("workflows.request.yourAnswer")}</span>
              {mode.kind === "scalar" && mode.type !== "string" ? (
                <input className="issues-page__input" type="number" value={text} disabled={busy} onChange={(e) => setText(e.target.value)} />
              ) : (
                <textarea className="issues-page__textarea" rows={3} value={text} disabled={busy} onChange={(e) => setText(e.target.value)} />
              )}
            </label>
          )}
          <div className="workflow-request__actions">
            <Button busy={busy} disabled={busy} onClick={submitAnswer}>
              {t("workflows.request.submit")}
            </Button>
          </div>
        </>
      )}

      {request.kind !== "question" ? (
        declining ? (
          <div className="workflow-request__decline">
            <label className="issues-page__field">
              <span className="issues-page__field-label">{t("workflows.request.whyDecline")}</span>
              <input className="issues-page__input" value={reason} disabled={busy} onChange={(e) => setReason(e.target.value)} />
            </label>
            <div className="workflow-request__actions">
              <Button variant="danger" disabled={busy} onClick={() => void send({ action: "decline", reason })}>
                {t("workflows.request.declineAndStop")}
              </Button>
              <Button variant="tertiary" disabled={busy} onClick={() => setDeclining(false)}>
                {t("workflows.request.keepOpen")}
              </Button>
            </div>
          </div>
        ) : (
          <Button variant="tertiary" size="compact" disabled={busy} onClick={() => setDeclining(true)}>
            {t("workflows.request.decline")}
          </Button>
        )
      ) : null}
      {error ? (
        <p className="modal__error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  )
}
