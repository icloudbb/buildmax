import { useCallback, useEffect, useState } from 'react';
import { MarkdownMessage } from './MarkdownMessage';
import { ISSUE_STATUSES, commentAuthorLabel, issueChatPrompt, issueStatusLabel } from '../lib/issues';

function formatWhen(value) {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function errorText(err) {
  return err?.message ?? String(err);
}

// IssuesView is the signed-in person's Space work, shown only in server mode:
// the open Issues they own, one Issue's detail, and the three things a person
// does from a local workbench — move its status, comment on it, and start a
// local chat from it. Planning the whole Space stays in Portal.
export function IssuesView({ app, projects, currentProject, onStartChat }) {
  const [inbox, setInbox] = useState(null);
  const [inboxError, setInboxError] = useState(null);
  const [selected, setSelected] = useState(null);
  const [detail, setDetail] = useState(null);
  const [detailError, setDetailError] = useState(null);
  const [statusBusy, setStatusBusy] = useState(false);
  const [notice, setNotice] = useState(null);
  const [comment, setComment] = useState('');
  const [commentBusy, setCommentBusy] = useState(false);
  const [commentError, setCommentError] = useState(null);
  const [projectId, setProjectId] = useState(currentProject?.id ?? projects[0]?.id ?? '');
  // Bumped to re-read the selected Issue after a change.
  const [detailSeq, setDetailSeq] = useState(0);

  const loadInbox = useCallback(async () => {
    setInboxError(null);
    try {
      setInbox(await app.ListMyIssues());
    } catch (err) {
      setInboxError(errorText(err));
    }
  }, [app]);

  useEffect(() => { loadInbox(); }, [loadInbox]);

  // A response for an Issue the person has already moved past is dropped.
  useEffect(() => {
    if (!selected) return undefined;
    let live = true;
    app.GetIssueDetail(selected.space_id, selected.space_name, selected.id)
      .then((next) => { if (live) { setDetail(next); setDetailError(null); } })
      .catch((err) => { if (live) setDetailError(errorText(err)); });
    return () => { live = false; };
  }, [app, selected, detailSeq]);

  const reloadDetail = () => setDetailSeq((n) => n + 1);

  useEffect(() => {
    if (!projectId && projects.length > 0) setProjectId(currentProject?.id ?? projects[0].id);
  }, [projects, currentProject, projectId]);

  function select(item) {
    setSelected(item);
    setDetail(null);
    setNotice(null);
    setDetailError(null);
    setComment('');
    setCommentError(null);
  }

  async function moveTo(status) {
    if (!detail || statusBusy) return;
    const { issue } = detail;
    setStatusBusy(true);
    setNotice(null);
    try {
      const res = await app.SetIssueStatus(issue.space_id, issue.space_name, issue.id, status, issue.version);
      if (res.conflict) {
        setNotice({ tone: 'error', text: 'This issue changed since it was loaded, so its status was not changed. It has been reloaded — check it and choose again.' });
      } else {
        setNotice({ tone: 'info', text: `Moved to ${issueStatusLabel(status)}.` });
      }
      reloadDetail();
      await loadInbox();
    } catch (err) {
      setNotice({ tone: 'error', text: `Couldn't change the status: ${errorText(err)}` });
    } finally {
      setStatusBusy(false);
    }
  }

  async function postComment(e) {
    e.preventDefault();
    if (!detail || commentBusy || !comment.trim()) return;
    setCommentBusy(true);
    setCommentError(null);
    try {
      await app.CommentOnIssue(detail.issue.space_id, detail.issue.id, comment);
      setComment('');
      reloadDetail();
    } catch (err) {
      setCommentError(errorText(err));
    } finally {
      setCommentBusy(false);
    }
  }

  function startChat() {
    const project = projects.find((p) => p.id === projectId);
    if (!project || !detail) return;
    onStartChat(project, issueChatPrompt(detail));
  }

  const groups = ['in_progress', 'todo'].map((status) => ({
    status,
    items: (inbox?.issues ?? []).filter((i) => i.status === status),
  }));

  return (
    <div className="page-schedules page-issues">
      <div className="page-schedules__header">
        <div>
          <h1 className="page-schedules__title">Issues</h1>
          <p className="page-schedules__subtitle">Open Space work you own. Plan and assign work in Portal.</p>
        </div>
        <div className="page-schedules__header-actions">
          <button type="button" className="page-schedules__ghost" onClick={loadInbox}>Refresh</button>
        </div>
      </div>

      {inboxError && (
        <div className="page-schedules__banner page-schedules__banner--error" role="alert">
          <span>Couldn&apos;t load your issues: {inboxError}</span>
        </div>
      )}
      {inbox?.warnings?.length > 0 && (
        <div className="page-schedules__banner page-schedules__banner--error" role="status">
          <span>Some spaces couldn&apos;t be read, so this list may be incomplete: {inbox.warnings.join('; ')}</span>
        </div>
      )}

      <div className="page-schedules__grid">
        <section className="page-schedules__section" aria-label="Your issues">
          {inbox === null && !inboxError && <p className="page-schedules__empty">Loading…</p>}
          {inbox && inbox.issues.length === 0 && (
            <p className="page-schedules__empty">No open issues are assigned to you.</p>
          )}
          {inbox && inbox.issues.length > 0 && groups.map((group) => group.items.length > 0 && (
            <div key={group.status} className="page-issues__group">
              <div className="page-schedules__section-head">
                <h2>{issueStatusLabel(group.status)} <span className="page-issues__count">{group.items.length}</span></h2>
              </div>
              <ul className="page-issues__list">
                {group.items.map((item) => {
                  const active = selected?.id === item.id && selected?.space_id === item.space_id;
                  return (
                    <li key={`${item.space_id}/${item.id}`}>
                      <button
                        type="button"
                        className={`page-issues__row${active ? ' page-issues__row--active' : ''}`}
                        aria-current={active ? 'true' : undefined}
                        onClick={() => select(item)}
                      >
                        <span className="page-issues__row-title">{item.title}</span>
                        <span className="page-issues__row-meta">{item.space_name} · {formatWhen(item.updated_at)}</span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </section>

        <section className="page-schedules__section" aria-label="Issue detail">
          {!selected && <p className="page-schedules__empty">Select an issue to see it here.</p>}
          {selected && detailError && (
            <div className="page-schedules__banner page-schedules__banner--error" role="alert">
              <span>Couldn&apos;t load this issue: {detailError}</span>
              <button type="button" onClick={reloadDetail}>Retry</button>
            </div>
          )}
          {selected && !detail && !detailError && <p className="page-schedules__empty">Loading…</p>}
          {detail && (
            <article className="page-issues__detail">
              <h2 className="page-issues__title">{detail.issue.title}</h2>
              <p className="page-issues__row-meta">
                {detail.issue.space_name} · {issueStatusLabel(detail.issue.status)} · {detail.issue.id}
              </p>

              <div className="page-issues__actions" role="group" aria-label="Move this issue">
                <span className="page-issues__label">Move to</span>
                {ISSUE_STATUSES.filter((s) => s !== detail.issue.status).map((status) => (
                  <button
                    key={status}
                    type="button"
                    className="page-schedules__ghost"
                    disabled={statusBusy}
                    onClick={() => moveTo(status)}
                  >
                    {issueStatusLabel(status)}
                  </button>
                ))}
              </div>
              {notice && (
                <p className={`page-issues__notice page-issues__notice--${notice.tone}`} role={notice.tone === 'error' ? 'alert' : 'status'}>
                  {notice.text}
                </p>
              )}

              <div className="page-issues__start">
                {projects.length === 0 ? (
                  <p className="page-schedules__hint">Open a project to work on this issue in a local chat.</p>
                ) : (
                  <>
                    <label className="page-schedules__field page-issues__project">
                      <span>Project</span>
                      <select value={projectId} onChange={(e) => setProjectId(e.target.value)}>
                        {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                      </select>
                    </label>
                    <button type="button" className="page-schedules__primary" onClick={startChat}>
                      Start chat
                    </button>
                  </>
                )}
              </div>
              <p className="page-schedules__hint">
                Starts a new chat with this issue in the message box. Nothing is sent until you send it.
              </p>

              {detail.description?.trim() ? (
                <div className="page-issues__description"><MarkdownMessage content={detail.description} /></div>
              ) : (
                <p className="page-schedules__empty">No description.</p>
              )}

              {detail.children.length > 0 && (
                <>
                  <h3 className="page-issues__subhead">Sub-issues</h3>
                  <ul className="page-issues__children">
                    {detail.children.map((child) => (
                      <li key={child.id}>
                        <span className="page-issues__child-status">{issueStatusLabel(child.status)}</span> {child.title}
                      </li>
                    ))}
                  </ul>
                </>
              )}

              <h3 className="page-issues__subhead">Discussion</h3>
              {detail.omitted_comments > 0 && (
                <p className="page-schedules__hint">{detail.omitted_comments} earlier comments are not shown.</p>
              )}
              {detail.comments.length === 0 && <p className="page-schedules__empty">No comments yet.</p>}
              <ul className="page-issues__comments">
                {detail.comments.map((c, i) => (
                  <li key={`${c.created_at}-${i}`} className="page-issues__comment">
                    <span className="page-issues__row-meta">{commentAuthorLabel(c)} · {formatWhen(c.created_at)}</span>
                    <div className="page-issues__comment-body">{c.body}</div>
                  </li>
                ))}
              </ul>
              <form className="page-schedules__form" onSubmit={postComment}>
                <label className="page-schedules__field">
                  <span>Add a comment</span>
                  <textarea rows={3} value={comment} onChange={(e) => setComment(e.target.value)} />
                </label>
                {commentError && <p className="page-schedules__form-error" role="alert">{commentError}</p>}
                <div className="page-schedules__form-actions">
                  <button type="submit" className="page-schedules__primary" disabled={commentBusy || !comment.trim()}>
                    {commentBusy ? 'Posting…' : 'Post comment'}
                  </button>
                </div>
              </form>
            </article>
          )}
        </section>
      </div>
    </div>
  );
}
