import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { Avatar, BaseModal, Button, ButtonLink, ChatComposer, ChatThread, type ChatThreadItem } from "@buildmax/gui"
import { AgentAvatar, UserAvatar } from "../../components/UserAvatar"
import { useApp } from "../../contexts/AppContext"
import { useAuth } from "../../contexts/AuthContext"
import { cancelTask, continueTask, getTask, getTaskRuns, retryTask, streamTaskOutput } from "../../features/tasks"
import { getAgent } from "../../features/agents"
import { RunTraceModal, runInputLabel } from "../../features/runs"
import { runStatusLabel } from "../../features/conversations/thread"
import { buildHash, navigate } from "../../router"
import type { ApiTask, ApiTaskRun } from "../../lib/api/types"
import type { BreadcrumbCrumb } from "../../lib/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { ApiRequestError } from "../../lib/api/client"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"
import { statusLabel } from "../../lib/statusLabels"

interface TaskDetailProps {
  token: string | null
  spaceId: string
  taskId: string
}

const activeStatuses = new Set(["PENDING", "SCHEDULED", "RUNNING"])

function fmtDuration(start?: string | null, end?: string | null): string {
  if (!start || !end) return "—"
  const ms = new Date(end).getTime() - new Date(start).getTime()
  if (!Number.isFinite(ms) || ms < 0) return "—"
  const s = Math.round(ms / 1000)
  const m = Math.floor(s / 60)
  return m > 0 ? `${m}m ${s % 60}s` : `${s}s`
}

function fmtWhen(ts?: string | null): string {
  if (!ts) return "—"
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString()
}

/** A working indicator shown while a run is still in flight, in place of the
 *  raw pending/scheduled/running status the user does not need to see. */
function TypingDots() {
  return (
    <span className="typing-dots" role="status" aria-label="Agent is working">
      <span />
      <span />
      <span />
    </span>
  )
}

export function TaskDetail({ token, spaceId, taskId }: TaskDetailProps) {
  const { user } = useAuth()
  const { entityLabels, setEntityLabel, setBreadcrumbTrail } = useApp()
  const historyRef = useRef<HTMLElement | null>(null)
  const [task, setTask] = useState<ApiTask | null>(null)
  const [runs, setRuns] = useState<ApiTaskRun[]>([])
  const [agentName, setAgentName] = useState<string | null>(null)
  const [input, setInput] = useState("")
  const [loading, setLoading] = useState(true)
  const [sending, setSending] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [unavailable, setUnavailable] = useState<ResourceUnavailableKind | null>(null)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [traceRunId, setTraceRunId] = useState<string | null>(null)
  const [streamingText, setStreamingText] = useState("")

  const load = useCallback(async (background = false) => {
    if (!token || !spaceId) return
    try {
      const [nextTask, nextRuns] = await Promise.all([
        getTask(spaceId, taskId, token),
        getTaskRuns(spaceId, taskId, token),
      ])
      setTask(nextTask)
      setRuns(nextRuns)
      setError(null)
      setUnavailable(null)
    } catch (err) {
      // A poll's failure is transient by nature (the task was readable a
      // moment ago) -- it must not bounce a reader watching a live run to a
      // not-found page. Only the initial load classifies the error.
      if (!background) {
        if (err instanceof ApiRequestError && err.status === 404) {
          setUnavailable("notFound")
        } else if (err instanceof ApiRequestError && err.status === 403) {
          setUnavailable("forbidden")
        } else {
          setUnavailable("error")
        }
        setTask(null)
        setError(getErrorMessage(err, "Failed to load task"))
      }
    } finally {
      setLoading(false)
    }
  }, [spaceId, taskId, token])

  useEffect(() => {
    setLoading(true)
    void load()
  }, [load])

  const running = runs.some((run) => activeStatuses.has(run.status))
  useEffect(() => {
    if (!running) return
    const timer = window.setInterval(() => void load(true), 1500)
    return () => window.clearInterval(timer)
  }, [load, running])

  // Live output for the in-flight run over SSE, layered on top of the poll,
  // which owns run lifecycle and status. The stream carries only output deltas,
  // a `done` sentinel, and a `draining` event; on any of those or an error the
  // page reloads for the terminal record and the poll carries on. The stream is
  // best-effort liveness, never the source of truth.
  useEffect(() => {
    if (!token || !spaceId || !running) return
    const ac = new AbortController()
    setStreamingText("")
    void streamTaskOutput(
      spaceId,
      taskId,
      token,
      {
        onDelta: (delta) => setStreamingText((text) => text + delta),
        onDone: () => {
          setStreamingText("")
          void load(true)
        },
        onDraining: () => {
          setStreamingText("")
          void load(true)
        },
        onError: () => setStreamingText(""),
      },
      { signal: ac.signal }
    )
    return () => ac.abort()
  }, [token, spaceId, taskId, running, load])

  useEffect(() => {
    historyRef.current?.scrollTo({ top: historyRef.current.scrollHeight, behavior: "smooth" })
  }, [runs])

  // Resolve the agent's name for the header link and details panel, and share it
  // so this task's breadcrumb reads the name too.
  useEffect(() => {
    if (!token || !spaceId || !task?.agent_id) {
      setAgentName(null)
      return
    }
    getAgent(spaceId, task.agent_id, token)
      .then((a) => setAgentName(a.name))
      .catch(() => setAgentName(null))
  }, [token, spaceId, task?.agent_id])

  useEffect(() => {
    if (task?.agent_id && agentName) setEntityLabel(task.agent_id, agentName)
  }, [task?.agent_id, agentName, setEntityLabel])

  // Origin-first breadcrumb: an Issue or Conversation is where this task came
  // from and outranks the Agent, which only describes what executed it — the
  // Agent gets its own link elsewhere (header, Details panel), never the
  // primary trail. A workflow-step task carries neither on the Task itself,
  // so its origin is the workflow run the server resolved for it. Only a task
  // with no recorded origin at all (a direct agent run) shows the Agent here,
  // because that genuinely is where it came from.
  useEffect(() => {
    if (!task) return
    const leaf: BreadcrumbCrumb = { label: task.title || "Task", route: { name: "task", spaceId, taskId } }
    let trail: BreadcrumbCrumb[]
    if (task.issue_id) {
      trail = [
        { label: "Issues", route: { name: "issues", spaceId } },
        { label: entityLabels[task.issue_id] ?? "Issue", route: { name: "issue", spaceId, issueId: task.issue_id } },
        leaf,
      ]
    } else if (task.conversation_id) {
      trail = [
        { label: "Chat", route: { name: "chat", spaceId } },
        { label: "Conversation", route: { name: "chat", spaceId, conversationId: task.conversation_id } },
        leaf,
      ]
    } else if (task.workflow_run_id) {
      trail = [
        { label: "Workflows", route: { name: "workflows", spaceId } },
        { label: "Workflow run", route: { name: "workflowRun", spaceId, workflowRunId: task.workflow_run_id } },
        leaf,
      ]
    } else if (task.agent_id) {
      trail = [
        { label: "Agents", route: { name: "agents", spaceId } },
        { label: entityLabels[task.agent_id] ?? "Agent", route: { name: "agent", spaceId, agentId: task.agent_id } },
        leaf,
      ]
    } else {
      trail = [{ label: "Chat", route: { name: "chat", spaceId } }, leaf]
    }
    setBreadcrumbTrail(taskId, trail)
  }, [task, taskId, spaceId, entityLabels, setBreadcrumbTrail])

  // The task rendered as a conversation: each run is one user turn (its input)
  // and one agent turn (its output). Run-level technical detail lives in the
  // task Details panel, not under every message.
  const items = useMemo<ChatThreadItem[]>(() => {
    return runs.flatMap((run) => {
      const active = activeStatuses.has(run.status)
      const inputLabel = runInputLabel(run)
      return [
        {
          id: `${run.id}-input`,
          role: "user",
          label: inputLabel,
          avatar:
            inputLabel === "You" && user ? (
              <UserAvatar user={user} size="sm" />
            ) : (
              <Avatar label={inputLabel.slice(0, 1)} size="sm" />
            ),
          body: (
            <div className="page-chat__msg-content page-chat__markdown">
              <Markdown remarkPlugins={[remarkGfm]}>{run.input}</Markdown>
            </div>
          ),
        },
        {
          id: `${run.id}-output`,
          role: "assistant",
          // While a run is in flight the label stays "Agent" — the working dots
          // carry the state; only a finished run shows Done / Failed / Stopped.
          label: active ? "Agent" : `Agent · ${runStatusLabel(run.status)}`,
          avatar: <AgentAvatar size="sm" />,
          body: run.output ? (
            <div className="page-chat__msg-content page-chat__markdown">
              <Markdown remarkPlugins={[remarkGfm]}>{run.output}</Markdown>
            </div>
          ) : active ? (
            streamingText ? (
              <div className="page-chat__msg-content page-chat__markdown">
                <Markdown remarkPlugins={[remarkGfm]}>{streamingText}</Markdown>
              </div>
            ) : (
              <TypingDots />
            )
          ) : run.error_message ? (
            <p className="bm-chat-thread__text bm-chat-thread__text--muted">{run.error_message}</p>
          ) : (
            <p className="bm-chat-thread__text bm-chat-thread__text--muted">No output.</p>
          ),
        },
      ]
    })
  }, [runs, user, streamingText])

  async function handleContinue() {
    const message = input.trim()
    if (!message || !token || !spaceId || sending || running) return
    setSending(true)
    setError(null)
    try {
      // Generated fresh per attempt: this call's own retry-on-401 reuses it, so
      // a token refresh cannot turn one Continue into two runs.
      const run = await continueTask(spaceId, taskId, message, token, crypto.randomUUID())
      setRuns((current) => [...current, run])
      setInput("")
    } catch (err) {
      setError(getErrorMessage(err, "Failed to continue task"))
    } finally {
      setSending(false)
    }
  }

  function handleStop() {
    if (!token || !spaceId || stopping || !running) return
    setStopping(true)
    setError(null)
    cancelTask(spaceId, taskId, token)
      .then(() => load(true))
      .catch((err) => setError(getErrorMessage(err, "Failed to stop this run")))
      .finally(() => setStopping(false))
  }

  function handleRetry() {
    if (!token || !spaceId || retrying || running) return
    setRetrying(true)
    setError(null)
    retryTask(spaceId, taskId, token)
      .then(() => load(true))
      .catch((err) => setError(getErrorMessage(err, "Failed to retry this run")))
      .finally(() => setRetrying(false))
  }

  const traceRun = task?.last_run_id ?? runs[runs.length - 1]?.id ?? null

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
        resourceLabel="Task"
        kind={unavailable}
        errorMessage={error}
        onRetry={() => void load()}
        backLabel="Back to Chat"
        onBack={() => navigate({ name: "chat", spaceId })}
      />
    )
  }

  return (
    <div className="page-chat task-thread">
      <header className="task-thread__header">
        <div>
          <h1 className="page-activity__title">{task?.title || "Task"}</h1>
          <p className="page-activity__subtitle">
            {agentName ? `${agentName} · ` : ""}
            {task ? runStatusLabel(task.status) : "Loading"}
          </p>
        </div>
        <div className="task-thread__header-actions">
          {running ? (
            <Button variant="danger" busy={stopping} onClick={handleStop}>Stop</Button>
          ) : runs.length > 0 ? (
            <Button variant="secondary" busy={retrying} onClick={handleRetry}>Retry last run</Button>
          ) : null}
          <Button variant="tertiary" aria-haspopup="dialog" onClick={() => setDetailsOpen(true)}>
            Details
          </Button>
          {task?.agent_id ? (
            <ButtonLink variant="tertiary" href={buildHash({ name: "agent", spaceId, agentId: task.agent_id })}>
              Open agent
            </ButtonLink>
          ) : null}
        </div>
      </header>

      {task ? (
        <BaseModal
          open={detailsOpen}
          title="Task details"
          titleId="task-details-title"
          onClose={() => setDetailsOpen(false)}
        >
          <div className="modal__body">
          <dl className="task-details__kv">
            <dt>Agent</dt>
            <dd>
              {task.agent_id ? (
                <a className="task-details__link" href={buildHash({ name: "agent", spaceId, agentId: task.agent_id })}>
                  {agentName ?? "Agent"}
                </a>
              ) : (
                "—"
              )}
            </dd>
            <dt>Status</dt>
            <dd>{runStatusLabel(task.status)}</dd>
            <dt>Trigger</dt>
            <dd>{runs[0]?.trigger_source ? statusLabel(runs[0].trigger_source) : "—"}</dd>
            <dt>Started</dt>
            <dd>{fmtWhen(task.started_at)}</dd>
            <dt>Ended</dt>
            <dd>{fmtWhen(task.ended_at)}</dd>
            <dt>Duration</dt>
            <dd>{fmtDuration(task.started_at, task.ended_at)}</dd>
            <dt>Runs</dt>
            <dd>{runs.length}</dd>
            <dt>Task</dt>
            <dd className="task-details__mono">{task.id}</dd>
            {task.issue_id ? (
              <>
                <dt>Issue</dt>
                <dd>
                  <a className="task-details__link" href={buildHash({ name: "issue", spaceId, issueId: task.issue_id })}>
                    Open issue
                  </a>
                </dd>
              </>
            ) : null}
            {task.conversation_id ? (
              <>
                <dt>Conversation</dt>
                <dd>
                  <a className="task-details__link" href={buildHash({ name: "chat", spaceId, conversationId: task.conversation_id })}>
                    Open conversation
                  </a>
                </dd>
              </>
            ) : null}
          </dl>
          <div className="task-details__actions">
            {traceRun ? (
              <Button
                variant="tertiary"
                size="compact"
                onClick={() => {
                  setDetailsOpen(false)
                  setTraceRunId(traceRun)
                }}
              >
                View trace
              </Button>
            ) : null}
          </div>
          </div>
        </BaseModal>
      ) : null}

      <ChatThread
        historyRef={historyRef}
        ariaLabel="Task conversation"
        items={items}
        loadingText={loading ? "Loading conversation…" : null}
        errorText={error}
        emptyText="No runs yet."
      />
      <section className="page-chat__input" aria-label="Continue task">
        <ChatComposer
          value={input}
          onChange={setInput}
          onSubmit={handleContinue}
          loading={sending}
          disabled={running || !task?.agent_id}
          error={error}
          placeholder={running ? "Wait for the current run to finish…" : "Continue this task…"}
          ariaLabel="Continue task"
          submitLabel="Continue"
        />
      </section>

      <RunTraceModal
        open={traceRunId != null}
        spaceId={spaceId}
        token={token}
        taskRunId={traceRunId}
        onClose={() => setTraceRunId(null)}
      />
    </div>
  )
}
