import { useEffect, useState } from 'react';
import { ChangeRow, ExplorerGroup } from './ExplorerRows';
import { ExplorerCommits } from './ExplorerCommits';
import { useStableT, useT } from '../i18n';

const NOT_A_REPO = 'not a git repository';

// ExplorerChanges is the Changes mode of the project section, modeled on a
// source-control view: the workspace's uncommitted changes, then the commit
// history of the workspace's HEAD. Clicks open diff tabs in the center.
export function ExplorerChanges({ projectID, sessionID, app, onOpenDiff, onOpenCommitDiff }) {
  const t = useT();
  const stableT = useStableT();
  const [state, setState] = useState({ loading: true });
  const [changesOpen, setChangesOpen] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setState({ loading: true });
    if (!app?.GetWorkspaceDiff) {
      setState({ error: stableT('explorer.rebuildChanges') });
      return undefined;
    }
    app.GetWorkspaceDiff(projectID, sessionID)
      .then((res) => { if (!cancelled) setState(res?.error ? { error: res.error } : { files: res?.files ?? [] }); })
      .catch((err) => { if (!cancelled) setState({ error: err?.message ?? String(err) }); });
    return () => { cancelled = true; };
  }, [projectID, sessionID, app, stableT]);

  // A plain-directory Project has neither changes nor history: one hint, not two.
  if (state.error === NOT_A_REPO) {
    return <div className="explorer__hint">{t('explorer.notRepo')}</div>;
  }

  let changes;
  if (state.loading) changes = <div className="explorer__hint explorer__hint--nested">{t('shell.loading')}</div>;
  else if (state.error) changes = <div className="explorer__hint explorer__hint--nested explorer__hint--error">{state.error}</div>;
  else if (!state.files.length) changes = <div className="explorer__hint explorer__hint--nested">{t('explorer.noChanges')}</div>;
  else changes = state.files.map((f) => <ChangeRow key={`${f.status}:${f.path}`} file={f} onOpen={onOpenDiff} />);

  return (
    <div className="explorer__changes" aria-label={t('explorer.changesAndCommits')}>
      <ExplorerGroup
        label={t('explorer.changes')}
        open={changesOpen}
        onToggle={() => setChangesOpen((v) => !v)}
        badge={state.files?.length > 0 && <span className="explorer__count">{state.files.length}</span>}
      >
        <div aria-label={t('explorer.changedFiles')} className="explorer__group-body">{changes}</div>
      </ExplorerGroup>
      <ExplorerCommits projectID={projectID} sessionID={sessionID} app={app} onOpenCommitDiff={onOpenCommitDiff} />
    </div>
  );
}
