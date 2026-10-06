import { Button } from "@buildmax/gui"
import type { ApiTask } from "../../../lib/api/types"
import { useT } from "../../../i18n"
import { taskRunFailed, taskRunFinished, taskStatusLabel } from "../thread"

const previewMaxLen = 600

interface TaskCardProps {
  task: ApiTask
  /** True while this card's own stop or retry is in flight. */
  busy: boolean
  onStop: (taskId: string) => void
  onRetry: (taskId: string) => void
  onOpenTrace: (taskRunId: string) => void
  onOpenIssue?: (issueId: string) => void
  error?: string | null
}

function statusTone(status: string): "running" | "failed" | "done" {
  if (!taskRunFinished(status)) return "running"
  return taskRunFailed(status) ? "failed" : "done"
}

function preview(output: string | null | undefined): string | null {
  if (!output) return null
  const trimmed = output.trim()
  if (trimmed === "") return null
  return trimmed.length > previewMaxLen ? `${trimmed.slice(0, previewMaxLen)}\n…` : trimmed
}

/**
 * One background task, projected into the conversation.
 *
 * It is a card and not a message on purpose: what it shows keeps changing after
 * it first appears, and it was written by a runtime rather than said by anyone.
 * It stands on its own — status, what the run produced, and the way back to the
 * run's files and trace — so a summary that never arrives costs a sentence, not
 * the result.
 */
export function TaskCard({
  task,
  busy,
  onStop,
  onRetry,
  onOpenTrace,
  onOpenIssue,
  error,
}: TaskCardProps) {
  const t = useT()
  const tone = statusTone(task.status)
  const finished = taskRunFinished(task.status)
  const body = preview(task.output)

  return (
    <article className={`task-card task-card--${tone}`}>
      <header className="task-card__head">
        <span className={`task-card__status task-card__status--${tone}`}>{taskStatusLabel(task, t)}</span>
        <span className="task-card__title">{task.title || task.input}</span>
      </header>
      {task.error_message ? <p className="task-card__error">{task.error_message}</p> : null}
      {body ? <pre className="task-card__preview">{body}</pre> : null}
      {!body && !task.error_message && finished ? (
        <p className="task-card__meta">{t("chat.card.noOutput")}</p>
      ) : null}
      {error ? <p className="task-card__error">{error}</p> : null}
      <footer className="task-card__actions">
        {!finished ? (
          <Button variant="danger" size="compact" busy={busy} onClick={() => onStop(task.id)}>{t("chat.card.stop")}</Button>
        ) : (
          <Button variant="secondary" size="compact" busy={busy} onClick={() => onRetry(task.id)}>{t("chat.card.runAgain")}</Button>
        )}
        {task.last_run_id ? (
          <Button variant="tertiary" size="compact" onClick={() => onOpenTrace(task.last_run_id!)}>
            {t("chat.card.runDetails")}
          </Button>
        ) : null}
        {task.issue_id && onOpenIssue ? (
          <Button variant="tertiary" size="compact" onClick={() => onOpenIssue(task.issue_id!)}>
            {t("chat.card.openIssue")}
          </Button>
        ) : null}
      </footer>
    </article>
  )
}
