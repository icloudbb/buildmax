import { useState } from "react"
import { Button, QuestionForm } from "@buildmax/gui"
import type { WorkflowRequest } from "../../lib/types"
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
      setError(err instanceof Error ? err.message : "Could not send the response.")
    } finally {
      setBusy(false)
    }
  }

  function submitAnswer() {
    switch (mode.kind) {
      case "text":
        if (!text.trim()) return setError("Enter an answer.")
        return void send({ action: "answer", response: text.trim() })
      case "scalar": {
        const { value, error: problem } = scalarValue(mode.type, text)
        if (problem) return setError(problem)
        return void send({ action: "answer", response: value })
      }
      case "fields": {
        const built = buildInputValue(mode.fields, values)
        if (built.errors.length > 0) return setError(built.errors.join(" "))
        return void send({ action: "answer", response: built.value })
      }
      case "json":
        try {
          return void send({ action: "answer", response: JSON.parse(text) })
        } catch {
          return setError("Enter valid JSON.")
        }
    }
  }

  const expires = request.expiresAt ? new Date(request.expiresAt).toLocaleString() : null

  return (
    <section className="workflow-request" aria-label={`Request from step ${request.nodeId}`}>
      <div className="workflow-request__head">
        <strong>{request.kind === "question" ? `Step ${request.nodeId} asks` : `Step ${request.nodeId} needs your input`}</strong>
        {expires ? <span className="page-activity__meta">Expires {expires}</span> : null}
      </div>
      {request.prompt ? <p className="workflow-request__prompt">{request.prompt}</p> : null}

      {request.kind === "question" ? (
        <QuestionForm
          questions={request.questions}
          keys={keys}
          title="Answer to continue the step"
          onAnswer={(answer) =>
            "declined" in answer
              ? void send({ action: "decline", reason: "The questions were dismissed." })
              : void send({ action: "answer", response: formatQuestionAnswers(request.questions, answer.answers) })
          }
        />
      ) : mode.kind === "boolean" ? (
        <div className="workflow-request__actions">
          <Button busy={busy} disabled={busy} onClick={() => void send({ action: "answer", response: true })}>
            Yes
          </Button>
          <Button variant="secondary" disabled={busy} onClick={() => void send({ action: "answer", response: false })}>
            No
          </Button>
        </div>
      ) : (
        <>
          {mode.kind === "fields" ? (
            <WorkflowRunInputForm
              title="Your answer"
              fields={mode.fields}
              values={values}
              disabled={busy}
              onChange={(name, value) => setValues((prev) => ({ ...prev, [name]: value }))}
            />
          ) : (
            <label className="issues-page__field">
              <span className="issues-page__field-label">{mode.kind === "json" ? "Your answer (JSON)" : "Your answer"}</span>
              {mode.kind === "scalar" && mode.type !== "string" ? (
                <input className="issues-page__input" type="number" value={text} disabled={busy} onChange={(e) => setText(e.target.value)} />
              ) : (
                <textarea className="issues-page__textarea" rows={3} value={text} disabled={busy} onChange={(e) => setText(e.target.value)} />
              )}
            </label>
          )}
          <div className="workflow-request__actions">
            <Button busy={busy} disabled={busy} onClick={submitAnswer}>
              Submit answer
            </Button>
          </div>
        </>
      )}

      {request.kind !== "question" ? (
        declining ? (
          <div className="workflow-request__decline">
            <label className="issues-page__field">
              <span className="issues-page__field-label">Why decline? The step fails with this reason.</span>
              <input className="issues-page__input" value={reason} disabled={busy} onChange={(e) => setReason(e.target.value)} />
            </label>
            <div className="workflow-request__actions">
              <Button variant="danger" disabled={busy} onClick={() => void send({ action: "decline", reason })}>
                Decline and stop the run
              </Button>
              <Button variant="tertiary" disabled={busy} onClick={() => setDeclining(false)}>
                Keep open
              </Button>
            </div>
          </div>
        ) : (
          <Button variant="tertiary" size="compact" disabled={busy} onClick={() => setDeclining(true)}>
            Decline…
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
