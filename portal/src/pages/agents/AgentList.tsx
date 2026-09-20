import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { Agent } from "../../lib/types"
import type { ApiSecret, ApiTask } from "../../lib/api/types"
import { listSecrets } from "../../features/spaceSecrets/api"
import { listActivations } from "../../features/spacePlugins/api"
import { listPlugins } from "../../features/plugins/api"
import { nameablePlugins } from "../../features/plugins/nameablePlugins"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { apiAgentToAgent, apiTaskToTask } from "../../lib/api/mappers"
import { createAgentTask, listAgentTasks } from "../../features/tasks"
import { getAgents, createAgent, listAgentModels } from "../../features/agents"
import { runStatusLabel, runStatusTone, taskRunFailed, taskRunFinished } from "../../features/conversations/thread"
import { AgentAvatar } from "../../components/UserAvatar"
import { CreateAgentModal } from "../../components/CreateAgentModal"
import { consumptionHealthCount } from "../../components/SecretConsumptionEditor"
import { RunAgentModal } from "../../components/RunAgentModal"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed } from "../../state/permissionState"

interface AgentListProps {
  token: string | null
  spaceId: string
}

// Stable identity so `agents` doesn't churn every render while agentsData is
// still null (before the first successful fetch).
const EMPTY_AGENTS: Agent[] = []

export function AgentList({ token, spaceId }: AgentListProps) {
  const { currentUserRole } = useSpace()
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no agents. See deriveResourceState.
  const [agentsData, setAgentsData] = useState<Agent[] | null>(null)
  const agents = agentsData ?? EMPTY_AGENTS
  const [secrets, setSecrets] = useState<ApiSecret[]>([])
  const [availablePlugins, setAvailablePlugins] = useState<string[]>([])
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [tasksByAgent, setTasksByAgent] = useState<Record<string, ApiTask[]>>({})
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [newTaskAgent, setNewTaskAgent] = useState<Agent | null>(null)
  const [startingTaskAgentId, setStartingTaskAgentId] = useState<string | null>(null)
  // Distinct from listError: the create-agent / run-agent mutations' own
  // error, shown inside their respective modals.
  const [error, setError] = useState<string | null>(null)
  const canManageAgentsState = useSpaceCapability(currentUserRole === "owner" || currentUserRole === "admin")
  const canManageAgents = isAllowed(canManageAgentsState)

  const fetchAgents = useCallback(() => {
    if (!token || !spaceId) {
      setAgentsData(null)
      setLoading(false)
      setListError(null)
      return
    }
    setLoading(true)
    setListError(null)
    getAgents(spaceId, token)
      .then((list) => {
        setAgentsData(list.map(apiAgentToAgent))
      })
      // agentsData from a prior successful fetch (if any) is left in place, so
      // a failed refresh reads as Stale rather than wiping the grid.
      .catch((err) => setListError(classifyError(err, "Failed to load agents")))
      .finally(() => setLoading(false))
  }, [token, spaceId])

  const agentsState = useMemo(
    () => deriveResourceState({ loading, data: agentsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, agentsData, listError]
  )

  // The space's secrets, to populate the create dialog's consumption editor and
  // flag broken grants on the cards and overview. Owner-or-admin may list them;
  // a failure leaves the editor with no options rather than blocking the page.
  useEffect(() => {
    if (!token || !spaceId || !canManageAgents) {
      setSecrets([])
      return
    }
    listSecrets(token, spaceId)
      .then((res) => setSecrets(res.secrets ?? []))
      .catch(() => setSecrets([]))
  }, [token, spaceId, canManageAgents])

  // The plugin names an agent in this space may name, for the create dialog's
  // plugins picker. A deployment without a Marketplace, or a failed request,
  // leaves it empty and the picker shows its empty state.
  useEffect(() => {
    if (!token || !spaceId || !canManageAgents) {
      setAvailablePlugins([])
      return
    }
    Promise.all([
      listActivations(token, spaceId).catch(() => null),
      listPlugins(token).catch(() => null),
    ])
      .then(([activations, catalog]) =>
        setAvailablePlugins(nameablePlugins(activations, catalog?.plugins ?? null)),
      )
      .catch(() => setAvailablePlugins([]))
    // Deployment-wide catalog, fetched independently of the space-scoped plugin
    // options; an empty list leaves the picker at just the deployment default.
    listAgentModels(token)
      .then(setAvailableModels)
      .catch(() => setAvailableModels([]))
  }, [token, spaceId, canManageAgents])

  useEffect(() => {
    fetchAgents()
  }, [fetchAgents])

  // Runs per agent feed both the overview aggregates and each card's activity.
  // There is no space-wide task endpoint, so this fans out one request per agent;
  // each is independent and a failure leaves that agent with no runs rather than
  // breaking the page. Tasks are stored newest-first for the "last run" label.
  useEffect(() => {
    if (!token || !spaceId || agents.length === 0) {
      setTasksByAgent({})
      return
    }
    let cancelled = false
    Promise.all(
      agents.map((a) =>
        listAgentTasks(spaceId, a.id, token)
          .then((res) => [a.id, [...res.tasks].sort((x, y) => y.created_at.localeCompare(x.created_at))] as const)
          .catch(() => [a.id, [] as ApiTask[]] as const),
      ),
    ).then((entries) => {
      if (!cancelled) setTasksByAgent(Object.fromEntries(entries))
    })
    return () => {
      cancelled = true
    }
  }, [token, spaceId, agents])

  const allTasks = useMemo(
    () => agents.flatMap((a) => (tasksByAgent[a.id] ?? []).map((task) => ({ task, agent: a }))),
    [agents, tasksByAgent],
  )

  const stats = useMemo(() => {
    const tasks = allTasks.map((x) => x.task)
    const finished = tasks.filter((t) => taskRunFinished(t.status))
    const failed = finished.filter((t) => taskRunFailed(t.status)).length
    const succeeded = finished.length - failed
    const running = tasks.filter((t) => !taskRunFinished(t.status)).length
    const successRate = finished.length > 0 ? `${Math.round((succeeded / finished.length) * 100)}%` : "—"
    const warnings = canManageAgents
      ? agents.reduce((n, a) => n + consumptionHealthCount(a.secretConsumption, secrets), 0)
      : 0
    return { total: tasks.length, running, successRate, warnings }
  }, [allTasks, agents, secrets, canManageAgents])

  const recent = useMemo(
    () => [...allTasks].sort((a, b) => b.task.created_at.localeCompare(a.task.created_at)).slice(0, 6),
    [allTasks],
  )

  function agentMeta(agent: Agent) {
    const ts = tasksByAgent[agent.id] ?? []
    return {
      count: ts.length,
      running: ts.some((t) => !taskRunFinished(t.status)),
      last: ts[0] ? apiTaskToTask(ts[0]).timeLabel : null,
    }
  }

  function handleCreateAgent(values: {
    name: string
    description?: string
    instructions?: string
    model?: string
    plugins?: string[]
    sandbox_network_tier?: string
    sandbox_filesystem_tier?: string
    secret_consumption?: import("../../lib/api/types").ApiSecretConsumption
  }) {
    if (!token || !spaceId) return
    setError(null)
    setCreating(true)
    createAgent(spaceId, values, token)
      .then((created) => {
        const mapped = apiAgentToAgent(created)
        setAgentsData((prev) => [...(prev ?? []), mapped])
        setModalOpen(false)
        navigate({ name: "agent", spaceId, agentId: mapped.id })
      })
      .catch((err) => setError(getErrorMessage(err, "Failed to create agent")))
      .finally(() => setCreating(false))
  }

  function handleOpenNewTaskModal(agent: Agent) {
    setError(null)
    setNewTaskAgent(agent)
  }

  function handleStartTaskFromAgent(editedInput: string) {
    if (!token || !spaceId || !newTaskAgent) return
    setError(null)
    setStartingTaskAgentId(newTaskAgent.id)
    createAgentTask(spaceId, newTaskAgent.id, editedInput, token)
      .then((created) => {
        setNewTaskAgent(null)
        navigate({ name: "task", spaceId, taskId: created.id })
      })
      .catch((err) => {
        setError(getErrorMessage(err, "Failed to run agent"))
      })
      .finally(() => setStartingTaskAgentId(null))
  }

  const kpis: { label: string; value: string | number; show: boolean }[] = [
    { label: "Agents", value: agents.length, show: true },
    { label: "Running now", value: stats.running, show: true },
    { label: "Total runs", value: stats.total, show: true },
    { label: "Success rate", value: stats.successRate, show: true },
    { label: "Config warnings", value: stats.warnings, show: canManageAgents },
  ]

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Agents</h1>
          <p className="page-activity__subtitle">
            Create and manage space agents (personas / task templates).
          </p>
        </div>
        <div className="page-activity__actions">
          {canManageAgents ? (
            <Button
              variant="primary"
              className="agent-list__create-btn"
              onClick={() => {
                setError(null)
                setModalOpen(true)
              }}
              aria-label="Create agent"
            >
              Create agent
            </Button>
          ) : null}
        </div>
      </div>

      {(agentsState.kind === "error" ||
        agentsState.kind === "forbidden" ||
        agentsState.kind === "notFound" ||
        agentsState.kind === "stale") && (
        <Alert
          tone={agentsState.kind === "stale" ? "stale" : agentsState.kind}
          message={agentsState.error.message}
          retry={{ label: "Retry", onClick: () => fetchAgents() }}
        />
      )}
      {error ? <p className="page-activity__empty">{error}</p> : null}

      {canManageAgentsState === "denied" ? (
        <p className="page-activity__empty">
          You can start conversations with space agents, but only space owners and admins can create or edit them.
        </p>
      ) : canManageAgentsState === "failed" ? (
        <p className="page-activity__empty">
          Couldn&apos;t verify your role in this space, so creating or editing agents stays unavailable. Refresh to try again.
        </p>
      ) : canManageAgentsState === "unknown" ? (
        <p className="page-activity__empty">Checking whether you can manage agents…</p>
      ) : null}

      {!loading && agents.length > 0 ? (
        <div className="agent-kpis">
          {kpis
            .filter((k) => k.show)
            .map((k) => (
              <div key={k.label} className="agent-kpi">
                <span className="agent-kpi__label">{k.label}</span>
                <span className="agent-kpi__value">{k.value}</span>
              </div>
            ))}
        </div>
      ) : null}

      <div className="agent-home">
        <section className="agent-list">
          {agentsState.kind === "loading" ? (
            <p className="page-activity__empty">Loading…</p>
          ) : agentsState.kind === "readyEmpty" ? (
            <EmptyState
              message={
                canManageAgents
                  ? 'No agents yet. Click "Create agent" to add one.'
                  : "No agents are available in this space yet. Space owners and admins can add one when you're ready to share a reusable agent."
              }
            />
          ) : agentsState.kind === "error" || agentsState.kind === "forbidden" || agentsState.kind === "notFound" ? null : (
            <div className="agent-list__grid">
              {agents.map((a) => {
                const meta = agentMeta(a)
                return (
                  <article
                    key={a.id}
                    className="agent-card"
                    role="button"
                    tabIndex={0}
                    aria-label={`Open agent ${a.name}`}
                    onClick={() => navigate({ name: "agent", spaceId, agentId: a.id })}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault()
                        navigate({ name: "agent", spaceId, agentId: a.id })
                      }
                    }}
                  >
                    <header className="agent-card__header">
                      <AgentAvatar size="md" className="agent-card__avatar" />
                      <div className="agent-card__title-row">
                        <h3 className="agent-card__name">{a.name}</h3>
                        {meta.running ? (
                          <span className="agent-card__running">running</span>
                        ) : (
                          <span className="agent-card__edit-hint" aria-hidden>Open</span>
                        )}
                      </div>
                    </header>
                    {a.description ? (
                      <p className="agent-card__description">{a.description}</p>
                    ) : null}
                    {canManageAgents && consumptionHealthCount(a.secretConsumption, secrets) > 0 ? (
                      <p className="agent-card__secret-warning" role="alert">
                        ⚠ {consumptionHealthCount(a.secretConsumption, secrets)} secret grant
                        {consumptionHealthCount(a.secretConsumption, secrets) === 1 ? "" : "s"} no longer
                        resolve. Open the agent to fix.
                      </p>
                    ) : null}
                    <div className="agent-card__foot">
                      <span className="agent-card__stat">
                        {meta.count} run{meta.count === 1 ? "" : "s"}
                        {meta.last ? ` · last ${meta.last}` : ""}
                      </span>
                      <Button
                        variant="secondary" size="compact"
                        onClick={(e) => {
                          e.stopPropagation()
                          handleOpenNewTaskModal(a)
                        }}
                        disabled={!token}
                        aria-label={`Run ${a.name}`}
                      >
                        Run
                      </Button>
                    </div>
                  </article>
                )
              })}
            </div>
          )}
        </section>

        {!loading && agents.length > 0 ? (
          <aside className="agent-activity">
            <h2 className="agent-activity__title">Recent activity</h2>
            {recent.length === 0 ? (
              <p className="page-activity__empty">No runs yet.</p>
            ) : (
              <div className="agent-activity__feed">
                {recent.map(({ task, agent }) => {
                  const ui = apiTaskToTask(task)
                  return (
                    <button
                      key={task.id}
                      type="button"
                      className="agent-activity__row"
                      onClick={() => navigate({ name: "task", spaceId, taskId: task.id })}
                    >
                      <div className="agent-activity__body">
                        <span className="agent-activity__row-title">{ui.title}</span>
                        <span className="agent-activity__row-sub">{agent.name}</span>
                      </div>
                      <span className={`agent-activity__status agent-activity__status--${runStatusTone(task.status)}`}>
                        {runStatusLabel(task.status)}
                      </span>
                      <span className="agent-activity__time">{ui.timeLabel}</span>
                    </button>
                  )
                })}
              </div>
            )}
          </aside>
        ) : null}
      </div>

      <CreateAgentModal
        open={modalOpen}
        loading={creating}
        error={error}
        secrets={secrets}
        availablePlugins={availablePlugins}
        availableModels={availableModels}
        onClose={() => {
          setModalOpen(false)
          setError(null)
        }}
        onCreate={handleCreateAgent}
      />

      <RunAgentModal
        open={newTaskAgent != null}
        agent={newTaskAgent}
        loading={startingTaskAgentId !== null}
        error={newTaskAgent != null ? error : null}
        onClose={() => {
          setNewTaskAgent(null)
          if (newTaskAgent) setError(null)
        }}
        onStart={handleStartTaskFromAgent}
      />
    </div>
  )
}
