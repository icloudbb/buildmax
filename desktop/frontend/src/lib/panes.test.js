import { describe, it, expect } from 'vitest';
import {
  emptyWorkspace, openInFocused, focusPaneTab, focusPane, pinPaneTab, closePaneTab,
  closeOtherPaneTabs, closeRightPaneTabs,
  splitRight, splitDown, moveTab, focusedPane, allTabs, pruneForPersist, isWorkspace,
  collapse, tile,
} from './panes';

// openTab (via openInFocused) computes each tab's `key`, so tests open by path.
const open = (ws, path, preview = false) => openInFocused(ws, { kind: 'file', ref: path, title: path, preview });

function paneIds(ws) {
  return ws.rows.map((row) => row.panes.map((p) => p.id));
}

describe('panes grid model', () => {
  it('opens activities in the focused pane', () => {
    const ws = open(emptyWorkspace, 'a.go');
    expect(paneCount(ws)).toBe(1);
    expect(focusedPane(ws).activeKey).toBe('file:a.go');
  });

  it('splits right into a new focused empty pane in the same row', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitRight(ws);
    expect(paneIds(ws)).toEqual([['pane-1', 'pane-2']]);
    expect(ws.focused).toBe('pane-2');
    expect(focusedPane(ws).tabs).toHaveLength(0);
    expect(ws.rows[0].panes[0].activeKey).toBe('file:a.go');
  });

  it('splits down into a new row', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitDown(ws);
    expect(paneIds(ws)).toEqual([['pane-1'], ['pane-2']]);
    expect(ws.focused).toBe('pane-2');
  });

  it('lands newly opened activities in the split pane', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitRight(ws);
    ws = open(ws, 'b.go');
    expect(ws.rows[0].panes[0].activeKey).toBe('file:a.go');
    expect(ws.rows[0].panes[1].activeKey).toBe('file:b.go');
  });

  it('reaps an empty pane when focus leaves it', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitRight(ws);
    ws = focusPaneTab(ws, 'pane-1', 'file:a.go');
    expect(paneCount(ws)).toBe(1);
    expect(ws.focused).toBe('pane-1');
  });

  it('reaps an emptied row', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitDown(ws); // row-2 / pane-2 empty, focused
    ws = focusPaneTab(ws, 'pane-1', 'file:a.go');
    expect(ws.rows).toHaveLength(1);
  });

  it('removes a pane when its last tab closes and focuses a neighbour', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitRight(ws);
    ws = open(ws, 'b.go');
    ws = closePaneTab(ws, 'pane-2', 'file:b.go');
    expect(paneCount(ws)).toBe(1);
    expect(ws.focused).toBe('pane-1');
  });

  it('moves a tab to another pane and focuses it there', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = open(ws, 'b.go'); // pane-1 has a.go + b.go
    ws = splitRight(ws); // pane-2 empty, focused
    ws = moveTab(ws, 'pane-1', 'file:a.go', 'pane-2');
    expect(ws.rows[0].panes[0].tabs.map((t) => t.key)).toEqual(['file:b.go']);
    expect(ws.rows[0].panes[1].tabs.map((t) => t.key)).toEqual(['file:a.go']);
    expect(ws.focused).toBe('pane-2');
    expect(ws.rows[0].panes[1].activeKey).toBe('file:a.go');
  });

  it('removes the source pane when a move empties it', () => {
    let ws = open(emptyWorkspace, 'a.go'); // pane-1: a.go
    ws = splitRight(ws); // pane-2 empty, focused
    ws = open(ws, 'b.go'); // pane-2: b.go
    ws = moveTab(ws, 'pane-1', 'file:a.go', 'pane-2'); // pane-1 empties
    expect(paneCount(ws)).toBe(1);
    expect(ws.rows[0].panes[0].tabs.map((t) => t.key)).toEqual(['file:b.go', 'file:a.go']);
    expect(ws.focused).toBe('pane-2');
  });

  it('treats a move onto the same pane as a focus', () => {
    let ws = open(emptyWorkspace, 'a.go');
    const before = ws;
    ws = moveTab(ws, 'pane-1', 'file:a.go', 'pane-1');
    expect(paneCount(ws)).toBe(1);
    expect(ws.rows).toEqual(before.rows);
  });

  it('reorders a tab within its pane by dropping it before another', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = open(ws, 'b.go');
    ws = open(ws, 'c.go'); // pane-1: a, b, c
    ws = moveTab(ws, 'pane-1', 'file:c.go', 'pane-1', 'file:a.go');
    expect(ws.rows[0].panes[0].tabs.map((t) => t.key)).toEqual(['file:c.go', 'file:a.go', 'file:b.go']);
    expect(ws.rows[0].panes[0].activeKey).toBe('file:c.go');
  });

  it('inserts a cross-pane move at the drop position', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = open(ws, 'b.go'); // pane-1: a, b
    ws = splitRight(ws); // pane-2 empty, focused
    ws = open(ws, 'x.go');
    ws = open(ws, 'y.go'); // pane-2: x, y
    ws = moveTab(ws, 'pane-1', 'file:a.go', 'pane-2', 'file:y.go');
    expect(ws.rows[0].panes[1].tabs.map((t) => t.key)).toEqual(['file:x.go', 'file:a.go', 'file:y.go']);
  });

  it('closes other tabs in a pane, keeping the anchor', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = open(ws, 'b.go');
    ws = open(ws, 'c.go');
    ws = closeOtherPaneTabs(ws, 'pane-1', 'file:b.go');
    expect(ws.rows[0].panes[0].tabs.map((t) => t.key)).toEqual(['file:b.go']);
    expect(ws.focused).toBe('pane-1');
  });

  it('closes tabs to the right in a pane', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = open(ws, 'b.go');
    ws = open(ws, 'c.go');
    ws = closeRightPaneTabs(ws, 'pane-1', 'file:a.go');
    expect(ws.rows[0].panes[0].tabs.map((t) => t.key)).toEqual(['file:a.go']);
  });

  it('pins a preview tab in its pane', () => {
    let ws = openInFocused(emptyWorkspace, { kind: 'file', ref: 'a.go', title: 'a.go', preview: true });
    ws = pinPaneTab(ws, 'pane-1', 'file:a.go');
    expect(ws.rows[0].panes[0].tabs[0].preview).toBe(false);
  });

  it('keeps the empty split pane while it stays focused', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = splitRight(ws);
    ws = focusPane(ws, 'pane-2'); // already focused: no-op
    expect(paneCount(ws)).toBe(2);
  });
});

describe('workspace persistence', () => {
  const term = (ws) => openInFocused(ws, { kind: 'terminal', ref: 't1', title: 'Terminal 1' });

  it('keeps terminal tabs but strips their dead PTY ref for a fresh restore', () => {
    let ws = open(emptyWorkspace, 'a.go'); // pane-1: a.go
    ws = splitRight(ws); // pane-2 empty, focused
    ws = term(ws); // pane-2: terminal only
    const pruned = pruneForPersist(ws);
    expect(pruned.rows[0].panes).toHaveLength(2);
    const t = allTabs(pruned).find((x) => x.kind === 'terminal');
    expect(t).toBeTruthy();
    expect(t.ref).toBe(''); // the dead PTY id is dropped; restore reopens a shell
    expect(t.key).toBe('terminal:');
    expect(isWorkspace(pruned)).toBe(true);
  });

  it('keeps file and diff tabs and is a valid workspace', () => {
    let ws = open(emptyWorkspace, 'a.go');
    ws = openInFocused(ws, { kind: 'diff', ref: 'a.go', title: 'a.go (diff)' });
    const pruned = pruneForPersist(ws);
    expect(isWorkspace(pruned)).toBe(true);
    expect(allTabs(pruned).map((t) => t.kind).sort()).toEqual(['diff', 'file']);
  });

  it('keeps a layout that is only terminals', () => {
    const ws = term(emptyWorkspace);
    const pruned = pruneForPersist(ws);
    expect(pruned).not.toBeNull();
    expect(allTabs(pruned).map((t) => t.kind)).toEqual(['terminal']);
  });

  it('reassigns focus if the focused pane was empty and pruned away', () => {
    let ws = open(emptyWorkspace, 'a.go'); // pane-1: a.go
    ws = splitRight(ws); // pane-2 empty, focused
    const pruned = pruneForPersist(ws); // the empty pane-2 is dropped
    expect(pruned.focused).toBe('pane-1');
  });

  it('rejects malformed restored values', () => {
    expect(isWorkspace(null)).toBe(false);
    expect(isWorkspace({ rows: [], focused: 'x', seq: 1 })).toBe(false);
    expect(isWorkspace({ rows: [{ id: 'r', panes: [] }], focused: 'x', seq: 1 })).toBe(false);
    expect(isWorkspace(emptyWorkspace)).toBe(true);
  });
});

describe('grid / tab toggle', () => {
  const four = () => {
    let ws = open(emptyWorkspace, 'a');
    ws = open(ws, 'b');
    ws = open(ws, 'c');
    ws = open(ws, 'd');
    return ws;
  };

  it('tiles every tab into its own pane in a near-square grid', () => {
    const ws = tile(four());
    expect(paneCount(ws)).toBe(4);
    expect(ws.rows.length).toBe(2);
    expect(ws.rows.every((r) => r.panes.length === 2)).toBe(true);
    expect(ws.rows.flatMap((r) => r.panes).every((p) => p.tabs.length === 1)).toBe(true);
    expect(allTabs(ws).map((t) => t.ref)).toEqual(['a', 'b', 'c', 'd']);
  });

  it('keeps the active tab focused after tiling and gives every pane a unique growing id', () => {
    const ws = tile(four());
    expect(focusedPane(ws).tabs[0].ref).toBe('d');
    const ids = ws.rows.flatMap((r) => [r.id, ...r.panes.map((p) => p.id)]);
    expect(new Set(ids).size).toBe(ids.length);
    // seq covers every id assigned, so a later split cannot reuse one.
    expect(ws.seq).toBeGreaterThanOrEqual(4);
    expect(isWorkspace(ws)).toBe(true);
  });

  it('does not tile a single tab', () => {
    const ws = open(emptyWorkspace, 'only');
    expect(tile(ws)).toBe(ws);
  });

  it('caps the grid at three columns', () => {
    let ws = emptyWorkspace;
    for (const ref of ['a', 'b', 'c', 'd', 'e', 'f', 'g']) ws = open(ws, ref);
    ws = tile(ws);
    expect(paneCount(ws)).toBe(7);
    expect(ws.rows.every((r) => r.panes.length <= 3)).toBe(true);
    // 7 tabs → rows of 3, 3, 1.
    expect(ws.rows.map((r) => r.panes.length)).toEqual([3, 3, 1]);
  });

  it('collapses a grid back into one pane holding every tab in order', () => {
    const ws = collapse(tile(four()));
    expect(paneCount(ws)).toBe(1);
    expect(ws.rows.length).toBe(1);
    expect(allTabs(ws).map((t) => t.ref)).toEqual(['a', 'b', 'c', 'd']);
    const active = focusedPane(ws).tabs.find((t) => t.ref === 'd');
    expect(focusedPane(ws).activeKey).toBe(active.key);
  });
});

function paneCount(ws) {
  return ws.rows.reduce((n, row) => n + row.panes.length, 0);
}
