import { splitPathForDisplay, statusGlyph, truncateMiddleText } from '../lib/format';
import { Chevron } from './icons';

// ChangeRow is one changed file: a single click previews its diff, a
// double-click pins it. Both the workspace changes (depth 1, under their group)
// and a commit's files (depth 2, under the commit) use it.
export function ChangeRow({ file, depth = 1, onOpen }) {
  const parts = splitPathForDisplay(file.path);
  return (
    <button
      type="button"
      className={`explorer__change explorer__change--depth${depth}`}
      onClick={() => onOpen(file.path)}
      onDoubleClick={() => onOpen(file.path, true)}
      title={file.old_path ? `${file.old_path} → ${file.path}` : file.path}
    >
      <span className={`explorer__change-status diff-drawer__status--${file.status}`}>{statusGlyph(file.status)}</span>
      <span className="explorer__change-name">{truncateMiddleText(parts.name, 30)}</span>
      {parts.dir && <span className="explorer__change-dir">{parts.dir}</span>}
      {(file.additions > 0 || file.deletions > 0) && (
        <span className="explorer__change-counts">+{file.additions} -{file.deletions}</span>
      )}
    </button>
  );
}

// ExplorerGroup is a collapsible group inside the project section, drawn as a
// tree row so it needs no header style of its own.
export function ExplorerGroup({ label, open, onToggle, badge, children }) {
  return (
    <>
      <button type="button" className="explorer__group" onClick={onToggle} aria-expanded={open}>
        <Chevron open={open} />
        <span className="explorer__group-label">{label}</span>
        {badge}
      </button>
      {open && children}
    </>
  );
}
