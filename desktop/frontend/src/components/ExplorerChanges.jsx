import { useEffect, useState } from 'react';
import { ChangeRow, ExplorerGroup } from './ExplorerRows';
import { ExplorerCommits } from './ExplorerCommits';

const NOT_A_REPO = 'not a git repository';

// ExplorerChanges is the Changes mode of the project section, modeled on a
// source-control view: the workspace's uncommitted changes, then the commit
// history of the workspace's HEAD. Clicks open diff tabs in the center.
export function ExplorerChanges({ projectID, sessionID, app, onOpenDiff, onOpenCommitDiff }) {
  const [state, setState] = useState({ loading: true });
  const [changesOpen, setChangesOpen] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setState({ loading: true });
    if (!app?.GetWorkspaceDiff) {
      setState({ error: 'Rebuild the desktop app to view changes.' });
      return undefined;
    }
    app.GetWorkspaceDiff(projectID, sessionID)
      .then((res) => { if (!cancelled) setState(res?.error ? { error: res.error } : { files: res?.files ?? [] }); })
      .catch((err) => { if (!cancelled) setState({ error: err?.message ?? String(err) }); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, app]);

  // A plain-directory Project has neither changes nor history: one hint, not two.
  if (state.error === NOT_A_REPO) {
    return <div className="explorer__hint">Not a Git repository.</div>;
  }

  let changes;
  if (state.loading) changes = <div className="explorer__hint explorer__hint--nested">Loading…</div>;
  else if (state.error) changes = <div className="explorer__hint explorer__hint--nested explorer__hint--error">{state.error}</div>;
  else if (!state.files.length) changes = <div className="explorer__hint explorer__hint--nested">No uncommitted changes.</div>;
  else changes = state.files.map((f) => <ChangeRow key={`${f.status}:${f.path}`} file={f} onOpen={onOpenDiff} />);

  return (
    <div className="explorer__changes" aria-label="Changes and commits">
      <ExplorerGroup
        label="Changes"
        open={changesOpen}
        onToggle={() => setChangesOpen((v) => !v)}
        badge={state.files?.length > 0 && <span className="explorer__count">{state.files.length}</span>}
      >
        <div aria-label="Changed files" className="explorer__group-body">{changes}</div>
      </ExplorerGroup>
      <ExplorerCommits projectID={projectID} sessionID={sessionID} app={app} onOpenCommitDiff={onOpenCommitDiff} />
    </div>
  );
}
