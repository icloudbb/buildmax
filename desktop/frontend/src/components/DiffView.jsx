import { useEffect, useState } from 'react';
import { FilePatch } from './FilePatch';

// DiffView renders one changed file's diff as a center tab. Without `commit` it
// loads the workspace diff and shows the patch for `path`; with one it shows
// that commit's change to `path`. Opened from the project section's Changes
// mode, from either its uncommitted changes or its commit history.
export function DiffView({ projectID, sessionID, path, commit, app }) {
  const [state, setState] = useState({ loading: true });

  useEffect(() => {
    let cancelled = false;
    setState({ loading: true });
    const fail = (err) => { if (!cancelled) setState({ error: err?.message ?? String(err) }); };
    if (commit) {
      if (!app?.GetCommitFileDiff) {
        setState({ error: 'Rebuild the desktop app to view commit diffs.' });
        return undefined;
      }
      app.GetCommitFileDiff(projectID, sessionID, commit, path)
        .then((file) => { if (!cancelled) setState({ file }); })
        .catch(fail);
      return () => { cancelled = true; };
    }
    if (!app?.GetWorkspaceDiff) {
      setState({ error: 'Rebuild the desktop app to view diffs.' });
      return undefined;
    }
    app.GetWorkspaceDiff(projectID, sessionID)
      .then((res) => {
        if (cancelled) return;
        if (res?.error) { setState({ error: res.error }); return; }
        setState({ file: (res?.files ?? []).find((f) => f.path === path) ?? null });
      })
      .catch(fail);
    return () => { cancelled = true; };
  }, [projectID, sessionID, path, commit, app]);

  if (state.loading) return <p className="diff-drawer__empty">Loading…</p>;
  if (state.error) return <p className="diff-drawer__error">{state.error}</p>;
  if (!state.file) return <p className="diff-drawer__empty">No uncommitted changes for {path}.</p>;

  return (
    <div className="diff-view">
      <FilePatch file={state.file} />
    </div>
  );
}
