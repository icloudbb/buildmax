import { useWorkspaceDir } from '../lib/useWorkspaceDir';
import { DirTree } from './DirTree';

// ExplorerTree is the Files mode of the project section: a workspace file tree
// whose file clicks open a file tab in the center.
export function ExplorerTree({ projectID, sessionID, app, onOpenFile }) {
  const { byDir, expanded, toggleDir } = useWorkspaceDir(projectID, sessionID, app);
  return (
    <div className="explorer__tree" aria-label="Workspace files">
      <DirTree
        byDir={byDir}
        expanded={expanded}
        toggleDir={toggleDir}
        onFileClick={(path) => onOpenFile(path)}
        onFileOpen={(path) => onOpenFile(path, true)}
      />
    </div>
  );
}
