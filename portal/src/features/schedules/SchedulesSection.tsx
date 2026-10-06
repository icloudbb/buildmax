import { useCallback, useEffect, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiAssistant, ApiSchedule, ApiScheduleDelivery, ApiTask, ApiWorkflowRun } from "../../lib/api/types"
import { navigate } from "../../router"
import { Alert } from "../../components/state/Alert"
import { getErrorMessage } from "../../lib/errorMessage"
import { apiTaskToTask } from "../../lib/api/mappers"
import { runStatusLabel, runStatusTone, taskStatusLabel } from "../conversations/thread"
import { listAssistants } from "../assistants"
import { CreateScheduleForm } from "./CreateScheduleForm"
import { describeDelivery } from "./delivery"
import { useStableT, useT } from "../../i18n"
import { useRelativeTime, useTimestamp } from "../../lib/dateFormat"
import {
  deleteSchedule,
  listScheduleDeliveries,
  listScheduleRuns,
  listScheduleTasks,
  listSchedules,
  updateSchedule,
} from "./api"

interface SchedulesSectionProps {
  token: string
  spaceId: string
  // What these schedules fire. The section is embedded on the executor's own
  // detail page, so the executor is pinned rather than chosen.
  executorKind: "agent" | "workflow"
  executorId: string
  // The executor's display name and, for a workflow, its definition JSON so the
  // create form can render the input its input_schema declares.
  executorName?: string
  workflowDefinition?: string
  canManage: boolean
  // Owners and admins may send results to a person through an Assistant.
  canDeliver?: boolean
}

export function SchedulesSection({
  token,
  spaceId,
  executorKind,
  executorId,
  executorName,
  workflowDefinition,
  canManage,
  canDeliver,
}: SchedulesSectionProps) {
  const t = useT()
  const stableT = useStableT()
  // null distinguishes "not yet fetched" from an executor with no schedules.
  const [schedules, setSchedules] = useState<ApiSchedule[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [assistants, setAssistants] = useState<ApiAssistant[]>([])

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await listSchedules(spaceId, token)
      // The list is space-wide; this section shows only this executor's schedules.
      setSchedules(res.schedules.filter((s) => s.executor_kind === executorKind && s.executor_id === executorId))
    } catch (err) {
      setError(getErrorMessage(err, stableT("schedules.error.load")))
    }
  }, [spaceId, executorKind, executorId, token, stableT])

  useEffect(() => {
    void load()
  }, [load])

  // Names the assistant a delivering schedule sends through; without it the
  // card says "an assistant".
  const delivers = schedules?.some((s) => s.delivery) ?? false
  useEffect(() => {
    if (!delivers) return
    let cancelled = false
    listAssistants(spaceId, token)
      .then((list) => {
        if (!cancelled) setAssistants(list)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [delivers, spaceId, token])

  if (error && schedules === null) {
    return (
      <Alert tone="error" message={error} retry={{ label: t("schedules.section.retry"), onClick: () => void load() }} />
    )
  }

  return (
    <div className="agent-schedules">
      <p className="page-activity__subtitle">
        {executorKind === "workflow" ? t("schedules.section.workflowIntro") : t("schedules.section.agentIntro")}
      </p>
      {error ? <Alert tone="stale" message={error} retry={{ label: t("schedules.section.retry"), onClick: () => void load() }} /> : null}

      {canManage ? (
        creating ? (
          <CreateScheduleForm
            token={token}
            spaceId={spaceId}
            pinned={{ kind: executorKind, id: executorId, name: executorName ?? executorId, definition: workflowDefinition }}
            canDeliver={canDeliver}
            onCreated={async () => {
              setCreating(false)
              await load()
            }}
            onCancel={() => setCreating(false)}
          />
        ) : (
          <Button variant="primary" onClick={() => setCreating(true)}>
            {t("schedules.new")}
          </Button>
        )
      ) : null}

      {schedules === null ? (
        <p className="page-activity__empty">{t("shell.loading")}</p>
      ) : schedules.length === 0 ? (
        <p className="page-activity__empty">{t("schedules.section.empty")}</p>
      ) : (
        <ul className="agent-schedules__list">
          {schedules.map((s) => (
            <ScheduleCard
              key={s.id}
              schedule={s}
              spaceId={spaceId}
              token={token}
              canManage={canManage}
              assistantName={assistants.find((a) => a.id === s.delivery?.assistant_id)?.name}
              onChanged={load}
            />
          ))}
        </ul>
      )}
    </div>
  )
}

function ScheduleCard({
  schedule,
  spaceId,
  token,
  canManage,
  assistantName,
  onChanged,
}: {
  schedule: ApiSchedule
  spaceId: string
  token: string
  canManage: boolean
  assistantName?: string
  onChanged: () => Promise<void>
}) {
  const t = useT()
  const formatWhen = useTimestamp()
  const relativeTime = useRelativeTime()
  const isWorkflow =schedule.executor_kind === "workflow"
  const [busyAction, setBusyAction] = useState<"toggle" | "delete" | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [tasks, setTasks] = useState<ApiTask[] | null>(null)
  const [runs, setRuns] = useState<ApiWorkflowRun[] | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyLoading, setHistoryLoading] = useState(false)
  // Keyed by the task or run each firing started.
  const [deliveries, setDeliveries] = useState<Record<string, ApiScheduleDelivery> | null>(null)

  async function toggleEnabled() {
    setBusyAction("toggle")
    setErr(null)
    try {
      await updateSchedule(spaceId, schedule.id, { enabled: !schedule.enabled }, token)
      await onChanged()
    } catch (e) {
      setErr(getErrorMessage(e, t("schedules.error.update")))
    } finally {
      setBusyAction(null)
    }
  }

  async function remove() {
    const confirmKey = isWorkflow ? "schedules.card.confirmDeleteRuns" : "schedules.card.confirmDeleteTasks"
    if (!window.confirm(t(confirmKey, { name: schedule.name || schedule.cron_expr }))) return
    setBusyAction("delete")
    setErr(null)
    try {
      await deleteSchedule(spaceId, schedule.id, token)
      await onChanged()
    } catch (e) {
      setErr(getErrorMessage(e, t("schedules.error.delete")))
      setBusyAction(null)
    }
  }

  async function loadHistory() {
    setHistoryLoading(true)
    setErr(null)
    try {
      if (schedule.delivery) {
        const res = await listScheduleDeliveries(spaceId, schedule.id, token)
        setDeliveries(Object.fromEntries(res.deliveries.map((d) => [d.fire_ref, d])))
      }
      if (isWorkflow) {
        const res = await listScheduleRuns(spaceId, schedule.id, token)
        setRuns(res.runs)
      } else {
        const res = await listScheduleTasks(spaceId, schedule.id, token)
        setTasks(res.tasks)
      }
    } catch (e) {
      setErr(getErrorMessage(e, t("schedules.error.history")))
    } finally {
      setHistoryLoading(false)
    }
  }

  function toggleHistory() {
    const next = !historyOpen
    setHistoryOpen(next)
    if (next && (isWorkflow ? runs === null : tasks === null)) void loadHistory()
  }

  const loaded = isWorkflow ? runs !== null : tasks !== null
  const empty = isWorkflow ? runs?.length === 0 : tasks?.length === 0

  return (
    <li className="agent-schedules__card">
      <div className="agent-schedules__card-head">
        <span className="agent-schedules__name">{schedule.name || t("schedules.card.unnamed")}</span>
        <span
          className={
            schedule.enabled
              ? "agent-schedules__badge agent-schedules__badge--on"
              : "agent-schedules__badge agent-schedules__badge--off"
          }
        >
          {schedule.enabled ? t("schedules.enabled") : t("schedules.paused")}
        </span>
        {schedule.consecutive_failures > 0 ? (
          <span className="agent-schedules__badge agent-schedules__badge--warn">
            {t("schedules.card.consecutiveFailures", { count: schedule.consecutive_failures })}
          </span>
        ) : null}
      </div>

      <dl className="agent-schedules__meta">
        <div>
          <dt>{t("schedules.card.schedule")}</dt>
          <dd><code>{schedule.cron_expr}</code> · {schedule.timezone}</dd>
        </div>
        <div>
          <dt>{t("schedules.card.nextFire")}</dt>
          <dd>{schedule.enabled ? formatWhen(schedule.next_fire_at) : "—"}</dd>
        </div>
        <div>
          <dt>{t("schedules.card.lastFire")}</dt>
          <dd>{formatWhen(schedule.last_fire_at)}</dd>
        </div>
        {schedule.delivery ? (
          <div>
            <dt>{t("schedules.card.results")}</dt>
            <dd>{assistantName ? t("schedules.card.sentThrough", { assistant: assistantName }) : t("schedules.card.sentThroughAny")}</dd>
          </div>
        ) : null}
      </dl>

      <p className="agent-schedules__prompt">{schedule.input || (isWorkflow ? t("schedules.card.noInput") : "")}</p>

      {err ? <p className="agent-schedules__error" role="alert">{err}</p> : null}

      <div className="agent-schedules__card-actions">
        <Button variant="tertiary" size="compact" onClick={toggleHistory}>
          {historyOpen
            ? isWorkflow ? t("schedules.card.hideRuns") : t("schedules.card.hideTasks")
            : isWorkflow ? t("schedules.card.showRuns") : t("schedules.card.showTasks")}
        </Button>
        {canManage ? (
          <>
            <Button variant="secondary" size="compact" busy={busyAction === "toggle"} onClick={() => void toggleEnabled()} disabled={busyAction !== null}>
              {schedule.enabled ? t("schedules.card.disable") : t("schedules.card.enable")}
            </Button>
            <Button variant="danger" size="compact" busy={busyAction === "delete"} onClick={() => void remove()} disabled={busyAction !== null}>
              {t("schedules.card.delete")}
            </Button>
          </>
        ) : null}
      </div>

      {historyOpen ? (
        historyLoading ? (
          <p className="page-activity__empty">{t("shell.loading")}</p>
        ) : !loaded ? (
          <Button variant="secondary" size="compact" onClick={() => void loadHistory()}>
            {isWorkflow ? t("schedules.card.retryRuns") : t("schedules.card.retryTasks")}
          </Button>
        ) : empty ? (
          <p className="page-activity__empty">{t("schedules.card.notFired")}</p>
        ) : isWorkflow ? (
          <table className="agent-runs">
            <thead>
              <tr>
                <th>{t("schedules.history.run")}</th>
                <th>{t("schedules.history.status")}</th>
                <th>{t("schedules.history.when")}</th>
                {deliveries ? <th>{t("schedules.history.delivery")}</th> : null}
              </tr>
            </thead>
            <tbody>
              {(runs ?? []).map((r) => (
                <tr
                  key={r.id}
                  tabIndex={0}
                  onClick={() => navigate({ name: "workflowRun", spaceId, workflowRunId: r.id })}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") navigate({ name: "workflowRun", spaceId, workflowRunId: r.id })
                  }}
                >
                  <td className="agent-runs__title">{r.id}</td>
                  <td>
                    <span className={`agent-runs__status agent-runs__status--${runStatusTone(r.status)}`}>
                      {runStatusLabel(r.status, t)}
                    </span>
                  </td>
                  <td className="agent-runs__when">{formatWhen(r.created_at)}</td>
                  {deliveries ? <td>{describeDelivery(deliveries[r.id], t)}</td> : null}
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <table className="agent-runs">
            <thead>
              <tr>
                <th>{t("schedules.history.task")}</th>
                <th>{t("schedules.history.status")}</th>
                <th>{t("schedules.history.when")}</th>
                {deliveries ? <th>{t("schedules.history.delivery")}</th> : null}
              </tr>
            </thead>
            <tbody>
              {(tasks ?? []).map((task) => {
                const ui = apiTaskToTask(task)
                return (
                  <tr
                    key={task.id}
                    tabIndex={0}
                    onClick={() => navigate({ name: "task", spaceId, taskId: task.id })}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") navigate({ name: "task", spaceId, taskId: task.id })
                    }}
                  >
                    <td className="agent-runs__title">{ui.title}</td>
                    <td>
                      <span className={`agent-runs__status agent-runs__status--${runStatusTone(task.status)}`}>
                        {taskStatusLabel(task, t)}
                      </span>
                    </td>
                    <td className="agent-runs__when">{relativeTime(ui.timeAt)}</td>
                    {deliveries ? <td>{describeDelivery(deliveries[task.id], t)}</td> : null}
                  </tr>
                )
              })}
            </tbody>
          </table>
        )
      ) : null}
    </li>
  )
}
