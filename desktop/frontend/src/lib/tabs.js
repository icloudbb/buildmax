// The center workspace tab model (see the desktop-workspace-tabs proposal). A
// tab renders one activity — chat, terminal, file, or diff — and its backing is
// identified by (kind, ref) so opening the same activity twice focuses the open
// tab instead of duplicating it. This module is pure state; React state and the
// backends live in the components.

// tabIdentity is the stable key for a tab's backing. Terminals pass a unique
// backend id as ref, so terminal tabs never collide; chat/file/diff dedupe by
// their session id or workspace path.
export function tabIdentity(tab) {
  return `${tab.kind}:${tab.ref ?? ''}`;
}

export const emptyTabs = { tabs: [], activeKey: null };

// openTab opens an activity, or focuses it if already open. A preview tab (a
// single browse click) replaces the current preview rather than accumulating,
// so scanning the Explorer does not spray tabs.
export function openTab(state, tab) {
  const key = tabIdentity(tab);
  if (state.tabs.some((t) => t.key === key)) {
    return { ...state, activeKey: key };
  }
  let tabs = state.tabs;
  if (tab.preview) {
    tabs = tabs.filter((t) => !t.preview);
  }
  return { tabs: [...tabs, { ...tab, key }], activeKey: key };
}

export function focusTab(state, key) {
  if (!state.tabs.some((t) => t.key === key)) return state;
  return { ...state, activeKey: key };
}

// closeTab removes a tab and, if it was active, focuses its neighbour (the next
// tab, else the previous, else nothing).
export function closeTab(state, key) {
  const idx = state.tabs.findIndex((t) => t.key === key);
  if (idx === -1) return state;
  const tabs = state.tabs.filter((t) => t.key !== key);
  let { activeKey } = state;
  if (activeKey === key) {
    const neighbour = tabs[idx] ?? tabs[idx - 1] ?? null;
    activeKey = neighbour ? neighbour.key : null;
  }
  return { tabs, activeKey };
}

// pinTab promotes a preview tab to a durable one, so an explicit open survives
// the next browse click.
export function pinTab(state, key) {
  return {
    ...state,
    tabs: state.tabs.map((t) => (t.key === key ? { ...t, preview: false } : t)),
  };
}

export function activeTab(state) {
  return state.tabs.find((t) => t.key === state.activeKey) ?? null;
}

// insertTab places tab immediately before the tab keyed beforeKey — or at the
// end when beforeKey is null or not found — and activates it. A tab already in
// the strip is moved, not duplicated, so a drag can reorder it in place.
export function insertTab(state, tab, beforeKey = null) {
  const rest = state.tabs.filter((t) => t.key !== tab.key);
  const idx = beforeKey == null ? -1 : rest.findIndex((t) => t.key === beforeKey);
  const at = idx === -1 ? rest.length : idx;
  return { tabs: [...rest.slice(0, at), tab, ...rest.slice(at)], activeKey: tab.key };
}

// closeOthers keeps the tab keyed key (which becomes active) and drops the rest —
// the tab-strip "Close others" action. A non-closable tab (closable === false,
// e.g. the current chat) is kept regardless, mirroring the per-tab close button.
export function closeOthers(state, key) {
  if (!state.tabs.some((t) => t.key === key)) return state;
  const tabs = state.tabs.filter((t) => t.key === key || t.closable === false);
  return { tabs, activeKey: key };
}

// closeToRight drops every closable tab after the one keyed key. The active tab
// stays active when it survives, otherwise key does — the "Close tabs to the
// right" action. Non-closable tabs to the right are kept.
export function closeToRight(state, key) {
  const idx = state.tabs.findIndex((t) => t.key === key);
  if (idx === -1) return state;
  const tabs = state.tabs.filter((t, i) => i <= idx || t.closable === false);
  const activeKey = tabs.some((t) => t.key === state.activeKey) ? state.activeKey : key;
  return { tabs, activeKey };
}
