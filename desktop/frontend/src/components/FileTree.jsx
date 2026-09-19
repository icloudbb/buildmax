import { useEffect, useState } from 'react';
import { usePaneResize } from '../lib/usePaneResize';
import { useWorkspaceDir } from '../lib/useWorkspaceDir';
import { DirTree } from './DirTree';
import { FileView } from './FileView';

// FileTree browses a project's workspace in the inspector: a lazily expanded
// directory tree beside a preview of the selected file. The tree and preview are
// shared with the Explorer (DirTree, FileView); FileTree only wires selection.
export function FileTree({ projectID, sessionID, app }) {
  const { byDir, expanded, toggleDir } = useWorkspaceDir(projectID, sessionID, app);
  const [selected, setSelected] = useState('');
  // Tree column width in the expanded (side-by-side) layout only.
  const { width: treeWidth, ref: browserRef, onMouseDown: startTreeResize } = usePaneResize('bm.desktop.fileTreeWidth', 260);

  // Clear the selection when the workspace changes under the tree.
  useEffect(() => { setSelected(''); }, [projectID, sessionID]);

  return (
    <div className="file-browser" ref={browserRef} style={{ '--tree-w': `${treeWidth}px` }}>
      <div className="file-browser__tree" aria-label="Workspace files">
        <DirTree byDir={byDir} expanded={expanded} toggleDir={toggleDir} onFileClick={setSelected} activePath={selected} />
      </div>
      <div
        className="file-browser__resizer"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize file tree"
        onMouseDown={startTreeResize}
      />
      <div className="file-browser__view" aria-label="File preview">
        <FileView projectID={projectID} sessionID={sessionID} path={selected} app={app} />
      </div>
    </div>
  );
}
