import { describe, it, expect } from 'vitest';
import {
  tabIdentity, emptyTabs, openTab, focusTab, closeTab, pinTab, activeTab,
} from './tabs';

const file = (path, preview = false) => ({ kind: 'file', ref: path, title: path, preview });

describe('tabs model', () => {
  it('opens a tab and makes it active', () => {
    const s = openTab(emptyTabs, file('a.go'));
    expect(s.tabs).toHaveLength(1);
    expect(s.activeKey).toBe('file:a.go');
    expect(activeTab(s).title).toBe('a.go');
  });

  it('focuses an already-open activity instead of duplicating it', () => {
    let s = openTab(emptyTabs, file('a.go'));
    s = openTab(s, file('b.go'));
    expect(s.activeKey).toBe('file:b.go');
    s = openTab(s, file('a.go')); // re-open a.go
    expect(s.tabs).toHaveLength(2);
    expect(s.activeKey).toBe('file:a.go');
  });

  it('distinguishes a file tab from its diff tab', () => {
    let s = openTab(emptyTabs, file('a.go'));
    s = openTab(s, { kind: 'diff', ref: 'a.go', title: 'a.go (diff)' });
    expect(s.tabs).toHaveLength(2);
    expect(tabIdentity(s.tabs[0])).toBe('file:a.go');
    expect(tabIdentity(s.tabs[1])).toBe('diff:a.go');
  });

  it('replaces the current preview tab rather than accumulating', () => {
    let s = openTab(emptyTabs, file('a.go', true));
    s = openTab(s, file('b.go', true));
    expect(s.tabs).toHaveLength(1);
    expect(s.activeKey).toBe('file:b.go');
  });

  it('keeps a pinned tab when a new preview opens', () => {
    let s = openTab(emptyTabs, file('a.go', true));
    s = pinTab(s, 'file:a.go');
    s = openTab(s, file('b.go', true));
    expect(s.tabs.map((t) => t.key)).toEqual(['file:a.go', 'file:b.go']);
  });

  it('focuses the next neighbour when closing the active tab', () => {
    let s = openTab(emptyTabs, file('a.go'));
    s = openTab(s, file('b.go'));
    s = openTab(s, file('c.go'));
    s = focusTab(s, 'file:b.go');
    s = closeTab(s, 'file:b.go');
    expect(s.tabs.map((t) => t.key)).toEqual(['file:a.go', 'file:c.go']);
    expect(s.activeKey).toBe('file:c.go');
  });

  it('falls back to the previous neighbour when closing the last tab', () => {
    let s = openTab(emptyTabs, file('a.go'));
    s = openTab(s, file('b.go'));
    s = closeTab(s, 'file:b.go');
    expect(s.activeKey).toBe('file:a.go');
  });

  it('clears the active tab when the last one closes', () => {
    let s = openTab(emptyTabs, file('a.go'));
    s = closeTab(s, 'file:a.go');
    expect(s.tabs).toHaveLength(0);
    expect(s.activeKey).toBeNull();
    expect(activeTab(s)).toBeNull();
  });

  it('ignores closing or focusing an unknown tab', () => {
    const s = openTab(emptyTabs, file('a.go'));
    expect(closeTab(s, 'file:zzz')).toBe(s);
    expect(focusTab(s, 'file:zzz')).toBe(s);
  });
});
