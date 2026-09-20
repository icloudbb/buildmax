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

import { emptyTabs, openTab, focusTab, closeTab, pinTab, insertTab, closeOthers, closeToRight } from './tabs';

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

// closeOtherPaneTabs / closeRightPaneTabs are the tab-strip context-menu bulk
// closes. Both keep the anchor tab, so the pane never empties. Callers close any
// terminal PTYs among the removed tabs before applying these (see App).
export function closeOtherPaneTabs(ws, paneId, key) {
  return { ...updatePane(ws, paneId, (p) => closeOthers(p, key)), focused: paneId };
}

export function closeRightPaneTabs(ws, paneId, key) {
  return { ...updatePane(ws, paneId, (p) => closeToRight(p, key)), focused: paneId };
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

// collapse gathers every tab across the grid into a single pane, in reading
// order, and drops the other panes and rows: one click back from a grid to a
// tabbed pane, the inverse of tile. The focused pane's active tab stays active
// if it survives; otherwise the last tab is shown.
export function collapse(ws) {
  const tabs = allPanes(ws).flatMap((p) => p.tabs);
  const focused = focusedPane(ws);
  const row = ws.rows.find((r) => r.panes.some((p) => p.id === focused.id)) ?? ws.rows[0];
  const activeKey = tabs.some((t) => t.key === focused.activeKey)
    ? focused.activeKey
    : (tabs.length ? tabs[tabs.length - 1].key : null);
  return {
    rows: [{ id: row.id, panes: [{ id: focused.id, tabs, activeKey }] }],
    focused: focused.id,
    seq: ws.seq,
  };
}

// tile spreads every open tab into its own pane, laid out in a near-square grid
// capped at three columns, so a person can see them all at once instead of
// splitting and dragging by hand. It is the inverse of collapse. Every pane and
// row gets a fresh id from the monotonic seq, so no id repeats one a terminal
// portal or a later split may reuse. The tab that was active stays focused.
export function tile(ws) {
  const tabs = allPanes(ws).flatMap((p) => p.tabs);
  if (tabs.length <= 1) return ws;
  const focusedKey = focusedPane(ws).activeKey;
  const cols = Math.min(3, Math.ceil(Math.sqrt(tabs.length)));
  let seq = ws.seq;
  const rows = [];
  for (let i = 0; i < tabs.length; i += cols) {
    seq += 1;
    const rowId = `row-${seq}`;
    const panes = tabs.slice(i, i + cols).map((t) => {
      seq += 1;
      return { id: `pane-${seq}`, tabs: [t], activeKey: t.key };
    });
    rows.push({ id: rowId, panes });
  }
  const all = rows.flatMap((r) => r.panes);
  const focused = (all.find((p) => p.activeKey === focusedKey) ?? all[0]).id;
  return { rows, focused, seq };
}

// pruneForPersist prepares a layout for storage across a restart. Terminal tabs
// are kept (positions and titles), but their PTY id is dropped: the shell does
// not survive the process, so on restore each terminal is reopened as a fresh
// shell (see respawnTerminalTabs) rather than rebound to a dead one. Empty panes
// and rows are dropped so a saved layout never reopens a blank pane. Returns null
// when nothing worth restoring remains.
export function pruneForPersist(ws) {
  const rows = ws.rows
    .map((row) => ({
      id: row.id,
      panes: row.panes
        .map((p) => {
          // A terminal keeps its tab (title and restore key) but loses its dead
          // backend ref; the key is recomputed from an empty ref so storage holds
          // no stale PTY id. restoreKey ties it to its saved buffer snapshot.
          const tabs = p.tabs.map((t) => (t.kind === 'terminal'
            ? { kind: 'terminal', title: t.title, restoreKey: t.restoreKey, ref: '', key: 'terminal:' }
            : t));
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

// moveTab moves a tab by drag-and-drop, and also reorders one within its strip.
// The tab keeps its descriptor — and, because its backing is pane-independent,
// its live content — so a moved terminal never loses scrollback. beforeKey is
// the tab to drop in front of, or null to append (a drop on empty strip space).
// Within the source pane, a null (or self) target is just a focus, so dropping a
// tab back on its own pane never reshuffles it; a real target reorders it. A
// source pane emptied by a cross-pane move is removed.
export function moveTab(ws, fromPaneId, key, toPaneId, beforeKey = null) {
  const from = findPane(ws, fromPaneId);
  const tab = from?.tabs.find((t) => t.key === key);
  if (!tab) return ws;
  if (!findPane(ws, toPaneId)) return ws;
  if (fromPaneId === toPaneId) {
    if (beforeKey == null || beforeKey === key) return { ...ws, focused: toPaneId };
    return { ...updatePane(ws, toPaneId, (p) => insertTab(p, tab, beforeKey)), focused: toPaneId };
  }
  let w = updatePane(ws, fromPaneId, (p) => closeTab(p, key));
  w = updatePane(w, toPaneId, (p) => insertTab(p, tab, beforeKey));
  w = { ...w, focused: toPaneId };
  const src = findPane(w, fromPaneId);
  if (src && src.tabs.length === 0 && paneCount(w) > 1) {
    w = removePane(w, fromPaneId);
    // removePane may steal focus to a neighbour; the drop target keeps it.
    w = { ...w, focused: toPaneId };
  }
  return w;
}
