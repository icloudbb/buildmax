import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import {
  Avatar,
  BaseModal,
  Button,
  ButtonLink,
  ChatComposer,
  ChatThread,
  type ChatThreadItem,
  DetailHeader,
  Skeleton,
  Spinner,
  StatusLabel,
  useStatusLabel,
} from "@buildmax/gui"
import { AgentAvatar, UserAvatar } from "../../components/UserAvatar"
import { useApp } from "../../contexts/AppContext"
import { useAuth } from "../../contexts/AuthContext"
import {
  cancelTask,
  continueTask,
  getTask,
  getTaskRuns,
  originKindLabel,
  originLabel,
  originName,
  originRoute,
  retryTask,
  streamTaskOutput,
  taskLabel,
} from "../../features/tasks"
import { apiTaskToTask } from "../../lib/api/mappers"
import { CopyableId } from "../../components/CopyableId"
import { getAgent } from "../../features/agents"
import {
  explainRunFailure,
  failureText,
  FailureFixLink,
  RunFailureNotice,
  RunTraceModal,
  runInputLabel,
  useCanManageSpace,
  useSecretName,
} from "../../features/runs"
import { taskStatus } from "../../features/conversations/thread"
import { buildHash, navigate } from "../../router"
import type { ApiTask, ApiTaskRun } from "../../lib/api/types"
import type { BreadcrumbCrumb } from "../../lib/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { ApiRequestError } from "../../lib/api/client"
import { ResourceUnavailable, type ResourceUnavailableKind } from "../../components/ResourceUnavailable"
import { useStableT, useT } from "../../i18n"
import { useTimestamp } from "../../lib/dateFormat"

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

/** A working indicator shown while a run is still in flight, in place of the
 *  raw pending/scheduled/running status the user does not need to see. */
function TypingDots() {
  const t = useT()
  return (
    <span className="typing-dots" role="status" aria-label={t("tasks.working")}>
      <span />
      <span />
      <span />
    </span>
  )
}

export function TaskDetail({ token, spaceId, taskId }: TaskDetailProps) {
  const t = useT()
  const fmtWhen = useTimestamp()
  const stableT = useStableT()
  const statusLabel = useStatusLabel()
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
        setError(getErrorMessage(err, stableT("tasks.error.load")))
      }
    } finally {
      setLoading(false)
    }
  }, [spaceId, taskId, token, stableT])

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

  // What the task is called: its origin's name for an Issue, a Workflow step,
  // or a Schedule, with its own generated title secondary.
  const label = useMemo(() => (task ? taskLabel(apiTaskToTask(task), t) : null), [task, t])

  // Origin-first breadcrumb: the server names what the task serves, so the
  // trail reads the same on a reload as when it was reached from its origin.
  // The origin outranks the Agent, which only describes what executed it —
  // the Agent gets its own link elsewhere (header, Details panel). Only a task
  // with no recorded origin at all (a direct agent run) shows the Agent here,
  // because that genuinely is where it came from.
  // The trail is display state, not a load, so it follows `t` and is rebuilt
  // in the new language when the language changes.
  useEffect(() => {
    if (!task || !label) return
    const leaf: BreadcrumbCrumb = { label: label.primary, route: { name: "task", spaceId, taskId } }
    const origin = task.origin
    let trail: BreadcrumbCrumb[]
    if (origin?.kind === "schedule") {
      trail = [{ label: t("shell.nav.schedules"), route: { name: "schedules", spaceId } }, leaf]
    } else if (origin) {
      const collection: BreadcrumbCrumb =
        origin.kind === "workflow"
          ? { label: t("shell.nav.workflows"), route: { name: "workflows", spaceId } }
          : origin.kind === "issue"
            ? { label: t("shell.nav.issues"), route: { name: "issues", spaceId } }
            : { label: t("shell.nav.chat"), route: { name: "chat", spaceId } }
      trail = [collection, { label: originName(origin, t), route: originRoute(origin, spaceId) }, leaf]
    } else if (task.agent_id) {
      trail = [
        { label: t("shell.nav.agents"), route: { name: "agents", spaceId } },
        { label: entityLabels[task.agent_id] ?? t("shell.crumbs.agent"), route: { name: "agent", spaceId, agentId: task.agent_id } },
        leaf,
      ]
    } else {
      trail = [{ label: t("shell.nav.chat"), route: { name: "chat", spaceId } }, leaf]
    }
    setBreadcrumbTrail(taskId, trail)
  }, [task, label, taskId, spaceId, entityLabels, setBreadcrumbTrail, t])

  // A failed run is explained in the person's terms, and the latest one decides
  // the header's lead: a fix the person can make outranks a retry that would
  // fail the same way.
  const lastRun = runs[runs.length - 1] ?? null
  const canManage = useCanManageSpace()
  const secretName = useSecretName(spaceId, token, lastRun?.failure_cause?.secret_id)
  const explainRun = useCallback(
    (run: ApiTaskRun) =>
      explainRunFailure(
        {
          status: run.status,
          failureClass: run.failure_class,
          failureCause: run.failure_cause,
          agentId: task?.agent_id,
          errorMessage: run.error_message,
          endedAt: run.ended_at,
        },
        {
          agent: agentName,
          secret: run.failure_cause?.secret_id === lastRun?.failure_cause?.secret_id ? secretName : null,
          canManage,
        },
      ),
    [task?.agent_id, agentName, secretName, lastRun?.failure_cause?.secret_id, canManage],
  )
  const lastFailure = lastRun ? explainRun(lastRun) : null

  // The task rendered as a conversation: each run is one user turn (its input)
  // and one agent turn (its output). Run-level technical detail lives in the
  // task Details panel, not under every message.
  const items = useMemo<ChatThreadItem[]>(() => {
    return runs.flatMap((run) => {
      const active = activeStatuses.has(run.status)
      const failure = explainRun(run)
      const inputLabel = runInputLabel(run, t)
      return [
        {
          id: `${run.id}-input`,
          role: "user",
          label: inputLabel,
          avatar:
            inputLabel === t("runs.input.you") && user ? (
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
          // carry the state; only a finished run shows its status.
          label: active
            ? t("tasks.agent")
            : `${t("tasks.agent")} · ${statusLabel(run.questions?.length ? "needs_answer" : run.status)}`,
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
          ) : failure ? (
            <RunFailureNotice explanation={failure} />
          ) : run.error_message ? (
            <p className="bm-chat-thread__text bm-chat-thread__text--muted">{run.error_message}</p>
          ) : run.status === "SUCCEEDED" ? (
            // A run can finish its work in tool calls and end without a word;
            // "No output" would read as if it did nothing.
            <p className="bm-chat-thread__text bm-chat-thread__text--muted">
              {t("tasks.noReply")}{" "}
              <Button variant="tertiary" size="compact" onClick={() => setTraceRunId(run.id)}>
                {t("tasks.seeWhatItDid")}
              </Button>
            </p>
          ) : (
            <p className="bm-chat-thread__text bm-chat-thread__text--muted">{t("tasks.noOutput")}</p>
          ),
        },
      ]
    })
  }, [runs, user, streamingText, t, statusLabel, explainRun])

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
      setError(getErrorMessage(err, stableT("tasks.error.continue")))
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
      .catch((err) => setError(getErrorMessage(err, stableT("tasks.error.stop"))))
      .finally(() => setStopping(false))
  }

  function handleRetry() {
    if (!token || !spaceId || retrying || running) return
    setRetrying(true)
    setError(null)
    retryTask(spaceId, taskId, token)
      .then(() => load(true))
      .catch((err) => setError(getErrorMessage(err, stableT("tasks.error.retry"))))
      .finally(() => setRetrying(false))
  }

  const traceRun = task?.last_run_id ?? runs[runs.length - 1]?.id ?? null

  if (loading) {
    return (
      <div className="page-activity">
        <Skeleton />
      </div>
    )
  }

  if (unavailable) {
    return (
      <ResourceUnavailable
        resourceLabel={t("tasks.resource")}
        kind={unavailable}
        errorMessage={error}
        onRetry={() => void load()}
        backLabel={t("tasks.backToChat")}
        onBack={() => navigate({ name: "chat", spaceId })}
      />
    )
  }

  return (
    <div className="page-chat task-thread">
      <DetailHeader
        className="task-thread__header"
        title={label?.primary ?? t("tasks.untitled")}
        status={task ? <StatusLabel status={taskStatus(task)} /> : <Spinner />}
        meta={[label?.secondary, agentName].filter(Boolean).join(" · ") || null}
        actions={
          <>
            {running ? (
              <Button variant="danger" busy={stopping} onClick={handleStop}>{t("tasks.stop")}</Button>
            ) : null}
            {!running && lastFailure?.leadsWithFix ? (
              <FailureFixLink explanation={lastFailure} spaceId={spaceId} agentId={task?.agent_id} />
            ) : null}
            {!running && runs.length > 0 ? (
              <Button variant={lastFailure?.leadsWithFix ? "tertiary" : "secondary"} busy={retrying} onClick={handleRetry}>
                {t("tasks.retryLast")}
              </Button>
            ) : null}
            <Button variant="tertiary" aria-haspopup="dialog" onClick={() => setDetailsOpen(true)}>
              {t("tasks.details")}
            </Button>
            {task?.agent_id && lastFailure?.primary !== "fixAgent" && lastFailure?.primary !== "openAgent" ? (
              <ButtonLink variant="tertiary" href={buildHash({ name: "agent", spaceId, agentId: task.agent_id })}>
                {t("tasks.openAgent")}
              </ButtonLink>
            ) : null}
          </>
        }
      />

      {task ? (
        <BaseModal
          open={detailsOpen}
          title={t("tasks.details.title")}
          titleId="task-details-title"
          onClose={() => setDetailsOpen(false)}
        >
          <div className="modal__body">
          <dl className="task-details__kv">
            {task.origin ? (
              <>
                <dt>{t("tasks.details.origin")}</dt>
                <dd>
                  {originKindLabel(task.origin, t)}{" "}
                  <a className="task-details__link" href={buildHash(originRoute(task.origin, spaceId))}>
                    {originLabel(task.origin, t)}
                  </a>
                </dd>
              </>
            ) : null}
            <dt>{t("tasks.details.agent")}</dt>
            <dd>
              {task.agent_id ? (
                <a className="task-details__link" href={buildHash({ name: "agent", spaceId, agentId: task.agent_id })}>
                  {agentName ?? t("tasks.details.agent")}
                </a>
              ) : (
                "—"
              )}
            </dd>
            <dt>{t("tasks.details.status")}</dt>
            <dd><StatusLabel status={taskStatus(task)} /></dd>
            {lastFailure ? (
              <>
                <dt>{t("tasks.details.cause")}</dt>
                <dd>{failureText(lastFailure, t).title}</dd>
              </>
            ) : null}
            <dt>{t("tasks.details.trigger")}</dt>
            <dd>{runs[0]?.trigger_source ? statusLabel(runs[0].trigger_source) : "—"}</dd>
            <dt>{t("tasks.details.started")}</dt>
            <dd>{fmtWhen(task.started_at)}</dd>
            <dt>{t("tasks.details.ended")}</dt>
            <dd>{fmtWhen(task.ended_at)}</dd>
            <dt>{t("tasks.details.duration")}</dt>
            <dd>{fmtDuration(task.started_at, task.ended_at)}</dd>
            <dt>{t("tasks.details.runs")}</dt>
            <dd>{runs.length}</dd>
            {/* A Workflow step can carry its run's Issue without that Issue
                being its origin; it is still worth a link. */}
            {task.issue_id && task.origin?.kind !== "issue" ? (
              <>
                <dt>{t("tasks.details.issue")}</dt>
                <dd>
                  <a className="task-details__link" href={buildHash({ name: "issue", spaceId, issueId: task.issue_id })}>
                    {t("tasks.details.openIssue")}
                  </a>
                </dd>
              </>
            ) : null}
            <dt>{t("tasks.details.taskId")}</dt>
            <dd>
              <CopyableId label={t("tasks.details.taskId")} id={task.id} showLabel={false} />
            </dd>
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
                {t("tasks.details.viewTrace")}
              </Button>
            ) : null}
          </div>
          </div>
        </BaseModal>
      ) : null}

      <ChatThread
        historyRef={historyRef}
        ariaLabel={t("tasks.thread.label")}
        items={items}
        loadingText={loading ? t("tasks.thread.loading") : null}
        errorText={error}
        emptyText={t("tasks.thread.empty")}
      />
      <section className="page-chat__input" aria-label={t("tasks.continue.label")}>
        <ChatComposer
          value={input}
          onChange={setInput}
          onSubmit={handleContinue}
          loading={sending}
          disabled={running || !task?.agent_id}
          error={error}
          placeholder={
            running
              ? t("tasks.continue.waiting")
              : task?.awaiting_answer
                ? t("tasks.continue.answer")
                : t("tasks.continue.placeholder")
          }
          ariaLabel={t("tasks.continue.label")}
          submitLabel={t("tasks.continue.submit")}
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
