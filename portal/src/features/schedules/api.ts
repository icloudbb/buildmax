import {
  apiFetch,
  getApiBase,
  requestJson,
  throwIfNotOk,
} from "../../lib/api/client"
import { authHeaders, jsonHeaders } from "../../lib/api/common"
import type {
  ApiSchedule,
  ApiScheduleListResponse,
  ApiTasksListResponse,
  ApiWorkflowRunListResponse,
} from "../../lib/api/types"

const base = (spaceId: string) =>
  `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/schedules`
const one = (spaceId: string, scheduleId: string) =>
  `${base(spaceId)}/${encodeURIComponent(scheduleId)}`

export interface CreateScheduleBody {
  executor_kind: "agent" | "workflow"
  executor_id: string
  name?: string
  input: string
  cron_expr: string
  timezone: string
}

// UpdateScheduleBody is a partial patch: an omitted field is left unchanged.
export interface UpdateScheduleBody {
  name?: string
  input?: string
  cron_expr?: string
  timezone?: string
  enabled?: boolean
}

export async function listSchedules(spaceId: string, token: string): Promise<ApiScheduleListResponse> {
  return requestJson<ApiScheduleListResponse>(base(spaceId), { headers: authHeaders(token) })
}

export async function createSchedule(spaceId: string, body: CreateScheduleBody, token: string): Promise<ApiSchedule> {
  return requestJson<ApiSchedule>(base(spaceId), {
    method: "POST",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function updateSchedule(spaceId: string, scheduleId: string, body: UpdateScheduleBody, token: string): Promise<ApiSchedule> {
  return requestJson<ApiSchedule>(one(spaceId, scheduleId), {
    method: "PATCH",
    headers: { ...jsonHeaders, ...authHeaders(token) },
    body: JSON.stringify(body),
  })
}

export async function deleteSchedule(spaceId: string, scheduleId: string, token: string): Promise<void> {
  const res = await apiFetch(one(spaceId, scheduleId), { method: "DELETE", headers: authHeaders(token) })
  await throwIfNotOk(res)
}

// listScheduleTasks returns the tasks an agent schedule has triggered, newest first.
export async function listScheduleTasks(spaceId: string, scheduleId: string, token: string): Promise<ApiTasksListResponse> {
  return requestJson<ApiTasksListResponse>(`${one(spaceId, scheduleId)}/tasks`, { headers: authHeaders(token) })
}

// listScheduleRuns returns the workflow runs a workflow schedule has triggered, newest first.
export async function listScheduleRuns(spaceId: string, scheduleId: string, token: string): Promise<ApiWorkflowRunListResponse> {
  return requestJson<ApiWorkflowRunListResponse>(`${one(spaceId, scheduleId)}/runs`, { headers: authHeaders(token) })
}
