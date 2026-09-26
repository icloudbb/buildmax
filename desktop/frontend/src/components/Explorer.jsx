import { ExplorerTree } from './ExplorerTree';
import { ExplorerChanges } from './ExplorerChanges';
import { SidebarSectionHeader } from './SidebarSection';
import { FilesIcon, SourceControlIcon } from './icons';

const MODES = [
  { id: 'files', label: 'Files', Icon: FilesIcon },
  { id: 'changes', label: 'Changes', Icon: SourceControlIcon },
];

// Explorer is the active project's own sidebar section (see
// docs/contribute/architecture/desktop.md). Its header names the project so the
// section reads as belonging to it, not as a peer of the project list. It only
// browses: a click opens a file or diff tab in the center. Mode is controlled
// so a slash command (/diff) can switch it.
export function Explorer({
  projectID, projectName, sessionID, app, mode, onModeChange, open, onToggle, style, onOpenFile, onOpenDiff,
}) {
  return (
    <section
      className={`explorer${open ? '' : ' explorer--closed'}`}
      aria-label={`${projectName} workspace`}
      style={style}
    >
      <SidebarSectionHeader label={projectName} open={open} onToggle={onToggle}>
        <div className="explorer__modes" role="group" aria-label="Workspace view">
          {MODES.map(({ id, label, Icon }) => (
            <button
              key={id}
              type="button"
              className="sidebar__icon-btn explorer__mode-btn"
              aria-pressed={mode === id}
              aria-label={label}
              title={label}
              onClick={() => onModeChange(id)}
            >
              <Icon />
            </button>
          ))}
        </div>
      </SidebarSectionHeader>
      {open && (
        <div className="explorer__body">
          {mode === 'files' ? (
            <ExplorerTree projectID={projectID} sessionID={sessionID} app={app} onOpenFile={onOpenFile} />
          ) : (
            <ExplorerChanges projectID={projectID} sessionID={sessionID} app={app} onOpenDiff={onOpenDiff} />
          )}
        </div>
      )}
    </section>
  );
}
