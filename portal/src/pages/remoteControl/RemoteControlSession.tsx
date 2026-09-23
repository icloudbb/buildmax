import { useEffect, useRef, useState } from "react"
import { navigate } from "../../router"
import {
  cancelRemoteSession,
  listRemoteSessions,
  respondRemoteApproval,
  sendRemotePrompt,
  streamRemoteApprovals,
  streamRemoteSession,
  type ApprovalFrame,
  type RemoteSession,
} from "../../features/remoteControl/api"

interface RemoteControlSessionProps {
  token: string | null
  sessionId: string
}

type StreamStatus = "connecting" | "streaming"

/**
 * Open a stream and reopen it whenever it ends, until the signal aborts. A
 * Remote Control session outlives any one connection, so a dropped stream is a
 * reason to reconnect, not to give up. Backoff grows for a stream that fails
 * immediately and resets for one that ran a while before dropping.
 */
async function reconnectingStream(
  signal: AbortSignal,
  open: () => Promise<void>,
  onReconnect?: () => void
): Promise<void> {
  const minBackoff = 1000
  const maxBackoff = 15000
  let backoff = minBackoff
  while (!signal.aborted) {
    const startedAt = Date.now()
    try {
      await open()
    } catch {
      // A failed open is just another reason to retry.
    }
    if (signal.aborted) return
    if (Date.now() - startedAt > maxBackoff) backoff = minBackoff
    onReconnect?.()
    await abortableSleep(backoff, signal)
    backoff = Math.min(backoff * 2, maxBackoff)
  }
}

function abortableSleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const id = setTimeout(resolve, ms)
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(id)
        resolve()
      },
      { once: true }
    )
  })
}

/**
 * Read-only view of one live session's relayed output. Execution stays on the
 * user's machine; this is a window onto its stream. Phase 1 relays assistant
 * content deltas, so this renders the accumulated text and does not steer.
 */
export function RemoteControlSession({ token, sessionId }: RemoteControlSessionProps) {
  const [text, setText] = useState("")
  const [status, setStatus] = useState<StreamStatus>("connecting")
  const [meta, setMeta] = useState<RemoteSession | null>(null)
  const [draft, setDraft] = useState("")
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState<string | null>(null)
  const [approval, setApproval] = useState<ApprovalFrame | null>(null)
  const bodyRef = useRef<HTMLDivElement>(null)

  // Header meta and presence: fetched from the list, which is the only place a
  // session's name and online state live.
  useEffect(() => {
    if (!token) return
    let active = true
    const load = () => {
      void listRemoteSessions(token)
        .then((sessions) => {
          if (active) setMeta(sessions.find((s) => s.id === sessionId) ?? null)
        })
        .catch(() => {})
    }
    load()
    const id = window.setInterval(load, 4000)
    return () => {
      active = false
      window.clearInterval(id)
    }
  }, [token, sessionId])

  // The stream itself. A session is long-lived, so a stream that ends — an idle
  // connection a proxy dropped, a replica draining, a brief network loss — is
  // reopened with backoff rather than left as a dead view. The server keeps an
  // idle stream alive with heartbeats, so this reconnect is for real drops, not
  // ordinary silence.
  useEffect(() => {
    if (!token) return
    const controller = new AbortController()
    setText("")
    setStatus("connecting")
    void reconnectingStream(controller.signal, async () => {
      // Each fresh connection replays the session's whole buffer before any live
      // delta, so start from empty and let the replay rebuild the text. Appending
      // to what a previous connection left would double the replayed content.
      setText("")
      await streamRemoteSession(
        sessionId,
        token,
        {
          onDelta: (delta) => {
            setStatus("streaming")
            setText((prev) => prev + delta)
          },
          onDone: () => {},
          onError: () => {},
          onDraining: () => {},
        },
        { signal: controller.signal }
      )
    }, () => {
      if (!controller.signal.aborted) setStatus("connecting")
    })
    return () => controller.abort()
  }, [token, sessionId])

  // Pending tool approvals: a separate stream carrying request/dismiss frames,
  // reconnected the same way so a device can still answer a prompt after a drop.
  useEffect(() => {
    if (!token) return
    const controller = new AbortController()
    setApproval(null)
    void reconnectingStream(controller.signal, async () => {
      await streamRemoteApprovals(
        sessionId,
        token,
        {
          onFrame: (frame) => {
            setApproval((prev) => {
              if (frame.resolved) return prev && prev.id === frame.id ? null : prev
              return frame
            })
          },
          onDone: () => {},
          onError: () => {},
          onDraining: () => {},
        },
        { signal: controller.signal }
      )
    })
    return () => controller.abort()
  }, [token, sessionId])

  // Keep the newest output in view as it streams.
  useEffect(() => {
    const el = bodyRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [text])

  const online = meta?.status === "online"
  const title = meta?.display_name || meta?.host || sessionId

  async function stopRun() {
    if (!token || !online) return
    try {
      await cancelRemoteSession(sessionId, token)
    } catch (err) {
      setSendError(err instanceof Error ? err.message : String(err))
    }
  }

  async function answerApproval(decision: "once" | "session" | "deny") {
    if (!approval || !token) return
    const id = approval.id
    setApproval(null) // optimistic; a resolved frame confirms
    try {
      await respondRemoteApproval(sessionId, id, decision, token)
    } catch (err) {
      setSendError(err instanceof Error ? err.message : String(err))
    }
  }

  async function submitPrompt() {
    const content = draft.trim()
    if (!content || !token || sending) return
    setSending(true)
    setSendError(null)
    try {
      await sendRemotePrompt(sessionId, content, token)
      setDraft("")
    } catch (err) {
      setSendError(err instanceof Error ? err.message : String(err))
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="rc-page rc-session">
      <header className="rc-page__header">
        <button type="button" className="rc-back" onClick={() => navigate({ name: "remoteControl" })}>
          ← Sessions
        </button>
        <div className="rc-session__heading">
          <span
            className={`rc-status-dot rc-status-dot--${online ? "online" : "offline"}`}
            aria-label={online ? "Online" : "Offline"}
          />
          <h1 className="rc-page__title">{title}</h1>
          {online && status === "streaming" ? (
            <button type="button" className="rc-stop" onClick={() => void stopRun()}>
              Stop
            </button>
          ) : null}
        </div>
        <p className="rc-page__subtitle">
          {meta ? [meta.platform, meta.host].filter(Boolean).join(" · ") : ""}
          {meta ? " · " : ""}
          {statusLabel(status, online)}
        </p>
      </header>

      <div className="rc-stream" ref={bodyRef}>
        {text ? (
          <pre className="rc-stream__body">{text}</pre>
        ) : (
          <p className="rc-stream__empty">
            {online
              ? "Waiting for output…"
              : "This session is offline. It will stream again when it reconnects."}
          </p>
        )}
      </div>

      {approval && online ? (
        <div className="rc-approval">
          <div className="rc-approval__body">
            <span className="rc-approval__title">Approve tool call</span>
            <code className="rc-approval__tool">{approval.tool || "tool"}</code>
            {approval.summary ? <span className="rc-approval__summary">{approval.summary}</span> : null}
          </div>
          <div className="rc-approval__actions">
            <button type="button" className="rc-approval__btn" onClick={() => void answerApproval("once")}>
              Allow once
            </button>
            <button type="button" className="rc-approval__btn" onClick={() => void answerApproval("session")}>
              Allow session
            </button>
            <button
              type="button"
              className="rc-approval__btn rc-approval__btn--deny"
              onClick={() => void answerApproval("deny")}
            >
              Deny
            </button>
          </div>
        </div>
      ) : null}

      <form
        className="rc-composer"
        onSubmit={(e) => {
          e.preventDefault()
          void submitPrompt()
        }}
      >
        <textarea
          className="rc-composer__input"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              void submitPrompt()
            }
          }}
          placeholder={online ? "Send a message to this session…" : "Session is offline"}
          rows={2}
          disabled={!online || sending}
        />
        <button type="submit" className="rc-composer__send" disabled={!online || sending || !draft.trim()}>
          {sending ? "Sending…" : "Send"}
        </button>
      </form>
      {sendError ? <div className="rc-alert">{sendError}</div> : null}
    </div>
  )
}

function statusLabel(status: StreamStatus, online: boolean): string {
  if (!online) return "offline"
  return status === "streaming" ? "live" : "connecting…"
}
