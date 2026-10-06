import {
  apiFetch,
  getApiBase,
  parseErrorResponse,
  requestJson,
  throwIfNotOk,
} from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import { readSSEStream } from "../../lib/api/sse"
import { detectLocale } from "@buildmax/gui"
import { translate } from "../../i18n"
import type {
  ApiTask,
  ApiTaskRun,
  ApiTaskRunsResponse,
  ApiTasksListResponse,
  CancelTaskResponse,
  RetryTaskResponse,
} from "../../lib/api/types"

export interface GetTasksPaginatedOptions {
  limit?: number
  offset?: number
  executedOnly?: boolean
}

export async function createAgentTask(spaceId: string, agentId: string, input: string, token: string): Promise<ApiTask> {
  return requestJson<ApiTask>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/agents/${encodeURIComponent(agentId)}/tasks`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) }, body: JSON.stringify({ input }) }
  )
}

export async function listAgentTasks(spaceId: string, agentId: string, token: string): Promise<ApiTasksListResponse> {
  return requestJson<ApiTasksListResponse>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/agents/${encodeURIComponent(agentId)}/tasks`,
    { headers: authHeaders(token) }
  )
}

export async function getTask(spaceId: string, taskId: string, token: string): Promise<ApiTask> {
  return requestJson<ApiTask>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/tasks/${encodeURIComponent(taskId)}`,
    { headers: authHeaders(token) }
  )
}

export async function getTaskRuns(spaceId: string, taskId: string, token: string): Promise<ApiTaskRun[]> {
  const response = await requestJson<ApiTaskRunsResponse>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/tasks/${encodeURIComponent(taskId)}/runs`,
    { headers: authHeaders(token) }
  )
  return response.runs
}

/**
 * Continue a task with a new input.
 *
 * idempotencyKey lets a caller that cannot tell whether an earlier attempt
 * landed retry safely: the server returns the run the first attempt created
 * instead of starting a second one. Optional so a caller with no retry logic
 * of its own is unaffected.
 */
export async function continueTask(
  spaceId: string,
  taskId: string,
  input: string,
  token: string,
  idempotencyKey?: string
): Promise<ApiTaskRun> {
  return requestJson<ApiTaskRun>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/tasks/${encodeURIComponent(taskId)}/runs`,
    {
      method: "POST",
      headers: { ...jsonHeaders, ...authHeaders(token) },
      body: JSON.stringify({ input, idempotency_key: idempotencyKey }),
    }
  )
}

export async function getTasks(
  spaceId: string,
  conversationId: string,
  token: string
): Promise<ApiTask[]> {
  return requestJson<ApiTask[]>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/conversations/${encodeURIComponent(conversationId)}/tasks`,
    { headers: authHeaders(token) }
  )
}

export async function getTasksPaginated(
  spaceId: string,
  conversationId: string,
  token: string,
  options?: GetTasksPaginatedOptions
): Promise<ApiTasksListResponse> {
  const params = new URLSearchParams()
  if (options?.limit != null) params.set("limit", String(options.limit))
  if (options?.offset != null) params.set("offset", String(options.offset))
  if (options?.executedOnly) params.set("executed_only", "true")
  const q = params.toString()
  const url = `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/conversations/${encodeURIComponent(conversationId)}/tasks${q ? `?${q}` : ""}`
  return requestJson<ApiTasksListResponse>(url, { headers: authHeaders(token) })
}

/**
 * Ask the server to stop the task's run.
 *
 * 409 means there was nothing to stop — the run finished between the page
 * rendering a Stop button and the click reaching the server — which is a state
 * the caller resolves by reloading, not an error worth showing.
 */
export async function cancelTask(
  spaceId: string,
  taskId: string,
  token: string
): Promise<CancelTaskResponse> {
  const res = await apiFetch(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/tasks/${encodeURIComponent(taskId)}/cancel`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) } }
  )
  if (res.status === 409) {
    const msg = await parseErrorResponse(res, translate(detectLocale(), "tasks.error.noRunInProgress"))
    throw new Error(msg)
  }
  await throwIfNotOk(res)
  return res.json() as Promise<CancelTaskResponse>
}

/**
 * Run the task's last run again, with the input that run carried.
 *
 * 409 covers three different states — a run already in flight, a task that has
 * never finished one, and a workflow step, which the workflow owns — so the
 * server's own reason is what the caller shows.
 */
export async function retryTask(
  spaceId: string,
  taskId: string,
  token: string
): Promise<RetryTaskResponse> {
  const res = await apiFetch(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/tasks/${encodeURIComponent(taskId)}/retry`,
    { method: "POST", headers: { ...jsonHeaders, ...authHeaders(token) } }
  )
  if (res.status === 409) {
    const msg = await parseErrorResponse(res, translate(detectLocale(), "tasks.error.cannotRetry"))
    throw new Error(msg)
  }
  await throwIfNotOk(res)
  return res.json() as Promise<RetryTaskResponse>
}

/**
 * Stream a task's live agent output over server-sent events.
 *
 * The endpoint carries output deltas for whichever run is currently active,
 * plus a `done` sentinel when the run finishes and a `draining` event when the
 * serving instance is shutting down. It does not carry run lifecycle or status
 * transitions, so the caller keeps polling for those and uses this only for the
 * in-flight output; on `done`, `draining`, or any error the caller reloads and
 * falls back to the poll.
 */
export async function streamTaskOutput(
  spaceId: string,
  taskId: string,
  token: string,
  callbacks: {
    onDelta: (delta: string) => void
    onDone: () => void
    onError: (err: Error) => void
    onDraining?: () => void
  },
  options?: { signal?: AbortSignal }
): Promise<void> {
  const url = `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/tasks/${encodeURIComponent(taskId)}/stream`
  const res = await apiFetch(url, { headers: authHeaders(token), signal: options?.signal })
  if (!res.ok) {
    callbacks.onError(new Error(await parseErrorResponse(res, translate(detectLocale(), "tasks.error.stream"))))
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
