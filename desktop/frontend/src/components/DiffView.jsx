import { useEffect, useState } from 'react';
import { FilePatch } from './FilePatch';

// DiffView renders one changed file's diff as a center tab: it loads the
// workspace diff and shows the patch for `path`. Opened by clicking a change in
// the Explorer's Changes mode.
export function DiffView({ projectID, sessionID, path, app }) {
  const [state, setState] = useState({ loading: true });

  useEffect(() => {
    let cancelled = false;
    setState({ loading: true });
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
      .catch((err) => { if (!cancelled) setState({ error: err?.message ?? String(err) }); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, path, app]);

  if (state.loading) return <p className="diff-drawer__empty">Loading…</p>;
  if (state.error) return <p className="diff-drawer__error">{state.error}</p>;
  if (!state.file) return <p className="diff-drawer__empty">No uncommitted changes for {path}.</p>;

  return (
    <div className="diff-view">
      <FilePatch file={state.file} />
    </div>
  );
}
