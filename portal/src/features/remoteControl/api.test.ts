import { afterEach, describe, expect, it, vi } from "vitest"
import { respondRemoteQuestion, streamRemoteQuestions, type QuestionFrame } from "./api"

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("respondRemoteQuestion", () => {
  it("posts one answer per question to the session's question route", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ accepted: true }), { status: 202 }))
    vi.stubGlobal("fetch", fetchMock)

    await respondRemoteQuestion("s 1", "q1", { answers: ["Postgres", "orders-api"] }, "tok")

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain("/api/remote-control/sessions/s%201/question")
    expect(init.method).toBe("POST")
    expect(JSON.parse(init.body as string)).toEqual({ id: "q1", answers: ["Postgres", "orders-api"] })
  })

  it("sends a dismissal without answers", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ accepted: true }), { status: 202 }))
    vi.stubGlobal("fetch", fetchMock)

    await respondRemoteQuestion("s1", "q1", { declined: true }, "tok")

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(init.body as string)).toEqual({ id: "q1", declined: true })
  })
})

describe("streamRemoteQuestions", () => {
  // A replayed buffer arrives as several newline-separated frames in one
  // payload; each has to come apart, in order, so a later dismissal wins.
  it("splits each payload into its question frames", async () => {
    const body =
      'data: {"id":"q1","questions":[{"question":"Which database?","options":[{"label":"Postgres"}]}]}\n' +
      'data: {"id":"q1","resolved":true}\n\n' +
      "data: done\n\n"
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } })),
    )
    const frames: QuestionFrame[] = []
    await streamRemoteQuestions("s1", "tok", {
      onFrame: (f) => frames.push(f),
      onDone: () => {},
      onError: (err) => {
        throw err
      },
    })
    expect(frames).toHaveLength(2)
    expect(frames[0].questions?.[0].options?.[0].label).toBe("Postgres")
    expect(frames[1]).toEqual({ id: "q1", resolved: true })
  })
})
