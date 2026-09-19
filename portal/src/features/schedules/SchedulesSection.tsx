import { useCallback, useEffect, useState } from "react"
import type { ApiSchedule, ApiTask } from "../../lib/api/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { apiTaskToTask } from "../../lib/api/mappers"
import { runStatusLabel, runStatusTone } from "../conversations/thread"
import { CreateScheduleForm } from "./CreateScheduleForm"
import {
  deleteSchedule,
  listScheduleTasks,
  listSchedules,
  updateSchedule,
} from "./api"

interface SchedulesSectionProps {
  token: string
  spaceId: string
  agentId: string
  canManage: boolean
}

function formatWhen(iso: string | null | undefined): string {
  if (!iso) return "—"
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString()
}

export function SchedulesSection({ token, spaceId, agentId, canManage }: SchedulesSectionProps) {
  // null distinguishes "not yet fetched" from an agent with no schedules.
  const [schedules, setSchedules] = useState<ApiSchedule[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await listSchedules(spaceId, token)
      // The list is space-wide; this section shows only this agent's schedules.
      setSchedules(res.schedules.filter((s) => s.agent_id === agentId))
    } catch (err) {
      setError(getErrorMessage(err, "Failed to load schedules"))
    }
  }, [spaceId, agentId, token])

  useEffect(() => {
    void load()
  }, [load])

  if (error && schedules === null) {
    return (
      <div className="agent-detail__banner" role="alert">
        <span>{error}</span>
        <button type="button" className="page-activity__action-btn" onClick={() => void load()}>
          Retry
        </button>
      </div>
    )
  }

  return (
    <div className="agent-schedules">
      <p className="page-activity__subtitle">
        A schedule runs this agent automatically on a cron timetable. Each firing starts a Task.
      </p>

      {canManage ? (
        creating ? (
          <CreateScheduleForm
            token={token}
            spaceId={spaceId}
            agentId={agentId}
            onCreated={async () => {
              setCreating(false)
              await load()
            }}
            onCancel={() => setCreating(false)}
          />
        ) : (
          <button type="button" className="page-activity__action-btn" onClick={() => setCreating(true)}>
            New schedule
          </button>
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
  onChanged,
}: {
  schedule: ApiSchedule
  spaceId: string
  token: string
  canManage: boolean
  onChanged: () => Promise<void>
}) {
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [tasks, setTasks] = useState<ApiTask[] | null>(null)
  const [tasksOpen, setTasksOpen] = useState(false)

  async function toggleEnabled() {
    setBusy(true)
    setErr(null)
    try {
      await updateSchedule(spaceId, schedule.id, { enabled: !schedule.enabled }, token)
      await onChanged()
    } catch (e) {
      setErr(getErrorMessage(e, "Failed to update schedule"))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    if (!window.confirm(`Delete schedule "${schedule.name || schedule.cron_expr}"? Tasks it already created are kept.`)) return
    setBusy(true)
    setErr(null)
    try {
      await deleteSchedule(spaceId, schedule.id, token)
      await onChanged()
    } catch (e) {
      setErr(getErrorMessage(e, "Failed to delete schedule"))
      setBusy(false)
    }
  }

  async function toggleTasks() {
    const next = !tasksOpen
    setTasksOpen(next)
    if (next && tasks === null) {
      try {
        const res = await listScheduleTasks(spaceId, schedule.id, token)
        setTasks(res.tasks)
      } catch (e) {
        setErr(getErrorMessage(e, "Failed to load triggered tasks"))
      }
    }
  }

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
      </dl>

      <p className="agent-schedules__prompt">{schedule.input}</p>

      {err ? <p className="agent-schedules__error" role="alert">{err}</p> : null}

      <div className="agent-schedules__card-actions">
        <button type="button" className="page-activity__action-btn" onClick={() => void toggleTasks()}>
          {tasksOpen ? "Hide triggered tasks" : "Show triggered tasks"}
        </button>
        {canManage ? (
          <>
            <button type="button" className="page-activity__action-btn" onClick={() => void toggleEnabled()} disabled={busy}>
              {schedule.enabled ? "Disable" : "Enable"}
            </button>
            <button type="button" className="page-activity__action-btn" onClick={() => void remove()} disabled={busy}>
              Delete
            </button>
          </>
        ) : null}
      </div>

      {tasksOpen ? (
        tasks === null ? (
          <p className="page-activity__empty">Loading…</p>
        ) : tasks.length === 0 ? (
          <p className="page-activity__empty">This schedule has not fired yet.</p>
        ) : (
          <table className="agent-runs">
            <thead>
              <tr>
                <th>Task</th>
                <th>Status</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map((t) => {
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
                        {runStatusLabel(t.status)}
                      </span>
                    </td>
                    <td className="agent-runs__when">{ui.timeLabel}</td>
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
