import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, CollectionFrame, PageHeader, StatusLabel } from "@buildmax/gui"
import type { Agent } from "../../lib/types"
import type { ApiTask } from "../../lib/api/types"
import { navigate } from "../../router"
import { getErrorMessage } from "../../lib/errorMessage"
import { apiAgentToAgent, apiTaskToTask } from "../../lib/api/mappers"
import { createAgentTask, listAgentTasks, taskLabel } from "../../features/tasks"
import { formatSuccessRate, getAgents, successRate as rateOf } from "../../features/agents"
import { taskRunFinished, taskStatus } from "../../features/conversations/thread"
import { AgentAvatar } from "../../components/UserAvatar"
import { CreateAgentModal } from "../../components/CreateAgentModal"
import { RunAgentModal } from "../../components/RunAgentModal"
import { useSpace, useSpaceCapability } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed } from "../../state/permissionState"
import { useStableT, useT } from "../../i18n"
import { useRelativeTime } from "../../lib/dateFormat"

interface AgentListProps {
  token: string | null
  spaceId: string
}

// Stable identity so `agents` doesn't churn every render while agentsData is
// still null (before the first successful fetch).
const EMPTY_AGENTS: Agent[] = []

export function AgentList({ token, spaceId }: AgentListProps) {
  const { currentUserRole } = useSpace()
  const t = useT()
  const relativeTime = useRelativeTime()
  const stableT = useStableT()
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no agents. See deriveResourceState.
  const [agentsData, setAgentsData] = useState<Agent[] | null>(null)
  const agents = agentsData ?? EMPTY_AGENTS
  const [tasksByAgent, setTasksByAgent] = useState<Record<string, ApiTask[]>>({})
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [newTaskAgent, setNewTaskAgent] = useState<Agent | null>(null)
  const [startingTaskAgentId, setStartingTaskAgentId] = useState<string | null>(null)
  // Distinct from listError: the run-agent mutation's own error, shown inside
  // its modal.
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
      .catch((err) => setListError(classifyError(err, stableT("agents.error.load"))))
      .finally(() => setLoading(false))
  }, [token, spaceId, stableT])

  const agentsState = useMemo(
    () => deriveResourceState({ loading, data: agentsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, agentsData, listError]
  )

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
    const running = tasks.filter((task) => !taskRunFinished(task.status)).length
    const successRate = formatSuccessRate(rateOf(tasks.map((task) => task.status)))
    const warnings = canManageAgents
      ? agents.reduce((n, a) => n + (a.secretGrantProblems?.length ?? 0), 0)
      : 0
    return { total: tasks.length, running, successRate, warnings }
  }, [allTasks, agents, canManageAgents])

  const recent = useMemo(
    () => [...allTasks].sort((a, b) => b.task.created_at.localeCompare(a.task.created_at)).slice(0, 6),
    [allTasks],
  )

  function agentMeta(agent: Agent) {
    const ts = tasksByAgent[agent.id] ?? []
    return {
      count: ts.length,
      running: ts.some((task) => !taskRunFinished(task.status)),
      last: ts[0] ? relativeTime(apiTaskToTask(ts[0]).timeAt) : null,
    }
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
        setError(getErrorMessage(err, t("agents.error.run")))
      })
      .finally(() => setStartingTaskAgentId(null))
  }

  const kpis: { label: string; value: string | number; show: boolean }[] = [
    { label: t("agents.kpi.agents"), value: agents.length, show: true },
    { label: t("agents.kpi.running"), value: stats.running, show: true },
    { label: t("agents.kpi.totalRuns"), value: stats.total, show: true },
    { label: t("agents.kpi.successRate"), value: stats.successRate, show: true },
    { label: t("agents.kpi.warnings"), value: stats.warnings, show: canManageAgents },
  ]

  return (
    <div className="page-activity">
      <PageHeader
        title={t("agents.title")}
        subtitle={t("agents.subtitle")}
        actions={
          canManageAgents ? (
            <Button
              variant="primary"
              className="agent-list__create-btn"
              onClick={() => setModalOpen(true)}
              aria-label={t("agents.create")}
            >
              {t("agents.create")}
            </Button>
          ) : null
        }
      />

      {(agentsState.kind === "error" ||
        agentsState.kind === "forbidden" ||
        agentsState.kind === "notFound" ||
        agentsState.kind === "stale") && (
        <Alert
          tone={agentsState.kind === "stale" ? "stale" : agentsState.kind}
          message={agentsState.error.message}
          retry={{ label: t("shell.retry"), onClick: () => fetchAgents() }}
        />
      )}
      {error ? <p className="page-activity__empty">{error}</p> : null}

      {canManageAgentsState === "denied" ? (
        <p className="page-activity__empty">{t("agents.role.denied")}</p>
      ) : canManageAgentsState === "failed" ? (
        <p className="page-activity__empty">{t("agents.role.failed")}</p>
      ) : canManageAgentsState === "unknown" ? (
        <p className="page-activity__empty">{t("agents.role.unknown")}</p>
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
        <CollectionFrame
          className="agent-list"
          label={t("agents.list.label")}
          loading={agentsState.kind === "loading"}
          empty={
            agentsState.kind === "readyEmpty" ? (
              <EmptyState message={canManageAgents ? t("agents.empty.manager") : t("agents.empty.member")} />
            ) : null
          }
        >
          {agentsState.kind === "error" || agentsState.kind === "forbidden" || agentsState.kind === "notFound" ? null : (
            <div className="agent-list__grid">
              {agents.map((a) => {
                const meta = agentMeta(a)
                return (
                  <article key={a.id} className="agent-card">
                    <header className="agent-card__header">
                      <AgentAvatar size="md" className="agent-card__avatar" />
                      <div className="agent-card__title-row">
                        <h3 className="agent-card__name">
                          {/* A real button whose hit area stretches over the
                              card, so the card stays one click target without
                              nesting the Run button inside another control. */}
                          <button
                            type="button"
                            className="agent-card__open"
                            aria-label={t("agents.openNamed", { name: a.name })}
                            onClick={() => navigate({ name: "agent", spaceId, agentId: a.id })}
                          >
                            {a.name}
                          </button>
                        </h3>
                        {meta.running ? (
                          <span className="agent-card__running">{t("agents.running")}</span>
                        ) : (
                          <span className="agent-card__edit-hint" aria-hidden>{t("agents.open")}</span>
                        )}
                      </div>
                    </header>
                    {a.description ? (
                      <p className="agent-card__description">{a.description}</p>
                    ) : null}
                    {a.secretGrantProblems?.length ? (
                      <p className="agent-card__secret-warning" role="alert">
                        ⚠ {t(canManageAgents ? "agents.card.secretWarning" : "agents.card.secretWarningMember", { count: a.secretGrantProblems.length })}
                      </p>
                    ) : null}
                    <div className="agent-card__foot">
                      <span className="agent-card__stat">
                        {t("agents.card.runs", { count: meta.count })}
                        {meta.last ? t("agents.card.lastRun", { when: meta.last }) : ""}
                      </span>
                      <Button
                        variant="secondary" size="compact"
                        onClick={() => handleOpenNewTaskModal(a)}
                        disabled={!token}
                        aria-label={t("agents.runNamed", { name: a.name })}
                      >
                        {t("agents.run")}
                      </Button>
                    </div>
                  </article>
                )
              })}
            </div>
          )}
        </CollectionFrame>

        {!loading && agents.length > 0 ? (
          <aside className="agent-activity">
            <h2 className="agent-activity__title">{t("agents.activity.title")}</h2>
            {recent.length === 0 ? (
              <p className="page-activity__empty">{t("agents.activity.empty")}</p>
            ) : (
              <div className="agent-activity__feed">
                {recent.map(({ task, agent }) => {
                  const ui = apiTaskToTask(task)
                  const label = taskLabel(ui, t)
                  return (
                    <button
                      key={task.id}
                      type="button"
                      className="agent-activity__row"
                      onClick={() => navigate({ name: "task", spaceId, taskId: task.id })}
                    >
                      <div className="agent-activity__body">
                        <span className="agent-activity__row-title">{label.primary}</span>
                        <span className="agent-activity__row-sub">
                          {[agent.name, label.secondary].filter(Boolean).join(" · ")}
                        </span>
                      </div>
                      <StatusLabel status={taskStatus(task)} />
                      <span className="agent-activity__time">{relativeTime(ui.timeAt)}</span>
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
        token={token}
        spaceId={spaceId}
        onClose={() => setModalOpen(false)}
        onCreated={(created) => {
          setAgentsData((prev) => [...(prev ?? []), created])
          setModalOpen(false)
          navigate({ name: "agent", spaceId, agentId: created.id })
        }}
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
