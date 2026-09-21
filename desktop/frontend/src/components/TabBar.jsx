// TabBar renders the center workspace tab strip: one entry per open tab, the
// active one highlighted, each closable. It is presentational — the tab model
// (src/lib/tabs.js) and the tab contents live above it. Beyond selecting and
// closing, it supports reordering by drag (onTabDrop reports the tab to drop
// before, or null for the end), a trailing "+" that starts a new chat in this
// pane (onNewTab), and a right-click menu for bulk closes and, on file/diff
// tabs, copying the path.

import { useState } from 'react';

const KIND_ICON = {
  chat: '💬',
  terminal: '>_',
  file: '📄',
  diff: '±',
};

// dropBeforeKey turns a drop on a tab into the key to insert in front of: the
// tab itself when the pointer is on its left half, the next tab (or null, the
// end) when on its right half.
function dropBeforeKey(e, tabs, index) {
  const rect = e.currentTarget.getBoundingClientRect();
  const after = e.clientX > rect.left + rect.width / 2;
  if (!after) return tabs[index].key;
  return tabs[index + 1]?.key ?? null;
}

// A file or diff tab's title is its filename; its tooltip is the full workspace
// path (its ref) so the whole location is visible on hover. Other kinds show
// their title in both.
function tabTooltip(t) {
  return (t.kind === 'file' || t.kind === 'diff') ? (t.ref || t.title) : t.title;
}

// The familiar four-corner glyphs: arrows out to the corners for maximize,
// arrows in to the centre for restore.
function MaximizeIcon() {
  return (
    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M4 9V4h5M4 4l6 6M20 9V4h-5M20 4l-6 6M4 15v5h5M4 20l6-6M20 15v5h-5M20 20l-6-6" />
    </svg>
  );
}

function RestoreIcon() {
  return (
    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M10 4v6H4M10 10L4 4M14 4v6h6M14 10l6-6M10 20v-6H4M10 14l-6 6M14 20v-6h6M14 14l6 6" />
    </svg>
  );
}

export function TabBar({
  tabs, activeKey, onSelect, onClose, onPin, onRename, onNewTab, onSplitRight, onSplitDown,
  onToggleMaximize, maximized, onTabDragStart, onTabDragEnd, onTabDrop,
  onCloseOthers, onCloseRight, onCopyPath,
}) {
  // The open context menu ({ key, x, y }), the tab being renamed inline and its
  // draft text, and the live drop indicator ({ key, after }) while a tab is
  // dragged over the strip. All local view state.
  const [menu, setMenu] = useState(null);
  const [renaming, setRenaming] = useState(null);
  const [renameValue, setRenameValue] = useState('');
  const [hint, setHint] = useState(null);
  if (tabs.length === 0) return null;

  const menuTab = menu ? tabs.find((t) => t.key === menu.key) : null;
  const closeMenu = () => setMenu(null);
  const othersClosable = menuTab && tabs.some((t) => t.key !== menuTab.key && t.closable !== false);
  const menuIdx = menuTab ? tabs.findIndex((t) => t.key === menuTab.key) : -1;
  const rightClosable = menuTab && tabs.slice(menuIdx + 1).some((t) => t.closable !== false);
  const canCopyPath = menuTab && (menuTab.kind === 'file' || menuTab.kind === 'diff');
  // Chat and terminal tabs carry a user-editable title; file and diff tabs are
  // named by their path and cannot be renamed.
  const isRenamable = (t) => t.kind === 'chat' || t.kind === 'terminal';
  const canRename = menuTab && isRenamable(menuTab);

  const startRename = (t) => { setRenaming(t.key); setRenameValue(t.title); };
  const commitRename = () => {
    if (renaming) onRename?.(renaming, renameValue);
    setRenaming(null);
  };

  return (
    <div className="workspace-tabs__bar" role="tablist" aria-label="Open tabs">
      {tabs.map((t, i) => (
        <div
          key={t.key}
          className={[
            'workspace-tabs__tab',
            t.key === activeKey ? 'workspace-tabs__tab--active' : '',
            t.preview ? 'workspace-tabs__tab--preview' : '',
            hint && hint.key === t.key && !hint.after ? 'workspace-tabs__tab--drop-before' : '',
            hint && hint.key === t.key && hint.after ? 'workspace-tabs__tab--drop-after' : '',
          ].filter(Boolean).join(' ')}
          draggable={!!onTabDragStart}
          onDragStart={(e) => {
            e.dataTransfer.effectAllowed = 'move';
            onTabDragStart?.(t.key, e);
          }}
          onDragEnd={() => { setHint(null); onTabDragEnd?.(); }}
          onDragOver={onTabDrop ? (e) => {
            e.preventDefault();
            const rect = e.currentTarget.getBoundingClientRect();
            setHint({ key: t.key, after: e.clientX > rect.left + rect.width / 2 });
          } : undefined}
          onDragLeave={() => setHint((h) => (h && h.key === t.key ? null : h))}
          onDrop={onTabDrop ? (e) => {
            e.preventDefault();
            e.stopPropagation();
            const before = dropBeforeKey(e, tabs, i);
            setHint(null);
            onTabDrop(before);
          } : undefined}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ key: t.key, x: e.clientX, y: e.clientY });
          }}
        >
          {renaming === t.key ? (
            <input
              className="workspace-tabs__tab-rename"
              value={renameValue}
              autoFocus
              onChange={(e) => setRenameValue(e.target.value)}
              onBlur={commitRename}
              onKeyDown={(e) => {
                if (e.key === 'Enter') commitRename();
                if (e.key === 'Escape') setRenaming(null);
              }}
            />
          ) : (
            <button
              type="button"
              role="tab"
              aria-selected={t.key === activeKey}
              className="workspace-tabs__tab-btn"
              title={tabTooltip(t)}
              onClick={() => onSelect(t.key)}
              // Double-click renames a chat/terminal tab in place; on a preview
              // file/diff tab it pins it (there is nothing to rename).
              onDoubleClick={() => (isRenamable(t) ? startRename(t) : onPin?.(t.key))}
            >
              <span className="workspace-tabs__tab-icon" aria-hidden>{KIND_ICON[t.kind] ?? ''}</span>
              <span className="workspace-tabs__tab-title">{t.title}</span>
            </button>
          )}
          {t.closable !== false && (
            <button
              type="button"
              className="workspace-tabs__tab-close"
              aria-label={`Close ${t.title}`}
              onClick={() => onClose(t.key)}
            >
              ×
            </button>
          )}
        </div>
      ))}
      {onNewTab && (
        <button
          type="button"
          className="workspace-tabs__new"
          title="New chat"
          aria-label="New chat"
          onClick={onNewTab}
        >
          +
        </button>
      )}
      {(onSplitRight || onSplitDown || onToggleMaximize) && (
        <div className="workspace-tabs__splits">
          {onToggleMaximize && (
            <button
              type="button"
              className="workspace-tabs__split"
              title={maximized ? 'Restore grid' : 'Maximize pane'}
              aria-label={maximized ? 'Restore grid' : 'Maximize pane'}
              onClick={onToggleMaximize}
            >
              {maximized ? <RestoreIcon /> : <MaximizeIcon />}
            </button>
          )}
          {onSplitRight && (
            <button
              type="button"
              className="workspace-tabs__split"
              title="Split right"
              aria-label="Split pane right"
              onClick={onSplitRight}
            >
              ◫
            </button>
          )}
          {onSplitDown && (
            <button
              type="button"
              className="workspace-tabs__split"
              title="Split down"
              aria-label="Split pane down"
              onClick={onSplitDown}
            >
              ⊟
            </button>
          )}
        </div>
      )}
      {menuTab && (
        <>
          <div className="context-menu__backdrop" onClick={closeMenu} onContextMenu={(e) => { e.preventDefault(); closeMenu(); }} />
          <div className="context-menu context-menu--tab" style={{ position: 'fixed', top: menu.y, left: menu.x }} role="menu">
            {canRename && (
              <button type="button" className="context-menu__item" onClick={() => { const t = menuTab; closeMenu(); startRename(t); }}>Rename</button>
            )}
            {menuTab.closable !== false && (
              <button type="button" className="context-menu__item" onClick={() => { closeMenu(); onClose(menuTab.key); }}>Close</button>
            )}
            <button type="button" className="context-menu__item" disabled={!othersClosable} onClick={() => { closeMenu(); onCloseOthers?.(menuTab.key); }}>Close others</button>
            <button type="button" className="context-menu__item" disabled={!rightClosable} onClick={() => { closeMenu(); onCloseRight?.(menuTab.key); }}>Close tabs to the right</button>
            {canCopyPath && (
              <>
                <div className="context-menu__divider" />
                <button type="button" className="context-menu__item" onClick={() => { closeMenu(); onCopyPath?.(menuTab.key, false); }}>Copy relative path</button>
                <button type="button" className="context-menu__item" onClick={() => { closeMenu(); onCopyPath?.(menuTab.key, true); }}>Copy absolute path</button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
