import { Chevron, FileIcon, FolderIcon } from './icons';

// DirTree renders a lazily-expanded workspace directory tree from the state a
// useWorkspaceDir hook holds. It is presentational: clicking a directory calls
// toggleDir; a single file click calls onFileClick(path) (preview) and a
// double-click calls onFileOpen(path) (pin).
export function DirTree({ byDir, expanded, toggleDir, onFileClick, onFileOpen, activePath }) {
  function renderEntries(dir, depth) {
    const node = byDir[dir];
    const pad = { paddingLeft: `${0.5 + depth * 0.85}rem` };
    if (!node || (node.loading && !node.entries)) {
      return <div className="file-tree__hint" style={pad}>Loading…</div>;
    }
    if (node.error) {
      return <div className="file-tree__hint file-tree__hint--error" style={pad}>{node.error}</div>;
    }
    if (node.entries.length === 0) {
      return <div className="file-tree__hint" style={pad}>Empty</div>;
    }
    return node.entries.map((e) => {
      const open = e.is_dir && expanded.has(e.path);
      const active = !e.is_dir && activePath === e.path;
      return (
        <div key={e.path}>
          <div
            className={`file-tree__row ${e.is_dir ? 'file-tree__row--dir' : 'file-tree__row--file'} ${active ? 'file-tree__row--active' : ''}`}
            style={pad}
            onClick={e.is_dir ? () => toggleDir(e.path) : () => onFileClick(e.path)}
            onDoubleClick={e.is_dir ? undefined : () => onFileOpen?.(e.path)}
            role="button"
            title={e.path}
          >
            <span className="file-tree__caret" aria-hidden>{e.is_dir && <Chevron open={open} />}</span>
            <span className="file-tree__icon" aria-hidden>{e.is_dir ? <FolderIcon /> : <FileIcon />}</span>
            <span className="file-tree__name">{e.name}</span>
          </div>
          {open && renderEntries(e.path, depth + 1)}
        </div>
      );
    });
  }

  return renderEntries('', 0);
}
