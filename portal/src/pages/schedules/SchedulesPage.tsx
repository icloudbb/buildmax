import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiSchedule } from "../../lib/api/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { getAgents } from "../../features/agents"
import { getWorkflows } from "../../features/workflows"
import { listSchedules, updateSchedule } from "../../features/schedules/api"
import { CreateScheduleForm, type ScheduleExecutorOption } from "../../features/schedules/CreateScheduleForm"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed } from "../../state/permissionState"

interface SchedulesPageProps {
  token: string | null
  spaceId: string
}

function formatWhen(iso: string | null | undefined): string {
  if (!iso) return "—"
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString()
}

// This is the space-wide overview: every schedule across every agent, so an
// owner can see what unattended automation is running. A member can create a
// schedule here (picking which agent runs it), pause or resume one, and open its
// agent; editing an existing schedule stays on the agent's detail page. See
// docs/design/scheduled-agent-execution.md.
export function SchedulesPage({ token, spaceId }: SchedulesPageProps) {
  const { currentUserRole } = useSpace()
  // Any member may manage schedules (manage_schedules is member-tier), so the
  // capability is membership itself, not the owner/admin gate agents use.
  const canManageState = useSpaceCapability(
    currentUserRole === "owner" || currentUserRole === "admin" || currentUserRole === "member"
  )
  const canManage = isAllowed(canManageState)
  // Creating needs a token to call the API; the pause/resume actions already do.
  const canCreate = canManage && !!token

  // null means "not yet successfully fetched", distinct from [] (no schedules).
  const [schedulesData, setSchedulesData] = useState<ApiSchedule[] | null>(null)
  const [executors, setExecutors] = useState<ScheduleExecutorOption[]>([])
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  // Which bulk action, if any, is running — so both buttons and every row action
  // disable together while it does.
  const [bulkBusy, setBulkBusy] = useState<"enable" | "pause" | null>(null)

  // Names for the schedule list, keyed by "<kind>:<id>" so an agent and a
  // workflow that happen to share an id never collide.
  const executorNames = useMemo(
    () => Object.fromEntries(executors.map((e) => [`${e.kind}:${e.id}`, e.name])),
    [executors]
  )

  const fetchSchedules = useCallback(() => {
    if (!token || !spaceId) {
      setSchedulesData(null)
      setLoading(false)
      setListError(null)
      return Promise.resolve()
    }
    setLoading(true)
    setListError(null)
    // Agents and published workflows are both schedulable; a draft or archived
    // workflow cannot start a run, so it is not offered.
    return Promise.all([listSchedules(spaceId, token), getAgents(spaceId, token), getWorkflows(spaceId, token)])
      .then(([scheduleRes, agentList, workflowRes]) => {
        setSchedulesData(scheduleRes.schedules)
        const agentOptions: ScheduleExecutorOption[] = agentList.map((a) => ({ kind: "agent", id: a.id, name: a.name }))
        const workflowOptions: ScheduleExecutorOption[] = workflowRes.workflows
          .filter((w) => w.status === "published")
          .map((w) => ({ kind: "workflow", id: w.id, name: w.name, definition: w.definition }))
        setExecutors([...agentOptions, ...workflowOptions])
      })
      // schedulesData from a prior fetch is left in place, so a failed refresh
      // reads as Stale rather than wiping the list.
      .catch((err) => setListError(classifyError(err, "Failed to load schedules")))
      .finally(() => setLoading(false))
  }, [token, spaceId])

  useEffect(() => {
    void fetchSchedules()
  }, [fetchSchedules])

  const schedulesState = useMemo(
    () => deriveResourceState({ loading, data: schedulesData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, schedulesData, listError]
  )

  const countLabel = useMemo(() => {
    const count = schedulesData?.length ?? 0
    return count === 1 ? "1 schedule" : `${count} schedules`
  }, [schedulesData])

  function toggleEnabled(schedule: ApiSchedule) {
    if (!token || !spaceId) return
    setBusyId(schedule.id)
    setActionError(null)
    updateSchedule(spaceId, schedule.id, { enabled: !schedule.enabled }, token)
      .then(() => fetchSchedules())
      .catch((err) => setActionError(getErrorMessage(err, "Failed to update schedule")))
      .finally(() => setBusyId(null))
  }

  // How many schedules a bulk action would touch: pausing affects the enabled
  // ones, resuming the paused ones. Used to label and disable the buttons.
  const enabledCount = schedulesData?.filter((s) => s.enabled).length ?? 0
  const pausedCount = schedulesData?.filter((s) => !s.enabled).length ?? 0

  // setAllEnabled flips every schedule that is not already in the target state,
  // reusing the same per-schedule endpoint the row toggle uses. The calls run
  // together and settle independently, so one failure neither aborts the rest nor
  // hides that it happened — the count that failed is surfaced, and the list is
  // reloaded to show the true state either way.
  async function setAllEnabled(target: boolean) {
    if (!token || !spaceId || !schedulesData) return
    const affected = schedulesData.filter((s) => s.enabled !== target)
    if (affected.length === 0) return
    setBulkBusy(target ? "enable" : "pause")
    setActionError(null)
    const results = await Promise.allSettled(
      affected.map((s) => updateSchedule(spaceId, s.id, { enabled: target }, token))
    )
    const failed = results.filter((r) => r.status === "rejected").length
    if (failed > 0) {
      const verb = target ? "resume" : "pause"
      setActionError(`Failed to ${verb} ${failed} of ${affected.length} schedule${affected.length === 1 ? "" : "s"}.`)
    }
    await fetchSchedules()
    setBulkBusy(null)
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Schedules</h1>
          <p className="page-activity__subtitle">
            Every recurring schedule in this space. Each runs one agent or workflow on a cron timetable.
          </p>
        </div>
        <div className="page-activity__actions">
          {canManage && (schedulesData?.length ?? 0) > 0 ? (
            <>
              <Button
                variant="secondary"
                busy={bulkBusy === "pause"}
                disabled={bulkBusy !== null || busyId !== null || enabledCount === 0}
                onClick={() => void setAllEnabled(false)}
              >
                Pause all
              </Button>
              <Button
                variant="secondary"
                busy={bulkBusy === "enable"}
                disabled={bulkBusy !== null || busyId !== null || pausedCount === 0}
                onClick={() => void setAllEnabled(true)}
              >
                Resume all
              </Button>
            </>
          ) : null}
          {canCreate && !creating ? (
            <Button variant="primary" onClick={() => setCreating(true)}>
              New schedule
            </Button>
          ) : null}
        </div>
      </div>

      {canCreate && creating ? (
        <CreateScheduleForm
          token={token as string}
          spaceId={spaceId}
          executors={executors}
          onCreated={async () => {
            setCreating(false)
            await fetchSchedules()
          }}
          onCancel={() => setCreating(false)}
        />
      ) : null}

      {(schedulesState.kind === "error" ||
        schedulesState.kind === "forbidden" ||
        schedulesState.kind === "notFound" ||
        schedulesState.kind === "stale") && (
        <Alert
          tone={schedulesState.kind === "stale" ? "stale" : schedulesState.kind}
          message={schedulesState.error.message}
          retry={{ label: "Retry", onClick: () => void fetchSchedules() }}
        />
      )}
      {actionError ? <Alert tone="error" message={actionError} /> : null}

      <section className="issues-page__panel" aria-label="Schedule list">
        <div className="issues-page__toolbar">
          {schedulesData !== null ? <span className="page-activity__meta">{countLabel}</span> : null}
        </div>

        {schedulesState.kind === "loading" ? (
          <p className="page-activity__empty">Loading…</p>
        ) : schedulesState.kind === "readyEmpty" ? (
          <EmptyState message="No schedules yet. Schedule an agent or workflow to run at a set time." />
        ) : schedulesState.kind === "error" || schedulesState.kind === "forbidden" || schedulesState.kind === "notFound" ? null : (
          <ul className="issues-page__list">
            {(schedulesData ?? []).map((s) => {
              const executorName = executorNames[`${s.executor_kind}:${s.executor_id}`] ?? s.executor_id
              const openExecutor = () =>
                s.executor_kind === "workflow"
                  ? navigate({ name: "workflow", spaceId, workflowId: s.executor_id })
                  : navigate({ name: "agent", spaceId, agentId: s.executor_id })
              return (
                <li key={s.id} className="issues-page__list-item schedules-page__item">
                  <div className="schedules-page__main">
                    <span className="issues-page__row-title">{s.name || s.cron_expr}</span>
                    <span className="issues-page__row-desc">
                      <button
                        type="button"
                        className="schedules-page__agent-link"
                        onClick={openExecutor}
                      >
                        {executorName}
                      </button>
                      {` (${s.executor_kind}) · `}
                      <code>{s.cron_expr}</code> {s.timezone}
                      {s.enabled ? ` · next ${formatWhen(s.next_fire_at)}` : ""}
                    </span>
                  </div>
                  <div className="schedules-page__side">
                    <span
                      className={
                        s.enabled ? "issues-page__status" : "issues-page__status schedules-page__status--off"
                      }
                    >
                      {s.enabled ? "Enabled" : "Paused"}
                    </span>
                    {s.consecutive_failures > 0 ? (
                      <span className="schedules-page__failures">
                        {s.consecutive_failures} failure{s.consecutive_failures === 1 ? "" : "s"}
                      </span>
                    ) : null}
                    {canManage ? (
                      <Button
                        variant="secondary"
                        size="compact"
                        disabled={busyId === s.id || bulkBusy !== null}
                        onClick={() => toggleEnabled(s)}
                      >
                        {s.enabled ? "Pause" : "Resume"}
                      </Button>
                    ) : null}
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </section>
    </div>
  )
}
