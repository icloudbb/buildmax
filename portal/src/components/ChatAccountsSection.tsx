import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { getErrorMessage } from "../lib/errorMessage"
import { navigate } from "../router"
import {
  createChannelLink,
  deleteChannelLink,
  getChannelPairing,
  listChannelLinks,
  type ChannelLink,
  type ChannelPairing,
  type ListChannelLinksResponse,
} from "../features/channelLinks/api"

interface ChatAccountsSectionProps {
  token: string | null
  /** A link code from the address the chat bot sent, if the page opened on one. */
  code?: string
}

function platformName(platform: string, platforms: ListChannelLinksResponse["platforms"]): string {
  return platforms.find((p) => p.platform === platform)?.name ?? platform
}

/**
 * Links chat-platform accounts (Telegram) to this BuildMax account so the
 * assistant can be used from the chat app. A link is started in the chat —
 * the bot answers an unlinked account with a code — and confirmed here, after
 * seeing which chat account it is, so a chat never carries a BuildMax secret.
 * See docs/design/instant-messaging-channels.md.
 */
export function ChatAccountsSection({ token, code }: ChatAccountsSectionProps) {
  const [data, setData] = useState<ListChannelLinksResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [codeInput, setCodeInput] = useState(code ?? "")
  const [pending, setPending] = useState<{ code: string; pairing: ChannelPairing } | null>(null)
  const [lookingUp, setLookingUp] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [linked, setLinked] = useState<ChannelLink | null>(null)
  const [unlinkingId, setUnlinkingId] = useState<string | null>(null)

  const refresh = useCallback(() => {
    if (!token) return
    setLoading(true)
    listChannelLinks(token)
      .then(setData)
      .catch((err) => setError(getErrorMessage(err, "Failed to load chat accounts")))
      .finally(() => setLoading(false))
  }, [token])

  useEffect(() => {
    refresh()
  }, [refresh])

  const lookUp = useCallback(
    (value: string) => {
      const trimmed = value.trim()
      if (!token || !trimmed) return
      setError(null)
      setLinked(null)
      setLookingUp(true)
      getChannelPairing(trimmed, token)
        .then((pairing) => setPending({ code: trimmed, pairing }))
        .catch((err) => {
          setPending(null)
          setError(getErrorMessage(err, "That code was not found"))
        })
        .finally(() => setLookingUp(false))
    },
    [token]
  )

  // Opening the address the bot sent looks its code up straight away; the
  // link is still only made on an explicit confirm.
  useEffect(() => {
    if (code) {
      setCodeInput(code)
      lookUp(code)
    }
  }, [code, lookUp])

  function clearCodeFromAddress() {
    if (code) navigate({ name: "account", section: "chat" })
  }

  function handleConfirm() {
    if (!token || !pending) return
    setError(null)
    setConfirming(true)
    createChannelLink(pending.code, token)
      .then((link) => {
        setLinked(link)
        setPending(null)
        setCodeInput("")
        clearCodeFromAddress()
        refresh()
      })
      .catch((err) => setError(getErrorMessage(err, "Failed to link")))
      .finally(() => setConfirming(false))
  }

  function handleCancel() {
    setPending(null)
    setCodeInput("")
    clearCodeFromAddress()
  }

  function handleUnlink(linkId: string) {
    if (!token) return
    setError(null)
    setUnlinkingId(linkId)
    deleteChannelLink(linkId, token)
      .then(refresh)
      .catch((err) => setError(getErrorMessage(err, "Failed to unlink")))
      .finally(() => setUnlinkingId(null))
  }

  const platforms = data?.platforms ?? []
  const links = data?.links ?? []

  return (
    <section className="settings-section settings-webhook">
      <h2 className="settings-panel__heading">Linked chat apps</h2>
      <div className="settings-panel__heading-divider" role="separator" />

      {loading && !data ? (
        <p className="settings-section__muted">Loading chat accounts…</p>
      ) : platforms.length === 0 ? (
        <p className="settings-section__muted">
          No chat app is connected to this BuildMax server. An operator can connect one; see the server
          configuration guide.
        </p>
      ) : (
        <>
          <p className="settings-webhook__description">
            Talk to your BuildMax assistant from a chat app. Send the bot any message
            {platforms.map((p) =>
              p.bot_url ? (
                <span key={p.platform}>
                  {" "}
                  (
                  <a href={p.bot_url} target="_blank" rel="noreferrer">
                    {p.bot_handle ?? p.name}
                  </a>{" "}
                  on {p.name})
                </span>
              ) : null
            )}
            ; it answers with a link code. Open the link it sends, or enter the code here.
          </p>

          {pending ? (
            <div className="settings-webhook__new-key" role="dialog" aria-label="Confirm chat link">
              <p className="settings-webhook__new-key-warning">
                Link {platformName(pending.pairing.platform, platforms)} account{" "}
                <strong>{pending.pairing.handle || "(no name)"}</strong> to your BuildMax account? Messages from it
                will act as you. Only confirm a code you asked for yourself.
              </p>
              <div className="settings-webhook__create">
                <Button variant="primary" busy={confirming} onClick={handleConfirm}>
                  Link account
                </Button>
                <Button variant="secondary" disabled={confirming} onClick={handleCancel}>
                  Cancel
                </Button>
              </div>
            </div>
          ) : (
            <div className="settings-webhook__create">
              <input
                type="text"
                className="settings-webhook__input"
                placeholder="ABCD-EFGH"
                aria-label="Link code"
                value={codeInput}
                onChange={(e) => setCodeInput(e.target.value)}
                disabled={lookingUp}
              />
              <Button variant="primary" busy={lookingUp} disabled={!codeInput.trim()} onClick={() => lookUp(codeInput)}>
                Look up code
              </Button>
            </div>
          )}

          {linked ? (
            <p className="settings-section__muted" role="status">
              Linked {platformName(linked.platform, platforms)} account {linked.handle}. Send it a message to start.
            </p>
          ) : null}
        </>
      )}

      {error ? (
        <div className="settings-webhook__error" role="alert">
          {error}
        </div>
      ) : null}

      {links.length > 0 ? (
        <ul className="settings-webhook__key-list" aria-label="Linked chat accounts">
          {links.map((l) => (
            <li key={l.id} className="settings-webhook__key-item">
              <span className="settings-webhook__key-name">
                {platformName(l.platform, platforms)} · {l.handle || l.id}
              </span>
              <span className="settings-webhook__key-meta">{new Date(l.created_at).toLocaleString()}</span>
              <Button
                variant="danger"
                size="compact"
                busy={unlinkingId === l.id}
                disabled={unlinkingId !== null}
                onClick={() => handleUnlink(l.id)}
              >
                Unlink
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  )
}
