import { useCallback, useEffect, useMemo, useState } from "react"
import type { Agent, AgentRevision } from "../../lib/types"
import type { ApiSecret, ApiTask } from "../../lib/api/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { ApiRequestError } from "../../lib/api/client"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"
import { apiAgentToAgent, apiAgentRevisionToAgentRevision, apiTaskToTask } from "../../lib/api/mappers"
import {
  getAgent,
  updateAgent,
  deleteAgent,
  getAgentRevisions,
  restoreAgentRevision,
  listAgentModels,
  type AgentDefinitionInput,
} from "../../features/agents"
import { createAgentTask, listAgentTasks } from "../../features/tasks"
import { SchedulesSection } from "../../features/schedules/SchedulesSection"
import { listSecrets } from "../../features/spaceSecrets/api"
import { listActivations } from "../../features/spacePlugins/api"
import { listPlugins } from "../../features/plugins/api"
import { nameablePlugins } from "../../features/plugins/nameablePlugins"
import { runStatusLabel, runStatusTone, taskRunFailed, taskRunFinished } from "../../features/conversations/thread"
import { AgentAvatar } from "../../components/UserAvatar"
import { AgentConfigForm } from "../../components/AgentConfigForm"
import { RevisionHistory } from "../../components/RevisionHistory"
import { RunAgentModal } from "../../components/RunAgentModal"
import { consumptionHealthCount } from "../../components/SecretConsumptionEditor"
import { useApp } from "../../contexts/AppContext"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { isAllowed } from "../../state/permissionState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"

interface AgentDetailProps {
  token: string | null
  spaceId: string
  agentId: string
}

type Tab = "overview" | "config" | "runs" | "schedules" | "revisions"

const TABS: { id: Tab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "config", label: "Configuration" },
  { id: "runs", label: "Runs" },
  { id: "schedules", label: "Schedules" },
  { id: "revisions", label: "Revisions" },
]

export function AgentDetail({ token, spaceId, agentId }: AgentDetailProps) {
  const { currentUserRole } = useSpace()
  const { setEntityLabel } = useApp()
  const canManage = isAllowed(useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin"))
  // Schedules are member-tier (manage_schedules), unlike agent config which is
  // owner/admin, so any member of the space may create and pause them.
  const canManageSchedules = isAllowed(
    useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin" || currentUserRole === "member")
  )

  const [agent, setAgent] = useState<Agent | null>(null)
  const [secrets, setSecrets] = useState<ApiSecret[]>([])
  const [availablePlugins, setAvailablePlugins] = useState<string[]>([])
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [tasks, setTasks] = useState<ApiTask[]>([])
  // null means "not yet successfully fetched", distinct from [] meaning the
  // agent genuinely has no revisions. See deriveResourceState.
  const [revisionsData, setRevisionsData] = useState<AgentRevision[] | null>(null)
  const [tab, setTab] = useState<Tab>("overview")

  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [unavailable, setUnavailable] = useState<ResourceUnavailableKind | null>(null)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [runOpen, setRunOpen] = useState(false)
  const [starting, setStarting] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)
  const [revisionsLoading, setRevisionsLoading] = useState(false)
  const [revisionsListError, setRevisionsListError] = useState<RequestError | null>(null)
  const [restoringRevision, setRestoringRevision] = useState<number | null>(null)
  // Restore's own error, tagged with which revision it was.
  const [restoreRevisionError, setRestoreRevisionError] = useState<{ revision: number; message: string } | null>(null)

  const load = useCallback(async () => {
    if (!token || !spaceId) {
      setAgent(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    setUnavailable(null)
    try {
      const [agentApi, tasksApi, revisionsApi] = await Promise.all([
        getAgent(spaceId, agentId, token),
        listAgentTasks(spaceId, agentId, token),
        getAgentRevisions(spaceId, agentId, token),
      ])
      setAgent(apiAgentToAgent(agentApi))
      setTasks(tasksApi.tasks)
      setRevisionsData(revisionsApi.revisions.map(apiAgentRevisionToAgentRevision))
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 404) {
        setUnavailable("notFound")
      } else if (err instanceof ApiRequestError && err.status === 403) {
        setUnavailable("forbidden")
      } else {
        setUnavailable("error")
      }
      setAgent(null)
      setError(getErrorMessage(err, "Failed to load agent"))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, agentId])

  useEffect(() => {
    void load()
  }, [load])

  // Publish the loaded name so the breadcrumb reads "Agents / <name>" instead of
  // the opaque id, and updates in place after a rename.
  useEffect(() => {
    if (agent) setEntityLabel(agent.id, agent.name)
  }, [agent, setEntityLabel])

  // Secrets and the nameable plugin set feed the config editor and the secret
  // health check. Only owners/admins may list them; a member gets empty options
  // rather than a blocked page.
  useEffect(() => {
    if (!token || !spaceId || !canManage) {
      setSecrets([])
      setAvailablePlugins([])
      return
    }
    listSecrets(token, spaceId)
      .then((res) => setSecrets(res.secrets ?? []))
      .catch(() => setSecrets([]))
    Promise.all([
      listActivations(token, spaceId).catch(() => null),
      listPlugins(token).catch(() => null),
    ])
      .then(([activations, catalog]) =>
        setAvailablePlugins(nameablePlugins(activations, catalog?.plugins ?? null)),
      )
      .catch(() => setAvailablePlugins([]))
    // The model catalog is deployment-wide, so it is fetched independently of
    // the space-scoped plugin and secret options; an empty list leaves the
    // picker at just the deployment default.
    listAgentModels(token)
      .then(setAvailableModels)
      .catch(() => setAvailableModels([]))
  }, [token, spaceId, canManage])

  const loadRevisions = useCallback(() => {
    if (!token || !spaceId) return
    setRevisionsLoading(true)
    setRevisionsListError(null)
    getAgentRevisions(spaceId, agentId, token)
      .then((res) => setRevisionsData(res.revisions.map(apiAgentRevisionToAgentRevision)))
      // revisionsData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping history.
      .catch((err) => setRevisionsListError(classifyError(err, "Failed to load history")))
      .finally(() => setRevisionsLoading(false))
  }, [token, spaceId, agentId])

  const revisionEntries = useMemo(
    () =>
      revisionsData?.map((rev) => ({
        id: rev.id,
        revision: rev.revision,
        createdBy: rev.createdBy,
        createdLabel: rev.createdLabel,
        summary: rev.instructions,
      })) ?? null,
    [revisionsData]
  )
  const revisionsState = useMemo(
    () =>
      deriveResourceState({
        loading: revisionsLoading,
        data: revisionEntries,
        error: revisionsListError,
        isEmpty: (data) => data.length === 0,
      }),
    [revisionsLoading, revisionEntries, revisionsListError]
  )

  function handleSave(definition: AgentDefinitionInput) {
    if (!token || !spaceId || !agent) return
    setSaving(true)
    setSaveError(null)
    updateAgent(spaceId, agent.id, definition, token)
      .then((updated) => {
        setAgent(apiAgentToAgent(updated))
        loadRevisions()
      })
      .catch((err) => setSaveError(getErrorMessage(err, "Failed to update agent")))
      .finally(() => setSaving(false))
  }

  function handleDelete() {
    if (!token || !spaceId || !agent) return
    setDeleting(true)
    setSaveError(null)
    deleteAgent(spaceId, agent.id, token)
      .then(() => navigate({ name: "agents", spaceId }))
      .catch((err) => {
        setSaveError(getErrorMessage(err, "Failed to delete agent"))
        setDeleting(false)
      })
  }

  function handleRestoreRevision(revision: number) {
    if (!token || !spaceId || !agent) return
    setRestoreRevisionError(null)
    setRestoringRevision(revision)
    restoreAgentRevision(spaceId, agent.id, revision, token)
      .then((restored) => {
        setAgent(apiAgentToAgent(restored))
        loadRevisions()
      })
      .catch((err) => setRestoreRevisionError({ revision, message: getErrorMessage(err, "Failed to restore revision") }))
      .finally(() => setRestoringRevision(null))
  }

  function handleStartRun(input: string) {
    if (!token || !spaceId || !agent) return
    setStarting(true)
    setRunError(null)
    createAgentTask(spaceId, agent.id, input, token)
      .then((created) => {
        setRunOpen(false)
        navigate({ name: "task", spaceId, taskId: created.id })
      })
      .catch((err) => setRunError(getErrorMessage(err, "Failed to run agent")))
      .finally(() => setStarting(false))
  }

  const stats = useMemo(() => {
    const finished = tasks.filter((t) => taskRunFinished(t.status))
    const failed = finished.filter((t) => taskRunFailed(t.status)).length
    const succeeded = finished.length - failed
    const running = tasks.some((t) => !taskRunFinished(t.status))
    const successRate = finished.length > 0 ? `${Math.round((succeeded / finished.length) * 100)}%` : "—"
    return { total: tasks.length, successRate, running, lastRun: tasks[0] ? apiTaskToTask(tasks[0]).timeLabel : "—" }
  }, [tasks])

  const secretWarnings = agent ? consumptionHealthCount(agent.secretConsumption, secrets) : 0

  function renderRunsTable(rows: ApiTask[]) {
    if (rows.length === 0) return <p className="page-activity__empty">No executions yet.</p>
    return (
      <table className="agent-runs">
        <thead>
          <tr>
            <th>Task</th>
            <th>Status</th>
            <th>When</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((t) => {
            const ui = apiTaskToTask(t)
            const tone = runStatusTone(t.status)
            return (
              <tr key={t.id} onClick={() => navigate({ name: "task", spaceId, taskId: t.id })} tabIndex={0}
                onKeyDown={(e) => {
                  if (e.key === "Enter") navigate({ name: "task", spaceId, taskId: t.id })
                }}>
                <td className="agent-runs__title">{ui.title}</td>
                <td>
                  <span className={`agent-runs__status agent-runs__status--${tone}`}>{runStatusLabel(t.status)}</span>
                </td>
                <td className="agent-runs__when">{ui.timeLabel}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    )
  }

  if (loading) {
    return (
      <div className="page-activity">
        <p className="page-activity__empty">Loading…</p>
      </div>
    )
  }

  if (unavailable) {
    return (
      <ResourceUnavailable
        resourceLabel="Agent"
        kind={unavailable}
        errorMessage={error}
        onRetry={() => void load()}
        backLabel="Back to Agents"
        onBack={() => navigate({ name: "agents", spaceId })}
      />
    )
  }

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div className="agent-detail__ident">
          <AgentAvatar size="md" className="agent-detail__avatar" />
          <div>
            <h1 className="page-activity__title">
              {agent?.name ?? "Agent"}
              {stats.running ? <span className="agent-detail__running">running</span> : null}
            </h1>
            {agent?.description ? <p className="agent-detail__desc">{agent.description}</p> : null}
          </div>
        </div>
        <div className="page-activity__actions">
          <button type="button" className="page-activity__action-btn" onClick={() => navigate({ name: "agents", spaceId })}>
            Back to Agents
          </button>
          {canManage ? (
            <button type="button" className="page-activity__action-btn" onClick={() => setTab("config")}>
              Edit config
            </button>
          ) : null}
          <button
            type="button"
            className="page-activity__action-btn"
            disabled={!agent}
            onClick={() => {
              setRunError(null)
              setRunOpen(true)
            }}
          >
            Run agent
          </button>
        </div>
      </div>

      {agent && (
        <>
          <nav className="agent-detail__tabs" aria-label="Agent sections" role="tablist">
            {TABS.map((t) => (
              <button
                key={t.id}
                type="button"
                role="tab"
                className={
                  t.id === tab ? "agent-detail__tab agent-detail__tab--active" : "agent-detail__tab"
                }
                aria-selected={t.id === tab}
                onClick={() => setTab(t.id)}
              >
                {t.label}
                {t.id === "runs" && tasks.length > 0 ? (
                  <span className="agent-detail__tab-count">{tasks.length}</span>
                ) : null}
                {t.id === "revisions" && agent.revision > 0 ? (
                  <span className="agent-detail__tab-count">{agent.revision}</span>
                ) : null}
              </button>
            ))}
          </nav>

          {tab === "overview" ? (
            <section className="agent-detail__panel">
              {secretWarnings > 0 ? (
                <div className="agent-detail__banner" role="alert">
                  <span>
                    ⚠ {secretWarnings} secret grant{secretWarnings === 1 ? "" : "s"} no longer resolve.
                  </span>
                  {canManage ? (
                    <button type="button" className="page-activity__action-btn" onClick={() => setTab("config")}>
                      Fix in config
                    </button>
                  ) : null}
                </div>
              ) : null}
              <div className="agent-detail__stats">
                <div className="agent-detail__stat">
                  <span className="agent-detail__stat-label">Total runs</span>
                  <span className="agent-detail__stat-value">{stats.total}</span>
                </div>
                <div className="agent-detail__stat">
                  <span className="agent-detail__stat-label">Success rate</span>
                  <span className="agent-detail__stat-value">{stats.successRate}</span>
                </div>
                <div className="agent-detail__stat">
                  <span className="agent-detail__stat-label">Last run</span>
                  <span className="agent-detail__stat-value agent-detail__stat-value--sm">{stats.lastRun}</span>
                </div>
              </div>
              <div className="agent-detail__section-head">
                <h2 className="issues-page__section-title">Recent runs</h2>
                {tasks.length > 3 ? (
                  <button type="button" className="page-activity__action-btn" onClick={() => setTab("runs")}>
                    View all
                  </button>
                ) : null}
              </div>
              {renderRunsTable(tasks.slice(0, 3))}
            </section>
          ) : null}

          {tab === "config" ? (
            <section className="agent-detail__panel">
              <AgentConfigForm
                agent={agent}
                secrets={secrets}
                availablePlugins={availablePlugins}
                availableModels={availableModels}
                canManage={canManage}
                saving={saving}
                deleting={deleting}
                error={saveError}
                onSave={handleSave}
                onDelete={handleDelete}
              />
            </section>
          ) : null}

          {tab === "runs" ? (
            <section className="agent-detail__panel">
              <p className="page-activity__subtitle">Each run is a durable Task thread. Select one to open it.</p>
              {renderRunsTable(tasks)}
            </section>
          ) : null}

          {tab === "schedules" ? (
            <section className="agent-detail__panel">
              {token ? (
                <SchedulesSection token={token} spaceId={spaceId} agentId={agent.id} canManage={canManageSchedules} />
              ) : null}
            </section>
          ) : null}

          {tab === "revisions" ? (
            <section className="agent-detail__panel">
              <RevisionHistory
                title="Configuration history"
                state={revisionsState}
                onRetry={loadRevisions}
                currentRevision={agent.revision}
                canRestore={canManage}
                restoringRevision={restoringRevision}
                restoreError={restoreRevisionError}
                onRestore={handleRestoreRevision}
              />
            </section>
          ) : null}
        </>
      )}

      <RunAgentModal
        open={runOpen}
        agent={agent}
        loading={starting}
        error={runError}
        onClose={() => {
          setRunOpen(false)
          setRunError(null)
        }}
        onStart={handleStartRun}
      />
    </div>
  )
}
