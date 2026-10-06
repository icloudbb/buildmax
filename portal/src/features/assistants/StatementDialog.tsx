import { BaseModal, Button } from "@buildmax/gui"
import type { ApiAssistantStatement } from "../../lib/api/types"
import { useT } from "../../i18n"
import { describeAudience } from "./model"

/**
 * The structured half of a publish statement: who can ask, what it can read,
 * and what it can run with which Secrets. The server's sentence form is shown
 * beside it, so an owner can check one against the other.
 */
export function StatementSummary({ statement }: { statement: ApiAssistantStatement }) {
  const t = useT()
  const list = (names: string[]) => names.join(t("assistants.listSeparator"))
  return (
    <div className="asst-statement">
      <p className="asst-statement__text">{statement.text}</p>
      <dl className="asst-statement__facts">
        <div>
          <dt>{t("assistants.statement.whoCanAsk")}</dt>
          <dd>{describeAudience(statement.audience, t)}</dd>
        </div>
        <div>
          <dt>{t("assistants.statement.files")}</dt>
          <dd>
            {statement.readable_files.length === 0 ? (
              t("assistants.statement.none")
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
          <dt>{t("assistants.statement.agents")}</dt>
          <dd>
            {statement.agents.length === 0 ? (
              t("assistants.statement.none")
            ) : (
              <ul>
                {statement.agents.map((a) => (
                  <li key={a.id}>
                    {a.name}
                    {a.secrets.length > 0 ? (
                      <span className="asst-statement__secrets">
                        {t("assistants.statement.holdsSecrets", { secrets: list(a.secrets) })}
                      </span>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </div>
        <div>
          <dt>{t("assistants.statement.workflows")}</dt>
          <dd>
            {statement.workflows.length === 0 ? (
              t("assistants.statement.none")
            ) : (
              <ul>
                {statement.workflows.map((w) => (
                  <li key={w.id}>
                    {w.name}
                    {w.agents.length > 0 ? (
                      <span className="asst-statement__secrets">
                        {t("assistants.statement.stepsRunAs", {
                          agents: list(
                            w.agents.map((a) =>
                              a.secrets.length > 0
                                ? t("assistants.statement.agentWithSecrets", { name: a.name, secrets: list(a.secrets) })
                                : a.name
                            )
                          ),
                        })}
                      </span>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </div>
        {statement.agents.length > 0 || statement.workflows.length > 0 ? (
          <div>
            <dt>{t("assistants.statement.spaceFiles")}</dt>
            <dd>
              {statement.space_files_total === 0 ? (
                t("assistants.statement.noneYet")
              ) : (
                <ul>
                  {statement.space_files.map((name) => (
                    <li key={name}>{name}</li>
                  ))}
                  {statement.space_files_total > statement.space_files.length ? (
                    <li>{t("assistants.statement.more", { count: statement.space_files_total - statement.space_files.length })}</li>
                  ) : null}
                </ul>
              )}
            </dd>
          </div>
        ) : null}
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
  const t = useT()
  if (!statement) return null
  return (
    <BaseModal
      open={open}
      title={t("assistants.statement.title")}
      titleId="assistant-statement-title"
      className="modal--large"
      onClose={() => {
        if (!busy) onCancel()
      }}
    >
      <div className="modal__body">
        <div className="sec-callout" role="note">
          <span>{t("assistants.statement.warning")}</span>
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
            {t("assistants.statement.cancel")}
          </Button>
          <Button variant="primary" busy={busy} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </BaseModal>
  )
}
