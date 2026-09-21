import { useEffect, useRef, useState } from "react"
import { navigate } from "../../router"
import {
  listRemoteSessions,
  sendRemotePrompt,
  streamRemoteSession,
  type RemoteSession,
} from "../../features/remoteControl/api"

interface RemoteControlSessionProps {
  token: string | null
  sessionId: string
}

type StreamStatus = "connecting" | "streaming" | "ended" | "error"

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

  // The stream itself: opened once, torn down on unmount or a token change.
  useEffect(() => {
    if (!token) return
    const controller = new AbortController()
    setText("")
    setStatus("connecting")
    void streamRemoteSession(
      sessionId,
      token,
      {
        onDelta: (delta) => {
          setStatus("streaming")
          setText((prev) => prev + delta)
        },
        onDone: () => setStatus("ended"),
        onError: () => {
          if (!controller.signal.aborted) setStatus("error")
        },
        onDraining: () => setStatus("ended"),
      },
      { signal: controller.signal }
    )
    return () => controller.abort()
  }, [token, sessionId])

  // Keep the newest output in view as it streams.
  useEffect(() => {
    const el = bodyRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [text])

  const online = meta?.status === "online"
  const title = meta?.display_name || meta?.host || sessionId

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
            {status === "error"
              ? "Could not read this session's stream."
              : online
                ? "Waiting for output…"
                : "This session is offline. It will stream again when it reconnects."}
          </p>
        )}
      </div>

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
  switch (status) {
    case "streaming":
      return "streaming"
    case "ended":
      return online ? "idle" : "offline"
    case "error":
      return "stream error"
    case "connecting":
    default:
      return online ? "connecting…" : "offline"
  }
}
