// The workspace layout layer over the tab model (see the desktop-workspace-tabs
// proposal §15). A workspace is a grid: rows of panes. Each pane is a `tabs.js`
// state (tabs + activeKey), so every pane operation reuses the tested tab model.
// One pane is `focused`: it receives newly opened activities and is highlighted
// when more than one pane is on screen.
//
// Splitting right adds a pane to the focused pane's row; splitting down adds a
// new row. Both create an *empty* focused pane — newly opened files, diffs, and
// terminals land there. A tab moves between panes only through moveTab (drag).
// Keeping a tab's backing (session/PTY/file) decoupled from its pane (§7.5) is
// what lets a terminal survive a move: the emulator is portalled, not remounted.
// An empty pane is transient and reaped as soon as focus leaves it.

import { emptyTabs, openTab, focusTab, closeTab, pinTab } from './tabs';

export const emptyWorkspace = {
  rows: [{ id: 'row-1', panes: [{ id: 'pane-1', ...emptyTabs }] }],
  focused: 'pane-1',
  seq: 1,
};

function allPanes(ws) {
  return ws.rows.flatMap((row) => row.panes);
}

function paneCount(ws) {
  return ws.rows.reduce((n, row) => n + row.panes.length, 0);
}

export function focusedPane(ws) {
  return allPanes(ws).find((p) => p.id === ws.focused) ?? allPanes(ws)[0];
}

function findPane(ws, paneId) {
  return allPanes(ws).find((p) => p.id === paneId) ?? null;
}

function updatePane(ws, paneId, fn) {
  return {
    ...ws,
    rows: ws.rows.map((row) => ({
      ...row,
      panes: row.panes.map((p) => (p.id === paneId ? { ...p, ...fn(p) } : p)),
    })),
  };
}

// removePane drops a pane, prunes an emptied row, and moves focus (if it was on
// that pane) to the nearest remaining pane in reading order.
function removePane(ws, paneId) {
  const order = allPanes(ws).map((p) => p.id);
  const idx = order.indexOf(paneId);
  const rows = ws.rows
    .map((row) => ({ ...row, panes: row.panes.filter((p) => p.id !== paneId) }))
    .filter((row) => row.panes.length > 0);
  const remaining = rows.flatMap((row) => row.panes).map((p) => p.id);
  const next = (order[idx + 1] && remaining.includes(order[idx + 1]) && order[idx + 1])
    || (order[idx - 1] && remaining.includes(order[idx - 1]) && order[idx - 1])
    || remaining[remaining.length - 1];
  const focused = ws.focused === paneId ? next : ws.focused;
  return { ...ws, rows, focused };
}

// reap removes empty panes that are not focused (and any row they emptied),
// always keeping at least one pane. A just-split empty pane stays until focus
// leaves it.
function reap(ws) {
  if (paneCount(ws) <= 1) return ws;
  let removed = false;
  const rows = ws.rows
    .map((row) => {
      const panes = row.panes.filter((p) => p.tabs.length > 0 || p.id === ws.focused);
      if (panes.length !== row.panes.length) removed = true;
      return { ...row, panes };
    })
    .filter((row) => row.panes.length > 0);
  if (!removed) return ws;
  const survivors = rows.flatMap((row) => row.panes).map((p) => p.id);
  const focused = survivors.includes(ws.focused) ? ws.focused : survivors[survivors.length - 1];
  return { ...ws, rows, focused };
}

// openInFocused opens (or focuses) an activity in the focused pane.
export function openInFocused(ws, tab) {
  return updatePane(ws, ws.focused, (p) => openTab(p, tab));
}

// focusPaneTab focuses a tab and makes its pane the focused one.
export function focusPaneTab(ws, paneId, key) {
  return reap({ ...updatePane(ws, paneId, (p) => focusTab(p, key)), focused: paneId });
}

// focusPane moves focus to a pane without changing its active tab, reaping the
// pane focus just left if it was empty.
export function focusPane(ws, paneId) {
  if (ws.focused === paneId || !findPane(ws, paneId)) return ws;
  return reap({ ...ws, focused: paneId });
}

export function pinPaneTab(ws, paneId, key) {
  return { ...updatePane(ws, paneId, (p) => pinTab(p, key)), focused: paneId };
}

// closePaneTab closes a tab and removes its pane if that empties it (unless it is
// the last pane), focusing a neighbour.
export function closePaneTab(ws, paneId, key) {
  const w = updatePane(ws, paneId, (p) => closeTab(p, key));
  const pane = findPane(w, paneId);
  if (pane && pane.tabs.length === 0 && paneCount(w) > 1) {
    return removePane(w, paneId);
  }
  return w;
}

// splitRight inserts a new empty pane after the focused one in its row.
export function splitRight(ws) {
  const id = `pane-${ws.seq + 1}`;
  const rows = ws.rows.map((row) => {
    const idx = row.panes.findIndex((p) => p.id === ws.focused);
    if (idx === -1) return row;
    return {
      ...row,
      panes: [...row.panes.slice(0, idx + 1), { id, ...emptyTabs }, ...row.panes.slice(idx + 1)],
    };
  });
  return reap({ ...ws, rows, focused: id, seq: ws.seq + 1 });
}

// splitDown inserts a new row (one empty pane) after the focused pane's row.
export function splitDown(ws) {
  const paneId = `pane-${ws.seq + 1}`;
  const rowId = `row-${ws.seq + 1}`;
  const rowIdx = ws.rows.findIndex((row) => row.panes.some((p) => p.id === ws.focused));
  const newRow = { id: rowId, panes: [{ id: paneId, ...emptyTabs }] };
  const rows = [...ws.rows.slice(0, rowIdx + 1), newRow, ...ws.rows.slice(rowIdx + 1)];
  return reap({ ...ws, rows, focused: paneId, seq: ws.seq + 1 });
}

// allTabs lists every open tab across the grid (used to reap terminals, find the
// chat tab, etc.).
export function allTabs(ws) {
  return ws.rows.flatMap((row) => row.panes.flatMap((p) => p.tabs));
}

// pruneForPersist strips what cannot be restored after a restart — terminal
// tabs, whose PTYs are gone — then drops any pane or row that leaves empty, so a
// saved layout never reopens a dead shell or a blank pane. Returns null when
// nothing worth restoring remains.
export function pruneForPersist(ws) {
  const rows = ws.rows
    .map((row) => ({
      id: row.id,
      panes: row.panes
        .map((p) => {
          const tabs = p.tabs.filter((t) => t.kind !== 'terminal');
          const activeKey = tabs.some((t) => t.key === p.activeKey)
            ? p.activeKey
            : (tabs.length ? tabs[tabs.length - 1].key : null);
          return { id: p.id, tabs, activeKey };
        })
        .filter((p) => p.tabs.length > 0),
    }))
    .filter((row) => row.panes.length > 0);
  if (rows.length === 0) return null;
  const ids = rows.flatMap((r) => r.panes).map((p) => p.id);
  const focused = ids.includes(ws.focused) ? ws.focused : ids[ids.length - 1];
  return { rows, focused, seq: ws.seq };
}

// isWorkspace lightly validates a value restored from storage before it is used
// as workspace state.
export function isWorkspace(v) {
  return !!v && Array.isArray(v.rows) && typeof v.focused === 'string' && typeof v.seq === 'number'
    && v.rows.length > 0
    && v.rows.every((r) => r && typeof r.id === 'string' && Array.isArray(r.panes) && r.panes.length > 0
      && r.panes.every((p) => p && typeof p.id === 'string' && Array.isArray(p.tabs)));
}

// moveTab moves a tab from one pane to another (drag-and-drop). The tab keeps its
// descriptor — and, because its backing is pane-independent, its live content —
// so a moved terminal never loses scrollback. Dropping onto the source pane, or
// onto a pane that already holds the tab, just focuses it. A source pane emptied
// by the move is removed.
export function moveTab(ws, fromPaneId, key, toPaneId) {
  const from = findPane(ws, fromPaneId);
  const tab = from?.tabs.find((t) => t.key === key);
  if (!tab) return ws;
  if (fromPaneId === toPaneId) return { ...ws, focused: toPaneId };
  if (!findPane(ws, toPaneId)) return ws;
  let w = updatePane(ws, fromPaneId, (p) => closeTab(p, key));
  w = updatePane(w, toPaneId, (p) => (
    p.tabs.some((t) => t.key === key)
      ? { activeKey: key }
      : { tabs: [...p.tabs, tab], activeKey: key }
  ));
  w = { ...w, focused: toPaneId };
  const src = findPane(w, fromPaneId);
  if (src && src.tabs.length === 0 && paneCount(w) > 1) {
    w = removePane(w, fromPaneId);
    // removePane may steal focus to a neighbour; the drop target keeps it.
    w = { ...w, focused: toPaneId };
  }
  return w;
}
