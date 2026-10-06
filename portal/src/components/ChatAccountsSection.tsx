import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { getErrorMessage } from "../lib/errorMessage"
import { useStableT, useT } from "../i18n"
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
import { chatLinkActivity, type ChatLinkActivity } from "../features/channelLinks/activity"

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
  const t = useT()
  const stableT = useStableT()
  const [data, setData] = useState<ListChannelLinksResponse | null>(null)
  // Judged when the list arrives, not on every render, so a render stays pure.
  const [activity, setActivity] = useState<ChatLinkActivity | null>(null)
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
      .then((res) => {
        setData(res)
        setActivity(chatLinkActivity(res.active_until, Date.now()))
      })
      .catch((err) => setError(getErrorMessage(err, stableT("account.chat.loadError"))))
      .finally(() => setLoading(false))
  }, [token, stableT])

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
          setError(getErrorMessage(err, stableT("account.chat.codeNotFound")))
        })
        .finally(() => setLookingUp(false))
    },
    [token, stableT]
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
      .catch((err) => setError(getErrorMessage(err, stableT("account.chat.linkError"))))
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
      .catch((err) => setError(getErrorMessage(err, stableT("account.chat.unlinkError"))))
      .finally(() => setUnlinkingId(null))
  }

  const platforms = data?.platforms ?? []
  const links = data?.links ?? []
  // The chat account's name is emphasized inside the question, so the
  // translated question is split at its placeholder.
  const [confirmBefore, confirmAfter] = pending
    ? t("account.chat.confirm", { platform: platformName(pending.pairing.platform, platforms) }).split("{handle}")
    : ["", ""]

  return (
    <section className="settings-section settings-webhook">
      <h2 className="settings-panel__heading">{t("account.chat.heading")}</h2>
      <div className="settings-panel__heading-divider" role="separator" />

      {loading && !data ? (
        <p className="settings-section__muted">{t("account.chat.loading")}</p>
      ) : platforms.length === 0 ? (
        <p className="settings-section__muted">{t("account.chat.noPlatform")}</p>
      ) : (
        <>
          <p className="settings-webhook__description">
            {t("account.chat.intro")}
            {platforms.map((p) => {
              if (!p.bot_url) return null
              // The bot is a link inside the phrase, so the phrase is split at it.
              const [before, after] = t("account.chat.botOn", { platform: p.name }).split("{bot}")
              return (
                <span key={p.platform}>
                  {before}
                  <a href={p.bot_url} target="_blank" rel="noreferrer">
                    {p.bot_handle ?? p.name}
                  </a>
                  {after}
                </span>
              )
            })}
            {t("account.chat.introEnd")}
          </p>

          {pending ? (
            <div className="settings-webhook__new-key" role="dialog" aria-label={t("account.chat.confirmLabel")}>
              <p className="settings-webhook__new-key-warning">
                {confirmBefore}
                <strong>{pending.pairing.handle || t("account.chat.noName")}</strong>
                {confirmAfter}
              </p>
              <div className="settings-webhook__create">
                <Button variant="primary" busy={confirming} onClick={handleConfirm}>
                  {t("account.chat.link")}
                </Button>
                <Button variant="secondary" disabled={confirming} onClick={handleCancel}>
                  {t("account.chat.cancel")}
                </Button>
              </div>
            </div>
          ) : (
            <div className="settings-webhook__create">
              <input
                type="text"
                className="settings-webhook__input"
                placeholder="ABCD-EFGH"
                aria-label={t("account.chat.codeLabel")}
                value={codeInput}
                onChange={(e) => setCodeInput(e.target.value)}
                disabled={lookingUp}
              />
              <Button variant="primary" busy={lookingUp} disabled={!codeInput.trim()} onClick={() => lookUp(codeInput)}>
                {t("account.chat.lookUp")}
              </Button>
            </div>
          )}

          {linked ? (
            <p className="settings-section__muted" role="status">
              {t("account.chat.linked", {
                platform: platformName(linked.platform, platforms),
                handle: linked.handle ?? "",
              })}
            </p>
          ) : null}
        </>
      )}

      {error ? (
        <div className="settings-webhook__error" role="alert">
          {error}
        </div>
      ) : null}

      {links.length > 0 && activity ? (
        <p className="settings-section__muted" role="status">
          {activity.state === "lapsed"
            ? t("account.chat.lapsed")
            : t("account.chat.activeUntil", { until: activity.until.toLocaleString() })}
        </p>
      ) : null}

      {links.length > 0 ? (
        <ul className="settings-webhook__key-list" aria-label={t("account.chat.listLabel")}>
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
                {t("account.chat.unlink")}
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  )
}
