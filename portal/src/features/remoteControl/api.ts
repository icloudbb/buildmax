import { apiFetch, getApiBase, parseErrorResponse, requestJson } from "../../lib/api/client"
import { authHeaders } from "../../lib/api/common"
import { readSSEStream } from "../../lib/api/sse"

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
    callbacks.onError(new Error(await parseErrorResponse(res, "Remote Control stream failed")))
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
