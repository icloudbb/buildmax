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

function formatWhen(iso: string | null | undefined): string {
  if (!iso) return "—"
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString()
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
      setError(getErrorMessage(err, "Failed to load schedules"))
    }
  }, [spaceId, executorKind, executorId, token])

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
      <Alert tone="error" message={error} retry={{ label: "Retry schedules", onClick: () => void load() }} />
    )
  }

  const noun = executorKind === "workflow" ? "workflow" : "agent"
  const fires = executorKind === "workflow" ? "starts a workflow run" : "starts a Task"

  return (
    <div className="agent-schedules">
      <p className="page-activity__subtitle">
        A schedule runs this {noun} automatically on a cron timetable. Each firing {fires}.
      </p>
      {error ? <Alert tone="stale" message={error} retry={{ label: "Retry schedules", onClick: () => void load() }} /> : null}

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
            New schedule
          </Button>
        )
      ) : null}

      {schedules === null ? (
        <p className="page-activity__empty">Loading…</p>
      ) : schedules.length === 0 ? (
        <p className="page-activity__empty">No schedules yet.</p>
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
  const isWorkflow = schedule.executor_kind === "workflow"
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
      setErr(getErrorMessage(e, "Failed to update schedule"))
    } finally {
      setBusyAction(null)
    }
  }

  async function remove() {
    const kept = isWorkflow ? "Runs it already started are kept." : "Tasks it already created are kept."
    if (!window.confirm(`Delete schedule "${schedule.name || schedule.cron_expr}"? ${kept}`)) return
    setBusyAction("delete")
    setErr(null)
    try {
      await deleteSchedule(spaceId, schedule.id, token)
      await onChanged()
    } catch (e) {
      setErr(getErrorMessage(e, "Failed to delete schedule"))
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
      setErr(getErrorMessage(e, "Failed to load firing history"))
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
        <span className="agent-schedules__name">{schedule.name || "(unnamed schedule)"}</span>
        <span
          className={
            schedule.enabled
              ? "agent-schedules__badge agent-schedules__badge--on"
              : "agent-schedules__badge agent-schedules__badge--off"
          }
        >
          {schedule.enabled ? "Enabled" : "Paused"}
        </span>
        {schedule.consecutive_failures > 0 ? (
          <span className="agent-schedules__badge agent-schedules__badge--warn">
            {schedule.consecutive_failures} consecutive failure{schedule.consecutive_failures === 1 ? "" : "s"}
          </span>
        ) : null}
      </div>

      <dl className="agent-schedules__meta">
        <div>
          <dt>Schedule</dt>
          <dd><code>{schedule.cron_expr}</code> · {schedule.timezone}</dd>
        </div>
        <div>
          <dt>Next fire</dt>
          <dd>{schedule.enabled ? formatWhen(schedule.next_fire_at) : "—"}</dd>
        </div>
        <div>
          <dt>Last fire</dt>
          <dd>{formatWhen(schedule.last_fire_at)}</dd>
        </div>
        {schedule.delivery ? (
          <div>
            <dt>Results</dt>
            <dd>Sent to one person through {assistantName ?? "an assistant"}</dd>
          </div>
        ) : null}
      </dl>

      <p className="agent-schedules__prompt">{schedule.input || (isWorkflow ? "(no run input)" : "")}</p>

      {err ? <p className="agent-schedules__error" role="alert">{err}</p> : null}

      <div className="agent-schedules__card-actions">
        <Button variant="tertiary" size="compact" onClick={toggleHistory}>
          {historyOpen
            ? isWorkflow ? "Hide triggered runs" : "Hide triggered tasks"
            : isWorkflow ? "Show triggered runs" : "Show triggered tasks"}
        </Button>
        {canManage ? (
          <>
            <Button variant="secondary" size="compact" busy={busyAction === "toggle"} onClick={() => void toggleEnabled()} disabled={busyAction !== null}>
              {schedule.enabled ? "Disable" : "Enable"}
            </Button>
            <Button variant="danger" size="compact" busy={busyAction === "delete"} onClick={() => void remove()} disabled={busyAction !== null}>
              Delete
            </Button>
          </>
        ) : null}
      </div>

      {historyOpen ? (
        historyLoading ? (
          <p className="page-activity__empty">Loading…</p>
        ) : !loaded ? (
          <Button variant="secondary" size="compact" onClick={() => void loadHistory()}>
            {isWorkflow ? "Retry triggered runs" : "Retry triggered tasks"}
          </Button>
        ) : empty ? (
          <p className="page-activity__empty">This schedule has not fired yet.</p>
        ) : isWorkflow ? (
          <table className="agent-runs">
            <thead>
              <tr>
                <th>Run</th>
                <th>Status</th>
                <th>When</th>
                {deliveries ? <th>Delivery</th> : null}
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
                      {runStatusLabel(r.status)}
                    </span>
                  </td>
                  <td className="agent-runs__when">{formatWhen(r.created_at)}</td>
                  {deliveries ? <td>{describeDelivery(deliveries[r.id])}</td> : null}
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <table className="agent-runs">
            <thead>
              <tr>
                <th>Task</th>
                <th>Status</th>
                <th>When</th>
                {deliveries ? <th>Delivery</th> : null}
              </tr>
            </thead>
            <tbody>
              {(tasks ?? []).map((t) => {
                const ui = apiTaskToTask(t)
                return (
                  <tr
                    key={t.id}
                    tabIndex={0}
                    onClick={() => navigate({ name: "task", spaceId, taskId: t.id })}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") navigate({ name: "task", spaceId, taskId: t.id })
                    }}
                  >
                    <td className="agent-runs__title">{ui.title}</td>
                    <td>
                      <span className={`agent-runs__status agent-runs__status--${runStatusTone(t.status)}`}>
                        {taskStatusLabel(t)}
                      </span>
                    </td>
                    <td className="agent-runs__when">{ui.timeLabel}</td>
                    {deliveries ? <td>{describeDelivery(deliveries[t.id])}</td> : null}
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
