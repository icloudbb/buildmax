import { BaseModal, Button } from "@buildmax/gui"
import type { ApiAssistantStatement } from "../../lib/api/types"
import { describeAudience } from "./model"

/**
 * The structured half of a publish statement: who can ask, what it can read,
 * and what it can run with which Secrets. The server's sentence form is shown
 * beside it, so an owner can check one against the other.
 */
export function StatementSummary({ statement }: { statement: ApiAssistantStatement }) {
  return (
    <div className="asst-statement">
      <p className="asst-statement__text">{statement.text}</p>
      <dl className="asst-statement__facts">
        <div>
          <dt>Who can ask</dt>
          <dd>{describeAudience(statement.audience)}</dd>
        </div>
        <div>
          <dt>Files it can read</dt>
          <dd>
            {statement.readable_files.length === 0 ? (
              "None"
            ) : (
              <ul>
                {statement.readable_files.map((f) => (
                  <li key={f.id}>{f.name}</li>
                ))}
              </ul>
            )}
          </dd>
        </div>
        <div>
          <dt>Agents it can run</dt>
          <dd>
            {statement.agents.length === 0 ? (
              "None"
            ) : (
              <ul>
                {statement.agents.map((a) => (
                  <li key={a.id}>
                    {a.name}
                    {a.secrets.length > 0 ? <span className="asst-statement__secrets"> — holds Secrets {a.secrets.join(", ")}</span> : null}
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </div>
        <div>
          <dt>Workflows it can run</dt>
          <dd>
            {statement.workflows.length === 0 ? (
              "None"
            ) : (
              <ul>
                {statement.workflows.map((w) => (
                  <li key={w.id}>
                    {w.name}
                    {w.agents.length > 0 ? (
                      <span className="asst-statement__secrets">
                        {" "}
                        — steps run as{" "}
                        {w.agents
                          .map((a) => (a.secrets.length > 0 ? `${a.name} (Secrets ${a.secrets.join(", ")})` : a.name))
                          .join(", ")}
                      </span>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </div>
      </dl>
    </div>
  )
}

/**
 * Publishing is a disclosure decision (docs/design/space-assistants.md §8):
 * before an Assistant goes live, or widens while live, its owner reads what it
 * discloses and confirms exactly that statement.
 */
export function StatementDialog({
  open,
  statement,
  confirmLabel,
  busy,
  error,
  notice,
  onCancel,
  onConfirm,
}: {
  open: boolean
  statement: ApiAssistantStatement | null
  confirmLabel: string
  busy: boolean
  error: string | null
  /** Shown above the statement, e.g. when it changed since the page loaded. */
  notice?: string | null
  onCancel: () => void
  onConfirm: () => void
}) {
  if (!statement) return null
  return (
    <BaseModal
      open={open}
      title="Confirm what this assistant discloses"
      titleId="assistant-statement-title"
      className="modal--large"
      onClose={() => {
        if (!busy) onCancel()
      }}
    >
      <div className="modal__body">
        <div className="sec-callout" role="note">
          <span>
            Publishing is a disclosure decision. Everything this assistant can read or run is disclosed to everyone who
            can ask it, however the question is phrased. Confirm only if that is acceptable.
          </span>
        </div>
        {notice ? (
          <p className="sec__error" role="alert">
            {notice}
          </p>
        ) : null}
        <StatementSummary statement={statement} />
        {error ? (
          <p className="modal__error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="modal__actions">
          <Button variant="secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" busy={busy} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </BaseModal>
  )
}
