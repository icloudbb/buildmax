import { useEffect, useState } from 'react';
import { splitPathForDisplay, statusGlyph, truncateMiddleText } from '../lib/format';

// ExplorerChanges is the Changes mode of the project section: the workspace's
// modified files as a flat list whose clicks open a diff tab in the center.
export function ExplorerChanges({ projectID, sessionID, app, onOpenDiff }) {
  const [state, setState] = useState({ loading: true });

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

  if (state.loading) return <div className="explorer__hint">Loading…</div>;
  if (state.error) return <div className="explorer__hint explorer__hint--error">{state.error}</div>;
  if (!state.files.length) return <div className="explorer__hint">No uncommitted changes.</div>;

  return (
    <div className="explorer__changes" aria-label="Changed files">
      {state.files.map((f) => {
        const parts = splitPathForDisplay(f.path);
        return (
          <button
            key={`${f.status}:${f.path}`}
            type="button"
            className="explorer__change"
            onClick={() => onOpenDiff(f.path)}
            onDoubleClick={() => onOpenDiff(f.path, true)}
            title={f.path}
          >
            <span className={`explorer__change-status diff-drawer__status--${f.status}`}>{statusGlyph(f.status)}</span>
            <span className="explorer__change-name">{truncateMiddleText(parts.name, 30)}</span>
            {parts.dir && <span className="explorer__change-dir">{parts.dir}</span>}
            {(f.additions > 0 || f.deletions > 0) && (
              <span className="explorer__change-counts">+{f.additions} -{f.deletions}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}
