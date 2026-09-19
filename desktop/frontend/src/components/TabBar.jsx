// TabBar renders the center workspace tab strip: one entry per open tab, the
// active one highlighted, each closable. It is presentational — the tab model
// (src/lib/tabs.js) and the tab contents live above it.

const KIND_ICON = {
  chat: '💬',
  terminal: '>_',
  file: '📄',
  diff: '±',
};

export function TabBar({
  tabs, activeKey, onSelect, onClose, onPin, onSplitRight, onSplitDown,
  onTabDragStart, onTabDragEnd,
}) {
  if (tabs.length === 0) return null;
  return (
    <div className="workspace-tabs__bar" role="tablist" aria-label="Open tabs">
      {tabs.map((t) => (
        <div
          key={t.key}
          className={[
            'workspace-tabs__tab',
            t.key === activeKey ? 'workspace-tabs__tab--active' : '',
            t.preview ? 'workspace-tabs__tab--preview' : '',
          ].filter(Boolean).join(' ')}
          draggable={!!onTabDragStart}
          onDragStart={(e) => {
            e.dataTransfer.effectAllowed = 'move';
            onTabDragStart?.(t.key, e);
          }}
          onDragEnd={() => onTabDragEnd?.()}
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
    </div>
  );
}
