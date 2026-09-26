import { useEffect, useState } from 'react';
import { formatSessionMeta } from '../lib/format';
import { ChangeRow, ExplorerGroup } from './ExplorerRows';
import { CommitIcon, MergeIcon } from './icons';

const PAGE_SIZE = 50;

// ExplorerCommits is the read-only history of the workspace's HEAD, newest
// first. A commit row expands to the files it changed (first-parent, so a merge
// shows what its branch brought in); a file opens that commit's diff of it.
// History is paged rather than loaded whole, since a repository can hold
// hundreds of thousands of commits.
export function ExplorerCommits({ projectID, sessionID, app, onOpenCommitDiff }) {
  const [open, setOpen] = useState(true);
  const [log, setLog] = useState({ loading: true, commits: [] });
  const [expanded, setExpanded] = useState(null);
  const [details, setDetails] = useState({});

  useEffect(() => {
    let cancelled = false;
    setLog({ loading: true, commits: [] });
    setExpanded(null);
    setDetails({});
    if (!app?.ListCommits) {
      setLog({ error: 'Rebuild the desktop app to view commits.', commits: [] });
      return undefined;
    }
    app.ListCommits(projectID, sessionID, 0, PAGE_SIZE)
      .then((res) => {
        if (cancelled) return;
        setLog(res?.error
          ? { error: res.error, commits: [] }
          : { commits: res?.commits ?? [], hasMore: !!res?.has_more, branch: res?.branch ?? '' });
      })
      .catch((err) => { if (!cancelled) setLog({ error: err?.message ?? String(err), commits: [] }); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, app]);

  function loadMore() {
    setLog((l) => ({ ...l, loadingMore: true }));
    app.ListCommits(projectID, sessionID, log.commits.length, PAGE_SIZE)
      .then((res) => setLog((l) => ({
        ...l,
        loadingMore: false,
        commits: [...l.commits, ...(res?.commits ?? [])],
        hasMore: !!res?.has_more,
      })))
      .catch((err) => setLog((l) => ({ ...l, loadingMore: false, moreError: err?.message ?? String(err) })));
  }

  function toggleCommit(sha) {
    if (expanded === sha) { setExpanded(null); return; }
    setExpanded(sha);
    if (details[sha]) return;
    setDetails((d) => ({ ...d, [sha]: { loading: true } }));
    app.GetCommit(projectID, sessionID, sha)
      .then((res) => setDetails((d) => ({ ...d, [sha]: { files: res?.files ?? [] } })))
      .catch((err) => setDetails((d) => ({ ...d, [sha]: { error: err?.message ?? String(err) } })));
  }

  let body;
  if (log.loading) body = <div className="explorer__hint explorer__hint--nested">Loading…</div>;
  else if (log.error) body = <div className="explorer__hint explorer__hint--nested explorer__hint--error">{log.error}</div>;
  else if (!log.commits.length) body = <div className="explorer__hint explorer__hint--nested">No commits yet.</div>;
  else {
    body = (
      <>
        {log.commits.map((c) => (
          <CommitRow
            key={c.sha}
            commit={c}
            open={expanded === c.sha}
            detail={details[c.sha]}
            onToggle={() => toggleCommit(c.sha)}
            onOpenFile={(path, pinned) => onOpenCommitDiff(c.sha, path, pinned)}
          />
        ))}
        {log.moreError && <div className="explorer__hint explorer__hint--nested explorer__hint--error">{log.moreError}</div>}
        {log.hasMore && (
          <button type="button" className="explorer__more" onClick={loadMore} disabled={log.loadingMore}>
            {log.loadingMore ? 'Loading…' : 'Load more'}
          </button>
        )}
      </>
    );
  }

  return (
    <ExplorerGroup
      label="Commits"
      open={open}
      onToggle={() => setOpen((v) => !v)}
      badge={log.branch && <span className="explorer__ref" title={`On branch ${log.branch}`}>{log.branch}</span>}
    >
      <div aria-label="Commits" className="explorer__group-body">{body}</div>
    </ExplorerGroup>
  );
}

function CommitRow({ commit, open, detail, onToggle, onOpenFile }) {
  const merge = commit.parents?.length > 1;
  const Icon = merge ? MergeIcon : CommitIcon;
  const when = commit.authored_at ? new Date(commit.authored_at).toLocaleString() : '';
  return (
    <>
      <button
        type="button"
        className={`explorer__commit${merge ? ' explorer__commit--merge' : ''}${open ? ' explorer__commit--open' : ''}`}
        onClick={onToggle}
        aria-expanded={open}
        title={`${commit.sha.slice(0, 8)} · ${commit.author} · ${when}\n${commit.subject}`}
      >
        <Icon />
        <span className="explorer__commit-subject">{commit.subject}</span>
        <span className="explorer__commit-meta">{formatSessionMeta(commit.authored_at)}</span>
      </button>
      {open && (
        <div className="explorer__commit-files" aria-label={`Files in ${commit.sha.slice(0, 8)}`}>
          {!detail || detail.loading ? (
            <div className="explorer__hint explorer__hint--deep">Loading…</div>
          ) : detail.error ? (
            <div className="explorer__hint explorer__hint--deep explorer__hint--error">{detail.error}</div>
          ) : detail.files.length === 0 ? (
            <div className="explorer__hint explorer__hint--deep">No file changes.</div>
          ) : (
            detail.files.map((f) => <ChangeRow key={`${f.status}:${f.path}`} file={f} depth={2} onOpen={onOpenFile} />)
          )}
        </div>
      )}
    </>
  );
}
