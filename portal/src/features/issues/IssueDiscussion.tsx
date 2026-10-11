import { Button, ButtonLink } from "@buildmax/gui"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { buildHash } from "../../router"
import type { ApiIssueComment, ApiSpaceMember } from "../../lib/api/types"
import { createIssueComment, deleteIssueComment, getIssueComments, replyToRequester, updateIssueComment } from "./comments"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT } from "../../i18n"
import { useTimestamp } from "../../lib/dateFormat"
import { FailureFixLink, RunFailureNotice, type RunFailureExplanation } from "../runs"

/** Matches CommentBodyLimit in internal/service/issue. */
const BODY_LIMIT = 16 * 1024
/** Where the counter appears, so a long comment warns before the server refuses it. */
const COUNTER_THRESHOLD = 15 * 1024
/** Matches the reply limit in internal/service/assistant, under a chat message's. */
const REPLY_LIMIT = 4000

/**
 * How often the thread reloads. There is no push channel for comments yet, so
 * two people commenting at once see each other within this window.
 */
const POLL_INTERVAL_MS = 20_000

interface IssueDiscussionProps {
  spaceId: string | null
  issueId: string | null
  token: string | null
  userId: string | null
  /** True when the caller may delete comments they did not write. */
  canModerate: boolean
  members: ApiSpaceMember[]
  agentNames: Record<string, string>
  onOpenTrace?: (taskRunId: string) => void
  /**
   * Failed runs explained, by task run id. An Agent's report of such a run is
   * shown as the explanation and its fix instead of the server's raw text,
   * which stays under the explanation's disclosure.
   */
  runFailures?: Record<string, { explanation: RunFailureExplanation; agentId?: string }>
  /**
   * Reports the current thread whenever it changes. This component owns the
   * fetch — the page reads the result rather than requesting it a second time.
   * The callback must be stable, or it will re-fire on every render.
   */
  onCommentsChanged?: (comments: ApiIssueComment[]) => void
  /**
   * Set when the Issue was escalated by a Space Assistant: the composer can then
   * send its text to the requester's chat through that Assistant's bot.
   */
  requesterReply?: { assistantName: string }
}

export function IssueDiscussion({
  spaceId,
  issueId,
  token,
  userId,
  canModerate,
  members,
  agentNames,
  onOpenTrace,
  runFailures,
  onCommentsChanged,
  requesterReply,
}: IssueDiscussionProps) {
  const t = useT()
  const formatTimestamp = useTimestamp()
  const stableT = useStableT()
  const [comments, setComments] = useState<ApiIssueComment[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [draft, setDraft] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [replying, setReplying] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState("")
  // Tracks the latest load so a slow response cannot overwrite a newer one.
  const loadSeq = useRef(0)

  const load = useCallback(
    async (showSpinner: boolean) => {
      if (!spaceId || !issueId || !token) return
      const seq = ++loadSeq.current
      if (showSpinner) setLoading(true)
      try {
        const res = await getIssueComments(spaceId, issueId, token, { limit: 200 })
        if (seq === loadSeq.current) {
          setComments(res.comments ?? [])
          setError(null)
        }
      } catch (err) {
        if (seq === loadSeq.current) setError(getErrorMessage(err, stableT("issues.comment.error.load")))
      } finally {
        if (seq === loadSeq.current && showSpinner) setLoading(false)
      }
    },
    [spaceId, issueId, token, stableT],
  )

  useEffect(() => {
    void load(true)
  }, [load])

  useEffect(() => {
    if (!spaceId || !issueId || !token) return
    const timer = window.setInterval(() => {
      void load(false)
    }, POLL_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [load, spaceId, issueId, token])

  useEffect(() => {
    onCommentsChanged?.(comments)
  }, [comments, onCommentsChanged])

  const memberNames = useMemo(() => {
    const out: Record<string, string> = {}
    for (const member of members) {
      out[member.user_id] = member.user_name || member.user_email || t("issues.memberId", { id: member.user_id.slice(0, 8) })
    }
    return out
  }, [members, t])

  function personLabel(authorID: string): string {
    if (authorID === userId) return t("issues.me")
    return memberNames[authorID] || t("issues.memberId", { id: authorID.slice(0, 8) })
  }

  function authorLabel(comment: ApiIssueComment): string {
    if (comment.author_kind === "agent") return agentNames[comment.author_id] || t("issues.agent")
    if (comment.author_kind === "system") return "BuildMax"
    // Named as a report by a person, not as an agent this deployment ran: it
    // scheduled nothing, admitted no quota, and recorded no trace for it.
    if (comment.author_kind === "local_agent") return t("issues.comment.localAgent", { person: personLabel(comment.author_id) })
    return personLabel(comment.author_id)
  }

  function canEdit(comment: ApiIssueComment): boolean {
    return comment.author_kind === "user" && comment.author_id === userId
  }

  function canDelete(comment: ApiIssueComment): boolean {
    return canEdit(comment) || canModerate
  }

  async function handleSubmit() {
    if (!spaceId || !issueId || !token) return
    const body = draft.trim()
    if (!body || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const created = await createIssueComment(spaceId, issueId, body, token)
      setComments((prev) => [...prev, created])
      setDraft("")
    } catch (err) {
      setError(getErrorMessage(err, t("issues.comment.error.post")))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleReply() {
    if (!spaceId || !issueId || !token) return
    const text = draft.trim()
    if (!text || replying) return
    setReplying(true)
    setError(null)
    try {
      const recorded = await replyToRequester(spaceId, issueId, text, token)
      setComments((prev) => [...prev, recorded])
      setDraft("")
    } catch (err) {
      setError(getErrorMessage(err, t("issues.comment.error.reply")))
    } finally {
      setReplying(false)
    }
  }

  async function handleSaveEdit(commentId: string) {
    if (!spaceId || !issueId || !token) return
    const body = editDraft.trim()
    if (!body) return
    try {
      const updated = await updateIssueComment(spaceId, issueId, commentId, body, token)
      setComments((prev) => prev.map((c) => (c.id === commentId ? updated : c)))
      setEditingId(null)
      setEditDraft("")
    } catch (err) {
      setError(getErrorMessage(err, t("issues.comment.error.save")))
    }
  }

  async function handleDelete(commentId: string) {
    if (!spaceId || !issueId || !token) return
    // Deletion is permanent — the row is removed, not tombstoned.
    if (!window.confirm(t("issues.comment.confirmDelete"))) return
    try {
      await deleteIssueComment(spaceId, issueId, commentId, token)
      setComments((prev) => prev.filter((c) => c.id !== commentId))
    } catch (err) {
      setError(getErrorMessage(err, t("issues.comment.error.delete")))
    }
  }

  function handleComposerKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault()
      void handleSubmit()
    }
  }

  return (
    <div className="issue-discussion">
      {error ? (
        <p className="modal__error" role="alert">
          {error}
        </p>
      ) : null}
      {loading ? (
        <p className="page-activity__empty">{t("shell.loading")}</p>
      ) : comments.length === 0 ? (
        <p className="page-activity__empty">{t("issues.comment.empty")}</p>
      ) : (
        <ol className="issue-discussion__list">
          {comments.map((comment) => {
            const failure =
              comment.author_kind === "agent" && comment.source_task_run_id
                ? runFailures?.[comment.source_task_run_id]
                : undefined
            return (
            <li key={comment.id} className="issue-discussion__item">
              <div className="issue-discussion__head">
                <span className={`issue-discussion__author issue-discussion__author--${comment.author_kind}`}>
                  {authorLabel(comment)}
                </span>
                <span className="page-activity__meta">
                  {formatTimestamp(comment.created_at)}
                  {comment.edited_at ? t("issues.comment.edited") : ""}
                </span>
              </div>
              {editingId === comment.id ? (
                <div className="issue-discussion__composer">
                  <textarea
                    className="issues-page__input"
                    value={editDraft}
                    maxLength={BODY_LIMIT}
                    rows={4}
                    onChange={(e) => setEditDraft(e.target.value)}
                  />
                  <div className="issue-discussion__actions">
                    <Button
                      variant="primary" size="compact"
                      onClick={() => void handleSaveEdit(comment.id)}
                      disabled={editDraft.trim() === ""}
                    >
                      {t("issues.comment.save")}
                    </Button>
                    <Button variant="secondary" size="compact" onClick={() => setEditingId(null)}>
                      {t("issues.cancel")}
                    </Button>
                  </div>
                </div>
              ) : failure ? (
                <RunFailureNotice explanation={failure.explanation} />
              ) : (
                // Agents report in Markdown and people write it too; react-markdown
                // renders no raw HTML, so a comment cannot inject markup.
                <div className="issue-discussion__body page-chat__markdown">
                  <Markdown remarkPlugins={[remarkGfm]}>{comment.body}</Markdown>
                </div>
              )}
              <div className="issue-discussion__actions">
                {failure && spaceId ? (
                  <FailureFixLink
                    explanation={failure.explanation}
                    spaceId={spaceId}
                    agentId={failure.agentId}
                    size="compact"
                    variant="secondary"
                  />
                ) : null}
                {comment.source_task_id && spaceId ? (
                  <ButtonLink
                    variant="tertiary" size="compact"
                    href={buildHash({ name: "task", spaceId, taskId: comment.source_task_id })}
                  >
                    {t("issues.comment.openTask")}
                  </ButtonLink>
                ) : null}
                {comment.source_task_run_id && onOpenTrace ? (
                  <Button
                    variant="tertiary" size="compact"
                    onClick={() => onOpenTrace(comment.source_task_run_id!)}
                  >
                    {t("issues.comment.runDetails")}
                  </Button>
                ) : null}
                {canEdit(comment) && editingId !== comment.id ? (
                  <Button
                    variant="tertiary" size="compact"
                    onClick={() => {
                      setEditingId(comment.id)
                      setEditDraft(comment.body)
                    }}
                  >
                    {t("issues.comment.edit")}
                  </Button>
                ) : null}
                {canDelete(comment) ? (
                  <Button
                    variant="danger" size="compact"
                    onClick={() => void handleDelete(comment.id)}
                  >
                    {t("issues.comment.delete")}
                  </Button>
                ) : null}
              </div>
            </li>
            )
          })}
        </ol>
      )}
      <div className="issue-discussion__composer">
        <textarea
          className="issues-page__input"
          value={draft}
          maxLength={BODY_LIMIT}
          rows={3}
          placeholder={t("issues.comment.placeholder")}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleComposerKeyDown}
        />
        <div className="issue-discussion__actions">
          <Button
            variant="primary"
            busy={submitting}
            onClick={() => void handleSubmit()}
            disabled={draft.trim() === ""}
          >
            {t("issues.comment.post")}
          </Button>
          {requesterReply ? (
            <Button
              variant="secondary"
              busy={replying}
              onClick={() => void handleReply()}
              disabled={draft.trim() === "" || [...draft.trim()].length > REPLY_LIMIT}
              title={t("issues.comment.replyTitle", { assistant: requesterReply.assistantName })}
            >
              {t("issues.comment.reply")}
            </Button>
          ) : null}
          {requesterReply && [...draft.trim()].length > REPLY_LIMIT ? (
            <span className="page-activity__meta">
              {t("issues.comment.replyLimit", { limit: REPLY_LIMIT })}
            </span>
          ) : null}
          {draft.length > COUNTER_THRESHOLD ? (
            <span className="page-activity__meta">
              {draft.length} / {BODY_LIMIT}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  )
}
