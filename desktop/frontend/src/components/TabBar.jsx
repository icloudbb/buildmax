// TabBar renders the center workspace tab strip: one entry per open tab, the
// active one highlighted, each closable. It is presentational — the tab model
// (src/lib/tabs.js) and the tab contents live above it. Beyond selecting and
// closing, it supports reordering by drag (onTabDrop reports the tab to drop
// before, or null for the end), a trailing "+" that starts a new chat in this
// pane (onNewTab), and a right-click menu for bulk closes and, on file/diff
// tabs, copying the path.

import { useState } from 'react';
import { useT } from '../i18n';

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

// The saved workspace layout keeps a tab's default title in English, so a
// language switch never leaves stale text in it. A default is translated here,
// where the tab is drawn; a title the person or a session gave is shown as is.
function tabTitle(tab, t) {
  if (tab.kind === 'chat' && tab.title === 'Chat') return t('chat.untitled');
  if (tab.kind === 'chat' && tab.title === 'New Chat') return t('chat.newChat');
  if (tab.kind === 'browser' && tab.title === 'Browser') return t('shell.browser');
  const terminal = tab.kind === 'terminal' && /^Terminal (\d+)$/.exec(tab.title ?? '');
  if (terminal) return t('chat.terminalN', { n: terminal[1] });
  return tab.title;
}

// A file or diff tab's title is its filename; its tooltip is the full workspace
// path (its ref) so the whole location is visible on hover, plus the commit for
// a commit's diff. Other kinds show their title in both.
function tabTooltip(tab, t) {
  if (tab.kind === 'diff' && tab.commit) return t('chat.tabs.atCommit', { path: tab.path, commit: tab.commit.slice(0, 8) });
  return (tab.kind === 'file' || tab.kind === 'diff') ? (tab.ref || tab.title) : tabTitle(tab, t);
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

export function AttentionDot() {
  const t = useT();
  return (
    <span className="attention-dot" role="status" title={t('chat.tabs.waiting')} aria-label={t('chat.tabs.waiting')} />
  );
}

export function TabBar({
  tabs, activeKey, onSelect, onClose, onPin, onRename, onNewTab, onSplitRight, onSplitDown,
  onToggleMaximize, maximized, onTabDragStart, onTabDragEnd, onTabDrop,
  onCloseOthers, onCloseRight, onCopyPath, waitingSessions,
}) {
  const t = useT();
  // The open context menu ({ key, x, y }), the tab being renamed inline and its
  // draft text, and the live drop indicator ({ key, after }) while a tab is
  // dragged over the strip. All local view state.
  const [menu, setMenu] = useState(null);
  const [renaming, setRenaming] = useState(null);
  const [renameValue, setRenameValue] = useState('');
  const [hint, setHint] = useState(null);
  if (tabs.length === 0) return null;

  const menuTab = menu ? tabs.find((tab) => tab.key === menu.key) : null;
  const closeMenu = () => setMenu(null);
  const othersClosable = menuTab && tabs.some((tab) => tab.key !== menuTab.key && tab.closable !== false);
  const menuIdx = menuTab ? tabs.findIndex((tab) => tab.key === menuTab.key) : -1;
  const rightClosable = menuTab && tabs.slice(menuIdx + 1).some((tab) => tab.closable !== false);
  const canCopyPath = menuTab && (menuTab.kind === 'file' || menuTab.kind === 'diff');
  // Chat and terminal tabs carry a user-editable title; file and diff tabs are
  // named by their path and cannot be renamed.
  const isRenamable = (tab) => tab.kind === 'chat' || tab.kind === 'terminal';
  const canRename = menuTab && isRenamable(menuTab);

  const startRename = (tab) => { setRenaming(tab.key); setRenameValue(tabTitle(tab, t)); };
  // An untouched edit is not a rename: committing it would store a translated
  // default title in place of the English one.
  const commitRename = () => {
    const tab = tabs.find((candidate) => candidate.key === renaming);
    if (tab && renameValue !== tabTitle(tab, t)) onRename?.(renaming, renameValue);
    setRenaming(null);
  };

  return (
    <div className="workspace-tabs__bar" role="tablist" aria-label={t('chat.tabs.label')}>
      {tabs.map((tab, i) => (
        <div
          key={tab.key}
          className={[
            'workspace-tabs__tab',
            tab.key === activeKey ? 'workspace-tabs__tab--active' : '',
            tab.preview ? 'workspace-tabs__tab--preview' : '',
            hint && hint.key === tab.key && !hint.after ? 'workspace-tabs__tab--drop-before' : '',
            hint && hint.key === tab.key && hint.after ? 'workspace-tabs__tab--drop-after' : '',
          ].filter(Boolean).join(' ')}
          draggable={!!onTabDragStart}
          onDragStart={(e) => {
            e.dataTransfer.effectAllowed = 'move';
            onTabDragStart?.(tab.key, e);
          }}
          onDragEnd={() => { setHint(null); onTabDragEnd?.(); }}
          onDragOver={onTabDrop ? (e) => {
            e.preventDefault();
            const rect = e.currentTarget.getBoundingClientRect();
            setHint({ key: tab.key, after: e.clientX > rect.left + rect.width / 2 });
          } : undefined}
          onDragLeave={() => setHint((h) => (h && h.key === tab.key ? null : h))}
          onDrop={onTabDrop ? (e) => {
            e.preventDefault();
            e.stopPropagation();
            const before = dropBeforeKey(e, tabs, i);
            setHint(null);
            onTabDrop(before);
          } : undefined}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ key: tab.key, x: e.clientX, y: e.clientY });
          }}
        >
          {renaming === tab.key ? (
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
              aria-selected={tab.key === activeKey}
              className="workspace-tabs__tab-btn"
              title={tabTooltip(tab, t)}
              onClick={() => onSelect(tab.key)}
              // Double-click renames a chat/terminal tab in place; on a preview
              // file/diff tab it pins it (there is nothing to rename).
              onDoubleClick={() => (isRenamable(tab) ? startRename(tab) : onPin?.(tab.key))}
            >
              <span className="workspace-tabs__tab-icon" aria-hidden>{KIND_ICON[tab.kind] ?? ''}</span>
              <span className="workspace-tabs__tab-title">{tabTitle(tab, t)}</span>
              {/* The active tab shows its own prompt; a tab behind it has no
                  other way to say its run is stopped on the user. */}
              {tab.kind === 'chat' && tab.key !== activeKey && waitingSessions?.has(tab.sessionId) && (
                <AttentionDot />
              )}
            </button>
          )}
          {tab.closable !== false && (
            <button
              type="button"
              className="workspace-tabs__tab-close"
              aria-label={t('chat.tabs.close', { title: tabTitle(tab, t) })}
              onClick={() => onClose(tab.key)}
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
          title={t('chat.tabs.newChat')}
          aria-label={t('chat.tabs.newChat')}
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
              title={maximized ? t('chat.tabs.restoreGrid') : t('chat.tabs.maximize')}
              aria-label={maximized ? t('chat.tabs.restoreGrid') : t('chat.tabs.maximize')}
              onClick={onToggleMaximize}
            >
              {maximized ? <RestoreIcon /> : <MaximizeIcon />}
            </button>
          )}
          {onSplitRight && (
            <button
              type="button"
              className="workspace-tabs__split"
              title={t('chat.tabs.splitRight')}
              aria-label={t('chat.tabs.splitRightLabel')}
              onClick={onSplitRight}
            >
              ◫
            </button>
          )}
          {onSplitDown && (
            <button
              type="button"
              className="workspace-tabs__split"
              title={t('chat.tabs.splitDown')}
              aria-label={t('chat.tabs.splitDownLabel')}
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
              <button type="button" className="context-menu__item" onClick={() => { const target = menuTab; closeMenu(); startRename(target); }}>{t('shell.rename')}</button>
            )}
            {menuTab.closable !== false && (
              <button type="button" className="context-menu__item" onClick={() => { closeMenu(); onClose(menuTab.key); }}>{t('shell.close')}</button>
            )}
            <button type="button" className="context-menu__item" disabled={!othersClosable} onClick={() => { closeMenu(); onCloseOthers?.(menuTab.key); }}>{t('chat.tabs.closeOthers')}</button>
            <button type="button" className="context-menu__item" disabled={!rightClosable} onClick={() => { closeMenu(); onCloseRight?.(menuTab.key); }}>{t('chat.tabs.closeRight')}</button>
            {canCopyPath && (
              <>
                <div className="context-menu__divider" />
                <button type="button" className="context-menu__item" onClick={() => { closeMenu(); onCopyPath?.(menuTab.key, false); }}>{t('chat.tabs.copyRelative')}</button>
                <button type="button" className="context-menu__item" onClick={() => { closeMenu(); onCopyPath?.(menuTab.key, true); }}>{t('chat.tabs.copyAbsolute')}</button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
