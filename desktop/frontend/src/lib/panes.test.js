import { describe, it, expect } from 'vitest';
import {
  emptyWorkspace, openInFocused, focusPaneTab, focusPane, pinPaneTab, closePaneTab,
  splitFocused, focusedPane,
} from './panes';

const file = (path, preview = false) => ({ kind: 'file', ref: path, title: path, preview });

describe('panes workspace model', () => {
  it('opens activities in the focused pane', () => {
    const ws = openInFocused(emptyWorkspace, file('a.go'));
    expect(ws.panes).toHaveLength(1);
    expect(focusedPane(ws).activeKey).toBe('file:a.go');
  });

  it('splits into a new focused empty pane after the current one', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = splitFocused(ws);
    expect(ws.panes).toHaveLength(2);
    expect(ws.focused).toBe('pane-2');
    expect(focusedPane(ws).tabs).toHaveLength(0);
    // The original pane keeps its tab.
    expect(ws.panes[0].activeKey).toBe('file:a.go');
  });

  it('lands newly opened activities in the split pane', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = splitFocused(ws);
    ws = openInFocused(ws, file('b.go'));
    expect(ws.panes[0].activeKey).toBe('file:a.go');
    expect(ws.panes[1].activeKey).toBe('file:b.go');
  });

  it('reaps an empty pane when focus leaves it', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = splitFocused(ws); // pane-2 empty, focused
    ws = focusPaneTab(ws, 'pane-1', 'file:a.go');
    expect(ws.panes).toHaveLength(1);
    expect(ws.focused).toBe('pane-1');
  });

  it('keeps the empty split pane while it stays focused', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = splitFocused(ws);
    ws = focusPane(ws, 'pane-2'); // already focused: no-op, not reaped
    expect(ws.panes).toHaveLength(2);
  });

  it('removes a pane when its last tab closes and focuses a neighbour', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = splitFocused(ws);
    ws = openInFocused(ws, file('b.go'));
    ws = closePaneTab(ws, 'pane-2', 'file:b.go');
    expect(ws.panes).toHaveLength(1);
    expect(ws.focused).toBe('pane-1');
  });

  it('keeps a pane that still has tabs after a close', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = openInFocused(ws, file('b.go'));
    ws = closePaneTab(ws, 'pane-1', 'file:b.go');
    expect(ws.panes).toHaveLength(1);
    expect(ws.panes[0].activeKey).toBe('file:a.go');
  });

  it('pins a preview tab in its pane', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go', true));
    ws = pinPaneTab(ws, 'pane-1', 'file:a.go');
    expect(ws.panes[0].tabs[0].preview).toBe(false);
  });

  it('opens the same file independently in two panes', () => {
    let ws = openInFocused(emptyWorkspace, file('a.go'));
    ws = splitFocused(ws);
    ws = openInFocused(ws, file('a.go'));
    expect(ws.panes[0].tabs).toHaveLength(1);
    expect(ws.panes[1].tabs).toHaveLength(1);
  });
});
