import { ExplorerTree } from './ExplorerTree';
import { ExplorerChanges } from './ExplorerChanges';

// Explorer is the project-scoped sidebar section that indexes the active
// project's workspace (see docs/contribute/architecture/desktop.md). It has two
// display modes — Directory and Changes — and only browses: a click opens a
// file or diff tab in the center, it never renders content itself. Mode is
// controlled so a slash command (/diff) can switch it.
export function Explorer({ projectID, sessionID, app, mode, onModeChange, onOpenFile, onOpenDiff }) {
  const setMode = onModeChange;
  return (
    <div className="explorer" aria-label="Explorer">
      <div className="explorer__header">
        <span className="explorer__label">Explorer</span>
        <div className="explorer__modes" role="group" aria-label="Explorer mode">
          <button
            type="button"
            className={`explorer__mode-btn ${mode === 'directory' ? 'explorer__mode-btn--active' : ''}`}
            aria-pressed={mode === 'directory'}
            onClick={() => setMode('directory')}
          >
            Directory
          </button>
          <button
            type="button"
            className={`explorer__mode-btn ${mode === 'changes' ? 'explorer__mode-btn--active' : ''}`}
            aria-pressed={mode === 'changes'}
            onClick={() => setMode('changes')}
          >
            Changes
          </button>
        </div>
      </div>
      <div className="explorer__body">
        {mode === 'directory' ? (
          <ExplorerTree projectID={projectID} sessionID={sessionID} app={app} onOpenFile={onOpenFile} />
        ) : (
          <ExplorerChanges projectID={projectID} sessionID={sessionID} app={app} onOpenDiff={onOpenDiff} />
        )}
      </div>
    </div>
  );
}
