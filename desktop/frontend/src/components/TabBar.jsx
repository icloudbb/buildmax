// TabBar renders the center workspace tab strip: one entry per open tab, the
// active one highlighted, each closable. It is presentational — the tab model
// (src/lib/tabs.js) and the tab contents live above it.

const KIND_ICON = {
  chat: '💬',
  terminal: '>_',
  file: '📄',
  diff: '±',
};

export function TabBar({ tabs, activeKey, onSelect, onClose, onPin, onSplit }) {
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
      {onSplit && (
        <button
          type="button"
          className="workspace-tabs__split"
          title="Split right"
          aria-label="Split editor right"
          onClick={onSplit}
        >
          ◫
        </button>
      )}
    </div>
  );
}
