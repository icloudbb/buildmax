import { useCallback, useEffect, useId, useState, type ReactNode } from "react"
import { useStatusLabel } from "@buildmax/gui"
import { buildHash } from "../../router"
import { useStableT, useT, type MessageKey } from "../../i18n"
import { useRelativeTime } from "../../lib/dateFormat"
import type { Agent, Task, Workflow } from "../../lib/types"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, type RequestError } from "../../state/resourceState"
import { explainRunFailure, failureText, useCanManageSpace } from "../runs"
import { getNeedsMe } from "./api"
import { requestSummary, toNeedsMe, waitingCount, type AttentionIssue, type NeedsMe, type NeedsMeGroup } from "./model"

interface NeedsMeSectionProps {
  token: string | null
  spaceId: string
  /** The Space's Agents and Workflows, for naming runs and requests. */
  agents: Agent[]
  workflows: Workflow[]
  /** The immediate next action when nothing is waiting. */
  onCreateIssue: () => void
}

const REASON: Record<AttentionIssue["reason"], MessageKey> = {
  run_failed: "needsMe.reason.runFailed",
  awaiting_answer: "needsMe.reason.awaitingAnswer",
  workflow_request: "needsMe.reason.workflowRequest",
}

/**
 * The work in this Space that is waiting on the signed-in person, at the top
 * of the page they land on. Each item opens where it is resolved. What counts
 * is the server's one definition (GET /needs-me); this only presents it.
 */
export function NeedsMeSection({ token, spaceId, agents, workflows, onCreateIssue }: NeedsMeSectionProps) {
  const t = useT()
  const stableT = useStableT()
  const relativeTime = useRelativeTime()
  const statusLabel = useStatusLabel()
  const canManage = useCanManageSpace()
  const headingId = useId()
  // null until the first answer: loading never reads as "nothing waiting".
  const [needs, setNeeds] = useState<NeedsMe | null>(null)
  const [error, setError] = useState<RequestError | null>(null)

  const load = useCallback(() => {
    if (!token || !spaceId) return
    setError(null)
    getNeedsMe(spaceId, token)
      .then((res) => setNeeds(toNeedsMe(res)))
      .catch((err) => setError(classifyError(err, stableT("needsMe.error"))))
  }, [token, spaceId, stableT])

  useEffect(() => {
    setNeeds(null)
    load()
  }, [load])

  const agentName = (task: Task) => agents.find((agent) => agent.id === task.agentId)?.name ?? t("issues.agent")
  const failureTitle = (task: Task) => {
    const explanation = explainRunFailure(
      {
        status: "FAILED",
        failureClass: task.failureClass,
        failureCause: task.failureCause,
        agentId: task.agentId,
        errorMessage: task.errorMessage,
        endedAt: task.timeAt,
      },
      { agent: agents.find((agent) => agent.id === task.agentId)?.name, canManage },
    )
    return explanation ? failureText(explanation, t).title : t("needsMe.failed.fallback")
  }

  const count = needs ? waitingCount(needs) : null

  return (
    <section className="needs-me" aria-labelledby={headingId}>
      <div className="needs-me__head">
        <h2 id={headingId} className="needs-me__title">
          {t("needsMe.title")}
        </h2>
        {count != null && count > 0 ? <span className="page-activity__meta">{t("needsMe.count", { count })}</span> : null}
      </div>

      {error ? (
        <Alert
          tone={error.kind}
          message={error.message}
          retry={error.kind === "forbidden" ? undefined : { label: t("shell.retry"), onClick: load }}
        />
      ) : needs === null ? (
        <p className="page-activity__empty">{t("shell.loading")}</p>
      ) : count === 0 ? (
        <EmptyState
          message={t("needsMe.empty")}
          action={{ label: t("needsMe.empty.action"), onClick: onCreateIssue, variant: "secondary" }}
        />
      ) : (
        <div className="needs-me__groups">
          <NeedsMeGroupList
            title={t("needsMe.issues.title")}
            group={needs.issues}
            keyOf={(item) => item.issue.id}
            render={(item) => (
              <NeedsMeItem
                href={buildHash({ name: "issue", spaceId, issueId: item.issue.id })}
                title={item.issue.title}
                detail={t(REASON[item.reason])}
                action={t("needsMe.open.issue")}
                time={relativeTime(item.issue.updatedAt)}
              />
            )}
          />
          <NeedsMeGroupList
            title={t("needsMe.awaiting.title")}
            group={needs.awaiting}
            keyOf={(task) => task.id}
            render={(task) => (
              <NeedsMeItem
                href={buildHash({ name: "task", spaceId, taskId: task.id })}
                title={task.title}
                detail={`${agentName(task)} · ${statusLabel("needs_answer")}`}
                action={t("needsMe.open.answer")}
                time={relativeTime(task.timeAt)}
              />
            )}
          />
          <NeedsMeGroupList
            title={t("needsMe.requests.title")}
            group={needs.requests}
            keyOf={(request) => request.id}
            render={(request) => (
              <NeedsMeItem
                href={buildHash({ name: "workflowRun", spaceId, workflowRunId: request.workflowRunId })}
                title={workflows.find((workflow) => workflow.id === request.workflowId)?.name ?? t("issues.workflow")}
                detail={
                  requestSummary(request) ||
                  t(request.kind === "question" ? "needsMe.request.question" : "needsMe.request.input", { step: request.nodeId })
                }
                action={t("needsMe.open.respond")}
                time={relativeTime(request.createdAt)}
              />
            )}
          />
          <NeedsMeGroupList
            title={t("needsMe.failed.title")}
            group={needs.failed}
            keyOf={(task) => task.id}
            render={(task) => (
              <NeedsMeItem
                href={buildHash({ name: "task", spaceId, taskId: task.id })}
                title={task.title}
                detail={`${agentName(task)} · ${failureTitle(task)}`}
                action={t("needsMe.open.run")}
                time={relativeTime(task.timeAt)}
              />
            )}
          />
        </div>
      )}
    </section>
  )
}

function NeedsMeGroupList<T>({
  title,
  group,
  keyOf,
  render,
}: {
  title: string
  group: NeedsMeGroup<T>
  keyOf: (item: T) => string
  render: (item: T) => ReactNode
}) {
  const t = useT()
  const headingId = useId()
  if (group.total === 0) return null
  return (
    <div className="needs-me__group" role="group" aria-labelledby={headingId}>
      <h3 id={headingId} className="needs-me__group-title">
        {title} <span className="page-activity__meta">{group.total}</span>
      </h3>
      <ul className="needs-me__list">
        {group.items.map((item) => (
          <li key={keyOf(item)}>{render(item)}</li>
        ))}
      </ul>
      {group.total > group.items.length ? (
        <p className="page-activity__meta">{t("needsMe.more", { shown: group.items.length, total: group.total })}</p>
      ) : null}
    </div>
  )
}

function NeedsMeItem({ href, title, detail, action, time }: { href: string; title: string; detail: string; action: string; time: string }) {
  return (
    <a className="needs-me__item" href={href}>
      <span className="needs-me__item-main">
        <span className="needs-me__item-title">{title}</span>
        <span className="needs-me__item-detail">{detail}</span>
      </span>
      <span className="needs-me__item-side">
        <span className="needs-me__item-action">{action}</span>
        <span className="page-activity__meta">{time}</span>
      </span>
    </a>
  )
}
