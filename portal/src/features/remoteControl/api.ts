import { apiFetch, getApiBase, parseErrorResponse, requestJson, throwIfNotOk } from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import { readSSEStream } from "../../lib/api/sse"
import { detectLocale, type Question, type QuestionAnswer } from "@buildmax/gui"
import { translate, type MessageKey } from "../../i18n"

/** One live, device-resident session the signed-in user has made reachable. */
export interface RemoteSession {
  id: string
  display_name?: string
  platform?: string
  host?: string
  status: "online" | "offline"
  created_at: string
  last_seen_at?: string
}

interface RemoteSessionsResponse {
  sessions: RemoteSession[]
}

/** List the caller's Remote Control sessions with their presence. */
export async function listRemoteSessions(token: string): Promise<RemoteSession[]> {
  const res = await requestJson<RemoteSessionsResponse>(`${getApiBase()}/api/remote-control/sessions`, {
    headers: authHeaders(token),
  })
  return res.sessions ?? []
}

/** One frame on a session's approval stream: a pending prompt or its dismissal. */
export interface ApprovalFrame {
  id: string
  tool?: string
  summary?: string
  resolved?: boolean
}

interface FrameStreamCallbacks<T> {
  onFrame: (frame: T) => void
  onDone: () => void
  onError: (err: Error) => void
  onDraining?: () => void
}

/**
 * Read one of a session's prompt streams. Each SSE payload carries one or more
 * newline-separated JSON frames; a frame with resolved=true dismisses the one
 * with the same id.
 */
async function streamPromptFrames<T>(
  sessionId: string,
  stream: "approval-stream" | "question-stream",
  failure: MessageKey,
  token: string,
  callbacks: FrameStreamCallbacks<T>,
  options?: { signal?: AbortSignal }
): Promise<void> {
  const url = `${getApiBase()}/api/remote-control/sessions/${encodeURIComponent(sessionId)}/${stream}`
  const res = await apiFetch(url, { headers: authHeaders(token), signal: options?.signal })
  if (!res.ok) {
    callbacks.onError(new Error(await parseErrorResponse(res, translate(detectLocale(), failure))))
    return
  }
  await readSSEStream(res, {
    onData: (data) => {
      if (data === "done") {
        callbacks.onDone()
        return false
      }
      for (const line of data.split("\n")) {
        if (!line.trim()) continue
        try {
          callbacks.onFrame(JSON.parse(line) as T)
        } catch {
          // A malformed frame is ignored rather than breaking the stream.
        }
      }
    },
    onEvent: (name) => {
      if (name === "draining") {
        callbacks.onDraining?.()
        return false
      }
    },
    onDone: callbacks.onDone,
    onError: callbacks.onError,
  })
}

/** Stream a session's pending tool-approval prompts. */
export function streamRemoteApprovals(
  sessionId: string,
  token: string,
  callbacks: FrameStreamCallbacks<ApprovalFrame>,
  options?: { signal?: AbortSignal }
): Promise<void> {
  return streamPromptFrames(sessionId, "approval-stream", "remote.error.approvalStream", token, callbacks, options)
}

/** One frame on a session's question stream: a pending AskUser set or its dismissal. */
export interface QuestionFrame {
  id: string
  questions?: Question[]
  resolved?: boolean
}

/** Stream a session's pending AskUser question sets. */
export function streamRemoteQuestions(
  sessionId: string,
  token: string,
  callbacks: FrameStreamCallbacks<QuestionFrame>,
  options?: { signal?: AbortSignal }
): Promise<void> {
  return streamPromptFrames(sessionId, "question-stream", "remote.error.questionStream", token, callbacks, options)
}

/** Answer a pending AskUser question set: one answer per question, or a dismissal. */
export async function respondRemoteQuestion(
  sessionId: string,
  id: string,
  answer: QuestionAnswer,
  token: string
): Promise<void> {
  const body = "declined" in answer ? { id, declined: true } : { id, answers: answer.answers }
  const res = await apiFetch(
    `${getApiBase()}/api/remote-control/sessions/${encodeURIComponent(sessionId)}/question`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) }, body: JSON.stringify(body) }
  )
  await throwIfNotOk(res)
}

/** Ask a live session to stop its current run. */
export async function cancelRemoteSession(sessionId: string, token: string): Promise<void> {
  const res = await apiFetch(
    `${getApiBase()}/api/remote-control/sessions/${encodeURIComponent(sessionId)}/cancel`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) } }
  )
  await throwIfNotOk(res)
}

/** Answer a pending tool-approval on a live session. */
export async function respondRemoteApproval(
  sessionId: string,
  id: string,
  decision: "once" | "session" | "deny",
  token: string
): Promise<void> {
  const res = await apiFetch(
    `${getApiBase()}/api/remote-control/sessions/${encodeURIComponent(sessionId)}/approval`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) }, body: JSON.stringify({ id, decision }) }
  )
  await throwIfNotOk(res)
}

/**
 * Send a follow-up prompt to a live session. Best-effort: the server delivers it
 * to the machine, which enqueues it into the running turn or starts a new one.
 */
export async function sendRemotePrompt(sessionId: string, content: string, token: string): Promise<void> {
  const res = await apiFetch(
    `${getApiBase()}/api/remote-control/sessions/${encodeURIComponent(sessionId)}/prompt`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) }, body: JSON.stringify({ content }) }
  )
  await throwIfNotOk(res)
}

/**
 * Stream a session's relayed output over server-sent events: buffered replay
 * then live deltas until the session ends. Read-only — the same shape the Task
 * output stream uses, so `done` ends it and `draining` means reopen elsewhere.
 */
export async function streamRemoteSession(
  sessionId: string,
  token: string,
  callbacks: {
    onDelta: (delta: string) => void
    onDone: () => void
    onError: (err: Error) => void
    onDraining?: () => void
  },
  options?: { signal?: AbortSignal }
): Promise<void> {
  const url = `${getApiBase()}/api/remote-control/sessions/${encodeURIComponent(sessionId)}/stream`
  const res = await apiFetch(url, { headers: authHeaders(token), signal: options?.signal })
  if (!res.ok) {
    callbacks.onError(new Error(await parseErrorResponse(res, translate(detectLocale(), "remote.error.stream"))))
    return
  }
  await readSSEStream(res, {
    onData: (data) => {
      if (data === "done") {
        callbacks.onDone()
        return false
      }
      callbacks.onDelta(data)
    },
    onEvent: (name) => {
      if (name === "draining") {
        callbacks.onDraining?.()
        return false
      }
    },
    onDone: callbacks.onDone,
    onError: callbacks.onError,
  })
}
