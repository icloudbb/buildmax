// The workspace layout layer over the tab model (see the desktop-workspace-tabs
// proposal). A workspace is a row of panes; each pane is a `tabs.js` state
// (tabs + activeKey), so every pane operation reuses the tested tab model. One
// pane is `focused`: it receives newly opened activities and is highlighted when
// more than one pane is on screen.
//
// Splitting never moves a tab between panes — it creates a new empty pane and
// focuses it, and the next opened file/diff/terminal lands there. Keeping a
// tab's backing (session/PTY/file) in one pane for its whole life means a
// terminal's emulator is never unmounted by a layout change, so scrollback
// survives. An empty pane is transient: it is reaped as soon as focus leaves it.

import { emptyTabs, openTab, focusTab, closeTab, pinTab } from './tabs';

export const emptyWorkspace = { panes: [{ id: 'pane-1', ...emptyTabs }], focused: 'pane-1', seq: 1 };

function updatePane(ws, paneId, fn) {
  return {
    ...ws,
    panes: ws.panes.map((p) => (p.id === paneId ? { ...p, ...fn(p) } : p)),
  };
}

// reap drops empty panes that are not focused, always keeping at least one pane.
// A just-split (empty, focused) pane is kept until the user puts something in it
// or focuses elsewhere.
function reap(ws) {
  if (ws.panes.length <= 1) return ws;
  const panes = ws.panes.filter((p) => p.tabs.length > 0 || p.id === ws.focused);
  if (panes.length === ws.panes.length) return ws;
  const focused = panes.some((p) => p.id === ws.focused) ? ws.focused : panes[panes.length - 1].id;
  return { ...ws, panes, focused };
}

export function focusedPane(ws) {
  return ws.panes.find((p) => p.id === ws.focused) ?? ws.panes[0];
}

// openInFocused opens (or focuses) an activity in the focused pane.
export function openInFocused(ws, tab) {
  return updatePane(ws, ws.focused, (p) => openTab(p, tab));
}

// focusPaneTab focuses a tab and makes its pane the focused one.
export function focusPaneTab(ws, paneId, key) {
  return reap({ ...updatePane(ws, paneId, (p) => focusTab(p, key)), focused: paneId });
}

// focusPane moves focus to a pane without changing its active tab (e.g. a click
// in the pane's content), reaping the pane focus just left if it was empty.
export function focusPane(ws, paneId) {
  if (ws.focused === paneId || !ws.panes.some((p) => p.id === paneId)) return ws;
  return reap({ ...ws, focused: paneId });
}

export function pinPaneTab(ws, paneId, key) {
  return { ...updatePane(ws, paneId, (p) => pinTab(p, key)), focused: paneId };
}

// closePaneTab closes a tab and removes its pane if that empties it (unless it is
// the last pane), focusing a neighbouring pane.
export function closePaneTab(ws, paneId, key) {
  const w = updatePane(ws, paneId, (p) => closeTab(p, key));
  const pane = w.panes.find((p) => p.id === paneId);
  if (pane && pane.tabs.length === 0 && w.panes.length > 1) {
    const idx = w.panes.findIndex((p) => p.id === paneId);
    const panes = w.panes.filter((p) => p.id !== paneId);
    const neighbour = panes[idx] ?? panes[idx - 1];
    return { ...w, panes, focused: neighbour.id };
  }
  return w;
}

// splitFocused inserts a new empty pane after the focused one and focuses it.
export function splitFocused(ws) {
  const idx = ws.panes.findIndex((p) => p.id === ws.focused);
  const id = `pane-${ws.seq + 1}`;
  const pane = { id, ...emptyTabs };
  const panes = [...ws.panes.slice(0, idx + 1), pane, ...ws.panes.slice(idx + 1)];
  return reap({ ...ws, panes, focused: id, seq: ws.seq + 1 });
}
