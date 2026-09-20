// TabBar renders the center workspace tab strip: one entry per open tab, the
// active one highlighted, each closable. It is presentational — the tab model
// (src/lib/tabs.js) and the tab contents live above it. Beyond selecting and
// closing, it supports reordering by drag (onTabDrop reports the tab to drop
// before, or null for the end) and a right-click menu for bulk closes and, on
// file/diff tabs, copying the path.

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

export function TabBar({
  tabs, activeKey, onSelect, onClose, onPin, onSplitRight, onSplitDown,
  onTabDragStart, onTabDragEnd, onTabDrop, onCloseOthers, onCloseRight, onCopyPath,
}) {
  // The open context menu ({ key, x, y }) and the live drop indicator ({ key,
  // after }) while a tab is dragged over the strip. Both are local view state.
  const [menu, setMenu] = useState(null);
  const [hint, setHint] = useState(null);
  if (tabs.length === 0) return null;

  const menuTab = menu ? tabs.find((t) => t.key === menu.key) : null;
  const closeMenu = () => setMenu(null);
  const othersClosable = menuTab && tabs.some((t) => t.key !== menuTab.key && t.closable !== false);
  const menuIdx = menuTab ? tabs.findIndex((t) => t.key === menuTab.key) : -1;
  const rightClosable = menuTab && tabs.slice(menuIdx + 1).some((t) => t.closable !== false);
  const canCopyPath = menuTab && (menuTab.kind === 'file' || menuTab.kind === 'diff');

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
          <button
            type="button"
            role="tab"
            aria-selected={t.key === activeKey}
            className="workspace-tabs__tab-btn"
            title={t.title}
            onClick={() => onSelect(t.key)}
            onDoubleClick={() => onPin?.(t.key)}
          >
            <span className="workspace-tabs__tab-icon" aria-hidden>{KIND_ICON[t.kind] ?? ''}</span>
            <span className="workspace-tabs__tab-title">{t.title}</span>
          </button>
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
      {(onSplitRight || onSplitDown) && (
        <div className="workspace-tabs__splits">
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
