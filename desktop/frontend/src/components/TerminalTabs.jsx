import { useCallback, useEffect, useRef, useState } from 'react';
import { getApp } from '../lib/app';
import { TerminalPane } from './TerminalPane';

let seq = 0;

// TerminalTabs is the Desktop tab strip for shell strands (see the
// desktop-terminal-tabs proposal). Each tab is one local PTY opened at the
// project workspace; panes stay mounted so scrollback survives switching, and
// the strip reaps its strands when it unmounts (for example on project switch).
export function TerminalTabs({ projectId }) {
  const [tabs, setTabs] = useState([]); // { id, title }
  const [activeId, setActiveId] = useState(null);
  const openingRef = useRef(false);
  const tabsRef = useRef(tabs);
  useEffect(() => { tabsRef.current = tabs; }, [tabs]);

  const openTab = useCallback(async () => {
    const app = getApp();
    if (!app?.TerminalOpen || openingRef.current) return;
    openingRef.current = true;
    try {
      const id = await app.TerminalOpen(projectId);
      seq += 1;
      setTabs((prev) => [...prev, { id, title: `Terminal ${seq}` }]);
      setActiveId(id);
    } catch {
      // Opening a shell can fail (e.g. unsupported platform); leave the strip.
    } finally {
      openingRef.current = false;
    }
  }, [projectId]);

  const closeTab = useCallback((id) => {
    getApp()?.TerminalClose?.(id);
    setTabs((prev) => {
      const next = prev.filter((t) => t.id !== id);
      setActiveId((cur) => (cur === id ? (next[next.length - 1]?.id ?? null) : cur));
      return next;
    });
  }, []);

  // Open one terminal when the strip first mounts for a project.
  useEffect(() => {
    if (tabsRef.current.length === 0) openTab();
  }, [openTab]);

  // Reap every strand this strip owns when it unmounts, so no shell is orphaned
  // when the user switches projects or closes the panel.
  useEffect(() => () => {
    tabsRef.current.forEach((t) => getApp()?.TerminalClose?.(t.id));
  }, []);

  return (
    <div className="terminal-tabs">
      <div className="terminal-tabs__strip" role="tablist" aria-label="Terminals">
        {tabs.map((t) => (
          <div
            key={t.id}
            className={`terminal-tabs__tab${t.id === activeId ? ' terminal-tabs__tab--active' : ''}`}
          >
            <button
              type="button"
              role="tab"
              aria-selected={t.id === activeId}
              className="terminal-tabs__tab-btn"
              onClick={() => setActiveId(t.id)}
            >
              {t.title}
            </button>
            <button
              type="button"
              className="terminal-tabs__tab-close"
              aria-label={`Close ${t.title}`}
              onClick={() => closeTab(t.id)}
            >
              ×
            </button>
          </div>
        ))}
        <button
          type="button"
          className="terminal-tabs__add"
          onClick={openTab}
          title="New terminal"
          aria-label="New terminal"
        >
          +
        </button>
      </div>
      <div className="terminal-tabs__panes">
        {tabs.length === 0 && <div className="terminal-tabs__empty">No terminals open.</div>}
        {tabs.map((t) => (
          <TerminalPane key={t.id} id={t.id} active={t.id === activeId} />
        ))}
      </div>
    </div>
  );
}
