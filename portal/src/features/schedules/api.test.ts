import { afterEach, describe, expect, it, vi } from "vitest"
import {
  createSchedule,
  deleteSchedule,
  listScheduleRuns,
  listScheduleTasks,
  listSchedules,
  updateSchedule,
} from "./api"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("listSchedules", () => {
  it("gets the space's schedules with the caller's token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { schedules: [], total: 0 }))
    vi.stubGlobal("fetch", fetchMock)

    const got = await listSchedules("tm 1", "token-123")

    expect(got.total).toBe(0)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/spaces/tm%201/schedules")
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer token-123")
  })
})

describe("createSchedule", () => {
  it("posts the schedule body to the space's schedules route", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, { id: "sch1", enabled: true }))
    vi.stubGlobal("fetch", fetchMock)

    const got = await createSchedule(
      "tm1",
      { executor_kind: "agent", executor_id: "a1", input: "summarize", cron_expr: "0 9 * * *", timezone: "UTC" },
      "token-123",
    )

    expect(got.id).toBe("sch1")
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/spaces/tm1/schedules")
    expect(init.method).toBe("POST")
    expect(JSON.parse(init.body as string)).toMatchObject({ executor_kind: "agent", executor_id: "a1", cron_expr: "0 9 * * *" })
  })

  it("surfaces the server's reason for refusing an invalid schedule", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(400, { error: "cron \"nope\": invalid" })))
    await expect(
      createSchedule("tm1", { executor_kind: "agent", executor_id: "a1", input: "x", cron_expr: "nope", timezone: "UTC" }, "t"),
    ).rejects.toThrow("invalid")
  })
})

describe("updateSchedule", () => {
  it("patches only the fields it is given", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { id: "sch1", enabled: false }))
    vi.stubGlobal("fetch", fetchMock)

    const got = await updateSchedule("tm1", "sch1", { enabled: false }, "token-123")

    expect(got.enabled).toBe(false)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/spaces/tm1/schedules/sch1")
    expect(init.method).toBe("PATCH")
    expect(JSON.parse(init.body as string)).toEqual({ enabled: false })
  })
})

describe("deleteSchedule", () => {
  it("deletes the schedule with the caller's token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal("fetch", fetchMock)

    await deleteSchedule("tm1", "sch1", "token-123")

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/spaces/tm1/schedules/sch1")
    expect(init.method).toBe("DELETE")
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer token-123")
  })
})

describe("listScheduleTasks", () => {
  it("gets the tasks a schedule triggered", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { tasks: [], total: 0 }))
    vi.stubGlobal("fetch", fetchMock)

    await listScheduleTasks("tm1", "sch1", "token-123")

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/spaces/tm1/schedules/sch1/tasks")
  })
})

describe("listScheduleRuns", () => {
  it("gets the workflow runs a schedule triggered", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { runs: [], total: 0 }))
    vi.stubGlobal("fetch", fetchMock)

    await listScheduleRuns("tm1", "sch1", "token-123")

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/spaces/tm1/schedules/sch1/runs")
  })
})
