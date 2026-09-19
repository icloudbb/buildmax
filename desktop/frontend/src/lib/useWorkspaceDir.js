import { useCallback, useEffect, useState } from 'react';

// useWorkspaceDir lazily loads a project's workspace as an expandable directory
// tree (App.ListWorkspaceDir), one level at a time so a large repository is
// never walked up front. Shared by the Explorer tree and the inspector file
// browser. A session may run in a worktree distinct from the project default,
// so it reloads from the root whenever project or session changes.
export function useWorkspaceDir(projectID, sessionID, app) {
  // dir path ("" is the root) -> { entries, error, loading }
  const [byDir, setByDir] = useState({});
  const [expanded, setExpanded] = useState(() => new Set(['']));

  const loadDir = useCallback((dir) => {
    if (!app?.ListWorkspaceDir) {
      setByDir((m) => ({ ...m, [dir]: { entries: [], error: 'Rebuild the desktop app to browse files.', loading: false } }));
      return;
    }
    setByDir((m) => ({ ...m, [dir]: { ...(m[dir] ?? {}), loading: true } }));
    app.ListWorkspaceDir(projectID, sessionID, dir)
      .then((res) => setByDir((m) => ({ ...m, [dir]: { entries: res?.entries ?? [], error: res?.error ?? null, loading: false } })))
      .catch((err) => setByDir((m) => ({ ...m, [dir]: { entries: [], error: err?.message ?? String(err), loading: false } })));
  }, [projectID, sessionID, app]);

  useEffect(() => {
    setByDir({});
    setExpanded(new Set(['']));
    loadDir('');
  }, [projectID, sessionID, loadDir]);

  const toggleDir = useCallback((dir) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(dir)) next.delete(dir);
      else {
        next.add(dir);
        if (!byDir[dir]) loadDir(dir);
      }
      return next;
    });
  }, [byDir, loadDir]);

  return { byDir, expanded, toggleDir };
}
