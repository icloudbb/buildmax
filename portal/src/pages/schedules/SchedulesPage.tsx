import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiSchedule } from "../../lib/api/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { getAgents } from "../../features/agents"
import { listSchedules, updateSchedule } from "../../features/schedules/api"
import { CreateScheduleForm, type ScheduleAgentOption } from "../../features/schedules/CreateScheduleForm"
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
  const [agents, setAgents] = useState<ScheduleAgentOption[]>([])
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  // The overview lets any member pick which agent a new schedule runs.
  const agentNames = useMemo(
    () => Object.fromEntries(agents.map((a) => [a.id, a.name])),
    [agents]
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
    return Promise.all([listSchedules(spaceId, token), getAgents(spaceId, token)])
      .then(([scheduleRes, agentList]) => {
        setSchedulesData(scheduleRes.schedules)
        setAgents(agentList.map((a) => ({ id: a.id, name: a.name })))
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

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Schedules</h1>
          <p className="page-activity__subtitle">
            Every recurring schedule in this space. Each runs one agent on a cron timetable.
          </p>
        </div>
        {canCreate && !creating ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            New schedule
          </Button>
        ) : null}
      </div>

      {canCreate && creating ? (
        <CreateScheduleForm
          token={token as string}
          spaceId={spaceId}
          agents={agents}
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
          <EmptyState message="No schedules yet. Schedule an agent to run at a set time." />
        ) : schedulesState.kind === "error" || schedulesState.kind === "forbidden" || schedulesState.kind === "notFound" ? null : (
          <ul className="issues-page__list">
            {(schedulesData ?? []).map((s) => {
              const agentName = agentNames[s.agent_id] ?? s.agent_id
              return (
                <li key={s.id} className="issues-page__list-item schedules-page__item">
                  <div className="schedules-page__main">
                    <span className="issues-page__row-title">{s.name || s.cron_expr}</span>
                    <span className="issues-page__row-desc">
                      <button
                        type="button"
                        className="schedules-page__agent-link"
                        onClick={() => navigate({ name: "agent", spaceId, agentId: s.agent_id })}
                      >
                        {agentName}
                      </button>
                      {" · "}
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
                        disabled={busyId === s.id}
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
