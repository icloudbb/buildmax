import { compareRecent } from './lib/format';
import { getApp } from './lib/app';
import { HomeDashboard } from './components/HomeDashboard';
import { CreateProjectModal, ConfirmModal } from './components/Modals';
import { Sidebar } from './components/Sidebar';
import { TerminalHost } from './components/TerminalHost';
import { ChatSession } from './components/ChatSession';
import { TabBar } from './components/TabBar';
import { FileView } from './components/FileView';
import { DiffView } from './components/DiffView';
import BrowserView from './components/BrowserView';
import { SchedulesView } from './components/SchedulesView';
import { IssuesView } from './components/IssuesView';
import { LaunchpadButton } from './components/LaunchpadButton';
import { GridIcon, MoonIcon, SidebarIcon, SplitRightIcon, SunIcon } from './components/icons';
import { readStored, writeStored } from './lib/storage';
import { activeTab, tabIdentity } from './lib/tabs';
import { withApproval, withoutApproval } from './lib/approvals';
import {
  emptyWorkspace, openInFocused, focusPaneTab, focusPane, pinPaneTab, closePaneTab,
  closeOtherPaneTabs, closeRightPaneTabs,
  splitRight, splitDown, moveTab, allTabs, pruneForPersist, isWorkspace,
  collapse, tile, closeTerminalTab,
} from './lib/panes';

import { useState, useRef, useEffect, useLayoutEffect, useMemo, useCallback } from 'react';
import { ThemeProvider, useTheme } from '@buildmax/gui';
import { EventsOn } from './lib/wailsRuntime';
import LoginPage from './LoginPage';

// Sidebar layout is a per-machine preference, remembered across runs.
const SIDEBAR_MIN_WIDTH = 180;
const SIDEBAR_MAX_WIDTH = 480;
const SIDEBAR_DEFAULT_WIDTH = 288;
const LS_SIDEBAR_COLLAPSED = 'bm.desktop.sidebarCollapsed';
const LS_SIDEBAR_WIDTH = 'bm.desktop.sidebarWidth';
// Workspace layout is remembered per project. Restoring reopens the tabs and the
// pane grid the user last left; terminal tabs come back as fresh shells seeded
// with their saved scrollback, since their PTYs do not survive a restart.
const workspaceStorageKey = (projectId) => `bm.desktop.workspace.${projectId}`;

// A stable per-terminal key for snapshot persistence: it survives a restart in
// the saved layout, so a reopened terminal reclaims its previous contents while
// the dead PTY id does not.
function newRestoreKey() {
  return globalThis.crypto?.randomUUID?.() ?? `rk-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function clampSidebarWidth(w) {
  const n = Number(w);
  if (!Number.isFinite(n)) return SIDEBAR_DEFAULT_WIDTH;
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, n));
}

// Theme toggle lives in the workspace status bar, always visible while a project
// is open — more reachable than the user menu, which hides with the sidebar. Its
// own component so it can call useTheme from inside ThemeProvider. The icon names
// the destination — moon to go dark, sun to go light.
function ThemeStatusButton() {
  const { theme, toggleTheme } = useTheme();
  const dark = theme === 'dark';
  return (
    <button
      type="button"
      className="workspace-statusbar__btn"
      title={dark ? 'Light mode' : 'Dark mode'}
      aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
      onClick={toggleTheme}
    >
      <span aria-hidden>{dark ? <SunIcon /> : <MoonIcon />}</span>
    </button>
  );
}

export default function App() {
  const [sessions, setSessions] = useState([]);
  // The primary selected session: it drives which project is active and what the
  // sidebar/Explorer follow. Each chat tab owns its own run state (see
  // ChatSession); this is only the selection, not a live transcript.
  const [selectedId, setSelectedId] = useState(null);
  // Said once when a project is opened: a memory file that will not load, or
  // a project registered beside one whose folder has moved. Neither is an
  // error, and both are invisible if nobody says them here.
  const [projectNotices, setProjectNotices] = useState([]);
  // Errors from project/session management (rename, delete, open); chat errors
  // live in their own tab.
  const [error, setError] = useState(null);
  const [wailsReady, setWailsReady] = useState(false);
  const [authStatus, setAuthStatus] = useState(null);
  const [signInOpen, setSignInOpen] = useState(false);
  // A pending destructive confirmation ({ title, message, confirmLabel, onConfirm }),
  // shown in an in-app ConfirmModal. window.confirm is unreliable in the webview
  // (it can return undefined), so every destructive prompt routes through here.
  const [confirmState, setConfirmState] = useState(null);

  const [projects, setProjects] = useState([]);
  const [projectsLoaded, setProjectsLoaded] = useState(false);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [sessionFilter, setSessionFilter] = useState('');

  // The primary center view. 'workbench' is Home or a project workspace;
  // 'schedules' and 'issues' are first-class surfaces reached from the sidebar.
  const [view, setView] = useState('workbench');
  // A message an Issue hands to the next new chat in one project, filled into
  // its composer once. It lives here rather than on the tab so nothing about
  // the Issue is saved with the layout or outlives the hand-off.
  const [chatDraft, setChatDraft] = useState(null);
  const [leftCollapsed, setLeftCollapsed] = useState(() => readStored(LS_SIDEBAR_COLLAPSED, false) === true);
  const [workspace, setWorkspace] = useState(emptyWorkspace);
  // The pane currently under a tab being dragged, highlighted as the drop target.
  const [dropPane, setDropPane] = useState(null);
  // In a grid, one pane can be temporarily maximized to fill the workspace for
  // focused work; null means show the whole grid. It is a view overlay, not a
  // layout change — the grid is restored intact when it clears.
  const [maximizedPaneId, setMaximizedPaneId] = useState(null);
  const [explorerMode, setExplorerMode] = useState('files'); // 'files' | 'changes'
  const [sidebarWidth, setSidebarWidth] = useState(() =>
    clampSidebarWidth(readStored(LS_SIDEBAR_WIDTH, SIDEBAR_DEFAULT_WIDTH)),
  );

  useEffect(() => { writeStored(LS_SIDEBAR_COLLAPSED, leftCollapsed); }, [leftCollapsed]);
  useEffect(() => { writeStored(LS_SIDEBAR_WIDTH, sidebarWidth); }, [sidebarWidth]);

  const startSidebarResize = useCallback((e) => {
    e.preventDefault();
    const onMove = (ev) => setSidebarWidth(clampSidebarWidth(ev.clientX));
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      document.body.style.userSelect = '';
    };
    document.body.style.userSelect = 'none';
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, []);

  // The project for the next new chat (set when user clicks + on a project,
  // cleared once a session is created). For existing sessions the project is
  // derived from session.workspace.
  const [newChatProject, setNewChatProject] = useState(null);

  // Group sessions by the project they belong to. Membership is recorded on the
  // session, not inferred from its folder: one project can span a repository's
  // worktrees, so matching directories both missed sessions and claimed others.
  const sessionsByProject = useMemo(() => {
    const map = {};
    const q = sessionFilter.trim().toLowerCase();
    for (const proj of projects) {
      map[proj.id] = sessions
        .filter((s) => s.project_id === proj.id)
        .filter((s) => !q ||
          (s.title ?? '').toLowerCase().includes(q) ||
          (s.id ?? '').toLowerCase().includes(q))
        .sort((a, b) => {
          if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1;
          return (b.created_at || '').localeCompare(a.created_at || '');
        });
    }
    return map;
  }, [projects, sessions, sessionFilter]);

  // The project currently in use: derived from the open session, or the pending
  // new-chat project when no session is selected yet.
  const currentProject = useMemo(() => {
    if (!selectedId) return newChatProject ?? null;
    const sess = sessions.find((s) => s.id === selectedId);
    if (!sess) return null;
    return projects.find((p) => p.id === sess.project_id) ?? null;
  }, [selectedId, newChatProject, sessions, projects]);

  const projectById = useMemo(() => {
    const map = new Map();
    for (const p of projects) map.set(p.id, p);
    return map;
  }, [projects]);

  const recentSessions = useMemo(() => {
    return [...sessions]
      .sort((a, b) => {
        if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1;
        return compareRecent(a.created_at, b.created_at);
      })
      .slice(0, 6);
  }, [sessions]);

  const recentProjects = useMemo(() => {
    return [...projects]
      .sort((a, b) => compareRecent(a.last_used_at || a.created_at, b.last_used_at || b.created_at))
      .slice(0, 6);
  }, [projects]);

  // Tool approvals pending per session (see lib/approvals). They are held here,
  // not in ChatSession, so a prompt raised while its chat tab is not on screen is
  // still waiting when the tab is shown. A run that ends withdraws its prompt.
  const [approvals, setApprovals] = useState({});

  useEffect(() => {
    const unsubs = [
      EventsOn('desktop/approval-request', (payload) => setApprovals((prev) => withApproval(prev, payload))),
      EventsOn('desktop/stream-done', (p) => setApprovals((prev) => withoutApproval(prev, p?.session_id))),
      EventsOn('desktop/stream-error', (p) => setApprovals((prev) => withoutApproval(prev, p?.session_id))),
    ];
    return () => unsubs.forEach((unsub) => unsub?.());
  }, []);

  // The Agent's browser opens as its own visible window; this tracks which page
  // each session is on so the status bar can show a compact indicator. Keyed by
  // session so concurrent chats do not clobber each other; cleared on close.
  const [browserPages, setBrowserPages] = useState({});
  useEffect(() => {
    const unsub = EventsOn('desktop/browser/state', (p) => {
      if (!p?.session_id) return;
      setBrowserPages((prev) => {
        const next = { ...prev };
        if (p.closed) delete next[p.session_id];
        else next[p.session_id] = { url: p.url || '', title: p.title || '' };
        return next;
      });
    });
    return () => unsub?.();
  }, []);
  const browserList = Object.entries(browserPages).map(([id, v]) => ({ id, ...v }));

  useEffect(() => {
    if (getApp()) { setWailsReady(true); return; }
    const id = setTimeout(() => setWailsReady(true), 150);
    return () => clearTimeout(id);
  }, []);

  const app = getApp();

  const refreshAuthStatus = useCallback(() => {
    if (!app) return;
    app.GetAuthStatus()
      .then((status) => setAuthStatus(status))
      .catch(() => setAuthStatus({ logged_in: false }));
  }, [app]);

  useEffect(() => {
    if (!wailsReady || !app) return;
    refreshAuthStatus();
  }, [wailsReady, app, refreshAuthStatus]);

  // An unreachable deployment is expected to come back, so the app keeps asking
  // and clears the banner on its own once it answers.
  const deploymentUnavailable = !!authStatus?.unavailable;
  useEffect(() => {
    if (!deploymentUnavailable) return undefined;
    const timer = setInterval(refreshAuthStatus, 15000);
    return () => clearInterval(timer);
  }, [deploymentUnavailable, refreshAuthStatus]);

  // Center workspace tabs: each chat tab is one session, terminals/file/diff are
  // peer tabs. A chat tab carries a `sessionId` ('' for a not-yet-sent new chat);
  // its `ref` is the session id, or `new-N` for a new chat that keeps its identity
  // across adoption so the ChatSession is not remounted.
  const termSeqRef = useRef(0);
  const newChatSeqRef = useRef(0);
  // Which project the current `workspace` belongs to, so the save effect writes
  // it under the right key even across the switch that swaps it out.
  const workspaceProjectRef = useRef(null);
  // Each project's live in-session layout, stashed on switch so returning to a
  // project restores its exact tabs — terminals included — without killing the
  // shells. Keyed by project id; the active project's layout lives in `workspace`.
  const stashedWorkspacesRef = useRef(new Map());
  // Terminals of *inactive* projects: { id, projectId, restoreKey }. TerminalHost
  // keeps them mounted but parked, so a shell's emulator and scrollback survive a
  // project switch and reappear intact on return; the project id and restore key
  // let a parked terminal keep persisting its snapshot under its own project.
  const [parkedTerminals, setParkedTerminals] = useState([]);
  // A mirror of the latest `workspace`, so the project-switch effect can read the
  // outgoing project's current layout without a stale closure and without a
  // setState-inside-updater (which React would not reliably apply).
  const workspaceRef = useRef(workspace);

  // A shell that ends on its own — the user typed `exit` — closes its tab, in the
  // active project or a parked one. An exit Desktop requested (a tab close,
  // project delete, or quit) is ignored: its tab is already gone, or must stay in
  // the saved layout so the terminal is restored on the next launch.
  useEffect(() => {
    const unsub = EventsOn('desktop/terminal/exit', (p) => {
      if (!p?.id || p.requested) return;
      setWorkspace((ws) => closeTerminalTab(ws, p.id));
      let parked = false;
      for (const [pid, ws] of stashedWorkspacesRef.current) {
        const next = closeTerminalTab(ws, p.id);
        if (next !== ws) { stashedWorkspacesRef.current.set(pid, next); parked = true; }
      }
      if (parked) setParkedTerminals((prev) => prev.filter((t) => t.id !== p.id));
    });
    return () => unsub?.();
  }, []);

  // openChatTabInto focuses an existing chat tab for a session, else opens one.
  const openChatTabInto = (ws, sessionId, title) => {
    for (const row of ws.rows) {
      for (const pane of row.panes) {
        const t = pane.tabs.find((x) => x.kind === 'chat' && (x.sessionId ?? '') === sessionId);
        if (t) return focusPaneTab(ws, pane.id, t.key);
      }
    }
    return openInFocused(ws, { kind: 'chat', ref: sessionId, sessionId, title: title || 'Chat' });
  };
  // openNewChatInto keeps at most one not-yet-sent new chat per project (new chats
  // serialize on the same run key), focusing it if present.
  const openNewChatInto = (ws) => {
    for (const row of ws.rows) {
      for (const pane of row.panes) {
        const t = pane.tabs.find((x) => x.kind === 'chat' && (x.sessionId ?? '') === '');
        if (t) return focusPaneTab(ws, pane.id, t.key);
      }
    }
    newChatSeqRef.current += 1;
    return openInFocused(ws, { kind: 'chat', ref: `new-${newChatSeqRef.current}`, sessionId: '', title: 'New Chat' });
  };
  // updateTabField patches one tab (matched by key) across the grid.
  const updateTabField = (ws, key, patch) => ({
    ...ws,
    rows: ws.rows.map((row) => ({
      ...row,
      panes: row.panes.map((p) => ({
        ...p,
        tabs: p.tabs.map((t) => (t.key === key ? { ...t, ...patch } : t)),
      })),
    })),
  });

  // Keep the workspace mirror current for the switch effect below.
  useEffect(() => { workspaceRef.current = workspace; }, [workspace]);

  // respawnTerminalTabs reopens a restored layout's terminals as fresh shells:
  // their old PTYs died with the previous process, so each terminal tab is rebound
  // to a newly opened shell in the project workspace, seeded with the tab's saved
  // scrollback snapshot. A terminal that cannot be reopened is dropped, and any
  // pane or row left empty is removed.
  const respawnTerminalTabs = async (ws, projectId) => {
    const a = getApp();
    const rows = [];
    let count = 0;
    const keptKeys = [];
    for (const row of ws.rows) {
      const panes = [];
      for (const p of row.panes) {
        const tabs = [];
        for (const t of p.tabs) {
          if (t.kind !== 'terminal') { tabs.push(t); continue; }
          if (!a?.TerminalOpen) continue;
          try {
            const id = await a.TerminalOpen(projectId);
            count += 1;
            const restoreKey = t.restoreKey || newRestoreKey();
            let restoreContent = '';
            if (t.restoreKey && a.LoadTerminalSnapshot) {
              try { restoreContent = (await a.LoadTerminalSnapshot(projectId, restoreKey)) || ''; } catch { /* no snapshot */ }
            }
            keptKeys.push(restoreKey);
            tabs.push({ ...t, ref: id, key: `terminal:${id}`, restoreKey, restoreContent });
          } catch { /* a shell that will not open is left out */ }
        }
        if (tabs.length === 0) continue;
        const activeKey = tabs.some((t) => t.key === p.activeKey) ? p.activeKey : tabs[tabs.length - 1].key;
        panes.push({ ...p, tabs, activeKey });
      }
      if (panes.length) rows.push({ ...row, panes });
    }
    // Drop snapshots for terminals that are no longer in the layout (closed
    // before the last quit), so their contents do not linger.
    if (a?.PruneTerminalSnapshots) { try { await a.PruneTerminalSnapshots(projectId, keptKeys); } catch { /* best effort */ } }
    // Continue terminal numbering past what was restored so a new terminal does
    // not reuse a restored one's default name.
    if (count > termSeqRef.current) termSeqRef.current = count;
    if (rows.length === 0) return emptyWorkspace;
    const ids = rows.flatMap((r) => r.panes).map((p) => p.id);
    const focused = ids.includes(ws.focused) ? ws.focused : ids[ids.length - 1];
    return { rows, focused, seq: ws.seq };
  };

  // On a project switch, stash the outgoing project's live layout (so returning
  // restores it, terminals and all — the shells are not killed, only parked),
  // restore the incoming project's stashed or saved layout, and make sure the
  // selected session — or a new chat — has a focused tab. A layout restored from
  // storage (a fresh app launch) reopens its terminals as new shells first, so no
  // tab binds to a dead PTY; the stash path keeps the live shells untouched.
  useEffect(() => {
    const prevPid = workspaceProjectRef.current;
    if (prevPid) stashedWorkspacesRef.current.set(prevPid, workspaceRef.current);
    const pid = currentProject?.id ?? null;
    workspaceProjectRef.current = pid;

    const withChat = (ws) => {
      if (!currentProject) return ws;
      if (selectedId) {
        const title = sessions.find((s) => s.id === selectedId)?.title?.trim() || 'Chat';
        return openChatTabInto(ws, selectedId, title);
      }
      if (!allTabs(ws).some((t) => t.kind === 'chat')) return openNewChatInto(ws);
      return ws;
    };
    // Every stashed (inactive) project's terminals stay mounted but parked.
    const computeParked = () => {
      const parked = [];
      for (const [p, w] of stashedWorkspacesRef.current) {
        if (p === pid) continue;
        for (const t of allTabs(w)) if (t.kind === 'terminal') parked.push({ id: t.ref, projectId: p, restoreKey: t.restoreKey ?? '' });
      }
      return parked;
    };
    const apply = (ws) => {
      workspaceRef.current = ws;
      setWorkspace(ws);
      setParkedTerminals(computeParked());
    };

    if (!currentProject) { apply(emptyWorkspace); return; }

    if (stashedWorkspacesRef.current.has(pid)) {
      const ws = stashedWorkspacesRef.current.get(pid);
      stashedWorkspacesRef.current.delete(pid);
      apply(withChat(ws));
      return;
    }

    const saved = readStored(workspaceStorageKey(pid), null);
    const savedWs = isWorkspace(saved) ? saved : emptyWorkspace;
    if (!allTabs(savedWs).some((t) => t.kind === 'terminal')) {
      apply(withChat(savedWs));
      return;
    }
    // Reopen persisted terminals as fresh shells, then apply — but only if this
    // project is still the active one once the async opens finish.
    respawnTerminalTabs(savedWs, pid).then((ws) => {
      if (workspaceProjectRef.current === pid) apply(withChat(ws));
    });
    // Reseed only when the active project changes; selectedId is read fresh above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentProject?.id]);

  // Persist the current project's layout (terminal PTY ids stripped) whenever it
  // changes, keyed by the project the workspace belongs to.
  useEffect(() => {
    const pid = workspaceProjectRef.current;
    if (!pid) return;
    writeStored(workspaceStorageKey(pid), pruneForPersist(workspace));
  }, [workspace]);

  // A maximized pane is only meaningful in a grid: drop it when the pane is gone
  // or the grid collapsed back to one pane, so a stale id never hides the layout.
  useEffect(() => {
    if (!maximizedPaneId) return;
    const panes = workspace.rows.flatMap((r) => r.panes);
    if (panes.length <= 1 || !panes.some((p) => p.id === maximizedPaneId)) {
      setMaximizedPaneId(null);
    }
  }, [workspace, maximizedPaneId]);
  const toggleMaximizePane = useCallback((paneId) => {
    setMaximizedPaneId((cur) => (cur === paneId ? null : paneId));
    setWorkspace((s) => focusPane(s, paneId));
  }, []);

  // A new chat adopts its real session id from the first event of the run it
  // launched: record it on the tab (its key stays stable so the ChatSession is
  // not remounted) and refresh the sidebar.
  const handleSessionAdopted = useCallback((tab, realId) => {
    setWorkspace((s) => updateTabField(s, tab.key, { sessionId: realId }));
    getApp()?.ListSessions().then((list) => setSessions(list ?? [])).catch(() => {});
  }, []);
  const handleTabTitle = useCallback((tab, title) => {
    setWorkspace((s) => updateTabField(s, tab.key, { title }));
  }, []);
  const refreshSessions = useCallback(() => {
    getApp()?.ListSessions().then((list) => setSessions(list ?? [])).catch(() => {});
  }, []);
  const openSessionTab = useCallback((sessionId) => {
    setSelectedId(sessionId);
    setWorkspace((s) => openChatTabInto(s, sessionId, 'Chat'));
  }, []);

  const openTerminalTab = useCallback(async () => {
    const a = getApp();
    if (!a?.TerminalOpen || !currentProject) return;
    try {
      const id = await a.TerminalOpen(currentProject.id);
      termSeqRef.current += 1;
      setWorkspace((s) => openInFocused(s, { kind: 'terminal', ref: id, restoreKey: newRestoreKey(), title: `Terminal ${termSeqRef.current}` }));
    } catch {
      // Opening a shell can fail (e.g. unsupported platform); leave the tabs.
    }
  }, [currentProject]);

  const openBrowserTab = useCallback((sessionId) => {
    setWorkspace((s) => openInFocused(s, { kind: 'browser', ref: sessionId, title: 'Browser' }));
  }, []);

  const selectCenterTab = useCallback((paneId, key) => setWorkspace((s) => focusPaneTab(s, paneId, key)), []);
  const focusCenterPane = useCallback((paneId) => setWorkspace((s) => focusPane(s, paneId)), []);
  const splitCenterRight = useCallback((paneId) => setWorkspace((s) => splitRight(focusPane(s, paneId))), []);
  const splitCenterDown = useCallback((paneId) => setWorkspace((s) => splitDown(focusPane(s, paneId))), []);
  // One-click switch between a single tabbed pane and a grid of panes: tile every
  // tab into its own pane, or collapse them all back into one, so a full grid
  // never has to be assembled or torn down tab by tab.
  const toggleGrid = useCallback(() => setWorkspace((s) => {
    const panes = s.rows.reduce((n, r) => n + r.panes.length, 0);
    return panes > 1 ? collapse(s) : tile(s);
  }), []);
  // A tab dragged from one pane's strip and dropped on another pane. Held in
  // state (set once on drag start) so drop handlers read it without a ref.
  const [dragTab, setDragTab] = useState(null);
  const moveCenterTab = useCallback((fromPane, key, toPane, beforeKey = null) => {
    setWorkspace((s) => moveTab(s, fromPane, key, toPane, beforeKey));
  }, []);

  // Terminals live in TerminalHost, portalled into the slot of the pane that
  // shows them (see TerminalHost). Each pane whose active tab is a terminal
  // registers its slot element here by pane id (read from data-pane on attach);
  // the slot map is state so the target computation reads it during render.
  const [termSlots, setTermSlots] = useState(() => new Map());
  const setSlotEl = useCallback((paneId, el) => {
    setTermSlots((prev) => {
      if (el) {
        if (prev.get(paneId) === el) return prev;
        const next = new Map(prev);
        next.set(paneId, el);
        return next;
      }
      if (!prev.has(paneId)) return prev;
      const next = new Map(prev);
      next.delete(paneId);
      return next;
    });
  }, []);
  // A stable ref callback (React 19 cleanup form) so it is not re-attached each
  // render and never reads a ref during render.
  const slotRef = useCallback((el) => {
    if (!el) return undefined;
    const paneId = el.dataset.pane;
    setSlotEl(paneId, el);
    return () => setSlotEl(paneId, null);
  }, [setSlotEl]);
  const [termParkEl, setTermParkEl] = useState(null);
  const setTermPark = useCallback((el) => setTermParkEl(el), []);

  // Every open terminal and where it should be portalled: into its pane's slot
  // when it is that pane's active tab, otherwise parked (mounted but hidden).
  const terminalTargets = useMemo(() => {
    const out = [];
    const seen = new Set();
    for (const row of workspace.rows) {
      for (const pane of row.panes) {
        for (const t of pane.tabs) {
          if (t.kind !== 'terminal') continue;
          const isActive = pane.activeKey === t.key;
          out.push({ id: t.ref, active: isActive, target: isActive ? (termSlots.get(pane.id) ?? null) : null });
          seen.add(t.ref);
        }
      }
    }
    // Inactive projects' terminals: mounted but parked, so scrollback survives a
    // switch. (A duplicate id could only appear mid-switch; skip it.)
    for (const { id } of parkedTerminals) {
      if (!seen.has(id)) out.push({ id, active: false, target: null });
    }
    return out;
  }, [workspace, termSlots, parkedTerminals]);

  // Snapshot identity per terminal id, for TerminalHost to hand each TerminalPane:
  // the current project's terminals carry its id, their restore key, and any
  // restored contents; parked terminals carry their own project's id and key so
  // they keep saving under it. restoreContent is only meaningful right after a
  // restore and is written once at mount.
  const terminalMetaById = useMemo(() => {
    const m = new Map();
    for (const row of workspace.rows) {
      for (const pane of row.panes) {
        for (const t of pane.tabs) {
          if (t.kind !== 'terminal') continue;
          m.set(t.ref, { projectId: currentProject?.id ?? '', restoreKey: t.restoreKey ?? '', restoreContent: t.restoreContent ?? '' });
        }
      }
    }
    for (const p of parkedTerminals) {
      if (!m.has(p.id)) m.set(p.id, { projectId: p.projectId, restoreKey: p.restoreKey, restoreContent: '' });
    }
    return m;
  }, [workspace, parkedTerminals, currentProject?.id]);

  // Each terminal keeps ONE host element for its whole life. TerminalPane portals
  // into it and never leaves: React remounts a portal's child when its container
  // changes (which would dispose xterm and lose scrollback), so instead the host
  // stays put and we move the host element itself between a pane's slot and the
  // hidden park with appendChild — the portal container is unchanged, so xterm is
  // never recreated across tab switches, drags, re-tiles, or project switches.
  const [termHosts, setTermHosts] = useState(() => new Map());
  const termIdsKey = terminalTargets.map((t) => t.id).join('\n');
  useEffect(() => {
    setTermHosts((prev) => {
      const ids = new Set(terminalTargets.map((t) => t.id));
      const next = new Map(prev);
      let changed = false;
      for (const id of ids) {
        if (!next.has(id)) {
          const el = document.createElement('div');
          el.className = 'terminal-host';
          next.set(id, el);
          changed = true;
        }
      }
      for (const id of [...next.keys()]) {
        if (!ids.has(id)) {
          next.get(id)?.remove();
          next.delete(id);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [termIdsKey]);

  // Place each terminal's host in its slot (visible) or the park (hidden). Moving
  // the host never changes the portal container, so no terminal is remounted.
  useLayoutEffect(() => {
    for (const { id, target } of terminalTargets) {
      const host = termHosts.get(id);
      if (!host) continue;
      const dest = target ?? termParkEl;
      if (dest && host.parentNode !== dest) dest.appendChild(host);
    }
  }, [terminalTargets, termHosts, termParkEl]);

  const terminalActiveById = useMemo(() => {
    const m = {};
    for (const { id, active } of terminalTargets) m[id] = active;
    return m;
  }, [terminalTargets]);
  const closeCenterTab = useCallback((paneId, key) => {
    setWorkspace((s) => {
      const tab = allTabs(s).find((t) => t.key === key);
      if (tab?.kind === 'terminal') getApp()?.TerminalClose?.(tab.ref);
      return closePaneTab(s, paneId, key);
    });
  }, []);
  // "Close others" / "Close tabs to the right" from a tab's context menu. Both
  // kill the PTYs of any terminal tabs they remove first (the pure pane ops only
  // drop them from state), then apply the same closable-respecting rule the pure
  // ops use, so a non-closable chat tab is never swept away.
  const closeCenterOthers = useCallback((paneId, key) => {
    setWorkspace((s) => {
      const pane = s.rows.flatMap((r) => r.panes).find((p) => p.id === paneId);
      pane?.tabs.forEach((t) => {
        if (t.key !== key && t.closable !== false && t.kind === 'terminal') getApp()?.TerminalClose?.(t.ref);
      });
      return closeOtherPaneTabs(s, paneId, key);
    });
  }, []);
  const closeCenterRight = useCallback((paneId, key) => {
    setWorkspace((s) => {
      const pane = s.rows.flatMap((r) => r.panes).find((p) => p.id === paneId);
      const idx = pane ? pane.tabs.findIndex((t) => t.key === key) : -1;
      if (idx !== -1) {
        pane.tabs.slice(idx + 1).forEach((t) => {
          if (t.closable !== false && t.kind === 'terminal') getApp()?.TerminalClose?.(t.ref);
        });
      }
      return closeRightPaneTabs(s, paneId, key);
    });
  }, []);

  // The Explorer opens file and diff content as tabs in the focused pane. A
  // single browse click opens a preview tab, which the next browse click
  // replaces; a double-click pins a durable tab (see tabs.js / panes.js).
  const openFileTab = useCallback((path, pinned = false) => {
    const tab = { kind: 'file', ref: path, title: path.split('/').pop() || path, preview: !pinned };
    setWorkspace((s) => {
      const opened = openInFocused(s, tab);
      return pinned ? pinPaneTab(opened, opened.focused, tabIdentity(tab)) : opened;
    });
  }, []);
  const openDiffTab = useCallback((path, pinned = false) => {
    const tab = { kind: 'diff', ref: path, title: `${path.split('/').pop() || path} (diff)`, preview: !pinned };
    setWorkspace((s) => {
      const opened = openInFocused(s, tab);
      return pinned ? pinPaneTab(opened, opened.focused, tabIdentity(tab)) : opened;
    });
  }, []);
  // A commit's diff is a diff tab carrying the commit; its ref adds the sha so
  // the same file in two commits (or uncommitted) opens as separate tabs.
  const openCommitDiffTab = useCallback((sha, path, pinned = false) => {
    const tab = {
      kind: 'diff',
      ref: `${path}@${sha}`,
      path,
      commit: sha,
      title: `${path.split('/').pop() || path} (${sha.slice(0, 7)})`,
      preview: !pinned,
    };
    setWorkspace((s) => {
      const opened = openInFocused(s, tab);
      return pinned ? pinPaneTab(opened, opened.focused, tabIdentity(tab)) : opened;
    });
  }, []);
  const pinCenterTab = useCallback((paneId, key) => setWorkspace((s) => pinPaneTab(s, paneId, key)), []);

  // The login is the mode. Without one the agent runs here against the models in
  // settings.yaml, which needs no server and therefore no sign-in first — so the
  // workbench opens as soon as the status is known, either way.
  const localMode = !authStatus?.logged_in;
  // Issues need a login the server still honours; an unreachable server keeps
  // the view, which then says it cannot load.
  const serverMode = !!authStatus?.logged_in && !authStatus?.expired;
  useEffect(() => {
    if (!serverMode && view === 'issues') setView('workbench');
  }, [serverMode, view]);
  const workbenchReady = !!authStatus;

  useEffect(() => {
    if (!wailsReady || !app || !workbenchReady) return;
    Promise.all([app.ListProjects(), app.ListSessions()])
      .then(([list, sessionList]) => {
        setProjects(list ?? []);
        setSessions(sessionList ?? []);
        setProjectsLoaded(true);
      })
      .catch(() => setProjectsLoaded(true));
  }, [wailsReady, app, workbenchReady]);

  // Signing out returns the app to local mode, which is a working state rather
  // than a locked door: the workbench stays open on settings.yaml's models.
  function handleLogout() {
    if (!app) return;
    const signedOut = { logged_in: false };
    app.Logout()
      .then(() => setAuthStatus(signedOut))
      .catch(() => setAuthStatus(signedOut));
  }

  // Selecting a session makes it the primary selection and opens (or focuses) its
  // chat tab. Across a project switch the tab is opened by the reseed effect,
  // which reads the fresh selectedId.
  function handleSelectSession(sessionId) {
    setView('workbench');
    setNewChatProject(null);
    const sess = sessions.find((s) => s.id === sessionId);
    setSelectedId(sessionId);
    if (sess && sess.project_id === currentProject?.id) {
      setWorkspace((s) => openChatTabInto(s, sessionId, sess.title?.trim() || 'Chat'));
    }
  }

  // Home is the no-project workbench: clearing the selection is what takes the
  // center back to the dashboard.
  function handleGoHome() {
    setView('workbench');
    setNewChatProject(null);
    setSelectedId(null);
  }

  function handleStartIssueChat(project, text) {
    setChatDraft({ projectId: project.id, text, seq: Date.now() });
    handleNewChatInProject(project);
  }

  function handleNewChatInProject(project) {
    setView('workbench');
    setProjectNotices([]);
    app?.ProjectNotices?.(project.id)
      .then((lines) => setProjectNotices(lines ?? []))
      .catch(() => {});
    const sameProject = project.id === currentProject?.id;
    setNewChatProject(project);
    setSelectedId(null);
    if (sameProject) setWorkspace((s) => openNewChatInto(s));
  }

  async function handleOpenProjectFolder(name, folderPath) {
    try {
      // A folder already known -- a worktree of a repository in the list, or
      // the same directory under another spelling -- resolves to the project
      // that owns it rather than adding a duplicate beside it.
      const project = await app.OpenProject(folderPath, name);
      setProjects((prev) => {
        const without = prev.filter((p) => p.id !== project.id);
        return [...without, project];
      });
      handleNewChatInProject(project);
      setShowCreateModal(false);
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  async function handleRenameProject(id, newName) {
    try {
      await app.RenameProject(id, newName);
      setProjects((prev) => prev.map((p) => p.id === id ? { ...p, name: newName } : p));
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  // afterProjectDeleted runs the local cleanup once the backend has removed a
  // project: drop it from the list and kill its shells (active or stashed), and
  // clear the workspace if it was the one in use.
  function afterProjectDeleted(id) {
    setProjects((prev) => prev.filter((p) => p.id !== id));
    const stashed = stashedWorkspacesRef.current.get(id);
    if (stashed) {
      allTabs(stashed).forEach((t) => { if (t.kind === 'terminal') getApp()?.TerminalClose?.(t.ref); });
      stashedWorkspacesRef.current.delete(id);
    }
    if (currentProject?.id === id) {
      allTabs(workspace).forEach((t) => { if (t.kind === 'terminal') getApp()?.TerminalClose?.(t.ref); });
      setNewChatProject(null);
      setSelectedId(null);
    }
  }

  async function handleDeleteProject(id) {
    // A project and its sessions are separate things to destroy. The first
    // attempt keeps the sessions; if the project still owns some, the backend
    // refuses and says how many, and only then is deleting them offered.
    try {
      await app.DeleteProject(id, false);
      afterProjectDeleted(id);
    } catch {
      const held = (sessions ?? []).filter((s) => s.project_id === id).length;
      setConfirmState({
        title: 'Delete project',
        confirmLabel: 'Delete project',
        message: `This project still has ${held || 'some'} session(s). Delete the project and its sessions? Files in the project folder are not touched.`,
        onConfirm: async () => {
          try {
            await app.DeleteProject(id, true);
            setSessions((prev) => prev.filter((s) => s.project_id !== id));
            afterProjectDeleted(id);
          } catch (err) {
            setError(err?.message ?? String(err));
          } finally {
            setConfirmState(null);
          }
        },
      });
    }
  }

  // closeSessionTabs removes any chat tab bound to a session (used when it is
  // deleted), keeping the project's workspace otherwise intact.
  const closeSessionTabs = useCallback((sessionIds) => {
    const drop = new Set(sessionIds);
    setWorkspace((s) => {
      const matches = [];
      s.rows.forEach((r) => r.panes.forEach((p) => p.tabs.forEach((t) => {
        if (t.kind === 'chat' && drop.has(t.sessionId ?? '')) matches.push([p.id, t.key]);
      })));
      let ws = matches.reduce((acc, [pid, key]) => closePaneTab(acc, pid, key), s);
      if (!allTabs(ws).some((t) => t.kind === 'chat')) ws = openNewChatInto(ws);
      return ws;
    });
  }, []);

  async function handleRenameSession(id, title) {
    try {
      await app.RenameSession(id, title);
      setSessions((prev) => prev.map((s) => s.id === id ? { ...s, title } : s));
      setWorkspace((s) => ({
        ...s,
        rows: s.rows.map((row) => ({
          ...row,
          panes: row.panes.map((p) => ({
            ...p,
            tabs: p.tabs.map((t) => (t.kind === 'chat' && (t.sessionId ?? '') === id ? { ...t, title: title || 'Chat' } : t)),
          })),
        })),
      }));
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  async function handleDeleteSession(id) {
    try {
      await app.DeleteSession(id);
      setSessions((prev) => prev.filter((s) => s.id !== id));
      closeSessionTabs([id]);
      if (selectedId === id) {
        setSelectedId(null);
        if (currentProject) setNewChatProject(currentProject);
      }
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  async function handlePinSession(id, pinned) {
    try {
      await app.SetSessionPinned(id, pinned);
      setSessions((prev) => prev.map((s) => s.id === id ? { ...s, pinned } : s));
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  function handleClearProjectSessions(project, projectSessions = []) {
    setConfirmState({
      title: 'Clear all sessions',
      confirmLabel: 'Clear sessions',
      message: `Clear all sessions for ${project.name}? This does not delete files in the project folder.`,
      onConfirm: async () => {
        try {
          const visibleIds = projectSessions.map((s) => s.id);
          let deleted = [];
          if (typeof app.ClearProjectSessions === 'function') {
            deleted = await app.ClearProjectSessions(project.id);
          }
          if ((deleted ?? []).length === 0 && visibleIds.length > 0) {
            await Promise.all(visibleIds.map((id) => app.DeleteSession(id)));
            deleted = visibleIds;
          }
          const deletedSet = new Set(deleted ?? visibleIds);
          setSessions((prev) => prev.filter((s) => !deletedSet.has(s.id)));
          closeSessionTabs([...deletedSet]);
          if (selectedId && deletedSet.has(selectedId)) {
            setSelectedId(null);
            if (currentProject?.id === project.id) setNewChatProject(project);
          }
        } catch (err) {
          setError(err?.message ?? String(err));
        } finally {
          setConfirmState(null);
        }
      },
    });
  }

  // Answer one session's prompt by its approval id. A rejection means the run
  // already stopped waiting (it ended or was cancelled); there is nothing to redo.
  const handleRespond = useCallback(async (request, decision) => {
    if (!request || !app) return;
    setApprovals((prev) => withoutApproval(prev, request.session_id, request.approval_id));
    try {
      await app.RespondApproval(request.approval_id, decision);
    } catch (err) {
      console.error('RespondApproval failed:', err);
    }
  }, [app]);

  // --- Loading / auth screens ---

  if (!wailsReady) {
    return (
      <ThemeProvider>
        <div className="shell"><div className="shell__body" style={{ padding: '2rem' }}>
          <p className="page-chat__muted">Loading…</p>
        </div></div>
      </ThemeProvider>
    );
  }
  if (!app) {
    return (
      <ThemeProvider>
        <div className="shell"><div className="shell__body" style={{ padding: '2rem' }}>
          <p className="page-chat__muted">
            Run this app with Wails (e.g. <code>wails dev</code> or <code>./make run desktop</code>).
          </p>
        </div></div>
      </ThemeProvider>
    );
  }
  if (authStatus === null || (workbenchReady && !projectsLoaded)) {
    return (
      <ThemeProvider>
        <div className="shell"><div className="shell__body" style={{ padding: '2rem' }}>
          <p className="page-chat__muted">Loading…</p>
        </div></div>
      </ThemeProvider>
    );
  }
  // An expired login is not silently swapped for local models: that would send
  // prompts somewhere nobody chose. Signing in again or signing out are the two
  // ways on, and signing out is what returns this app to local mode.
  if (authStatus?.expired || signInOpen) {
    return (
      <ThemeProvider>
        <LoginPage
          expiredDetail={authStatus?.expired ? authStatus.expired_detail : ''}
          accountDisabled={!!authStatus?.account_disabled}
          knownServerURL={authStatus?.expired ? authStatus.server_url : ''}
          onLogin={(status) => {
            setAuthStatus(status);
            setSignInOpen(false);
          }}
          onCancel={() => {
            if (authStatus?.expired) handleLogout();
            setSignInOpen(false);
          }}
        />
      </ThemeProvider>
    );
  }

  // The focused pane's active tab drives the sidebar highlight and what the
  // Explorer resolves its workspace against — the focused chat's session, or the
  // primary selection when the focus is not a chat.
  const focusedPaneObj = workspace.rows.flatMap((r) => r.panes).find((p) => p.id === workspace.focused);
  const focusedActiveTab = focusedPaneObj ? focusedPaneObj.tabs.find((t) => t.key === focusedPaneObj.activeKey) : null;
  const focusedChatSessionId = focusedActiveTab?.kind === 'chat' ? (focusedActiveTab.sessionId ?? '') : (selectedId || '');
  const highlightSessionId = focusedActiveTab?.kind === 'chat' ? (focusedActiveTab.sessionId || null) : selectedId;

  // Copy a file/diff tab's path to the clipboard. Go resolves and copies it, so
  // the absolute form matches the file the panel reads (the session's workspace
  // root may be a worktree) and clipboard access works in the native window.
  const copyCenterPath = (key, absolute) => {
    const tab = allTabs(workspace).find((t) => t.key === key);
    if (!tab || (tab.kind !== 'file' && tab.kind !== 'diff') || !currentProject) return;
    getApp()?.CopyWorkspacePath?.(currentProject.id, focusedChatSessionId, tab.path ?? tab.ref, absolute)
      .catch(() => {});
  };

  // Rename a chat or terminal tab. A chat tab bound to a real session renames the
  // session (which persists and updates the sidebar); an unadopted chat or a
  // terminal tab is titled locally. File/diff titles are the filename and are not
  // renamable — their tooltip shows the full path instead.
  const renameCenterTab = (key, title) => {
    const next = title.trim();
    if (!next) return;
    const tab = allTabs(workspace).find((t) => t.key === key);
    if (!tab || (tab.kind !== 'chat' && tab.kind !== 'terminal')) return;
    if (tab.kind === 'chat' && tab.sessionId) {
      handleRenameSession(tab.sessionId, next);
    } else {
      setWorkspace((s) => updateTabField(s, key, { title: next }));
    }
  };

  // One pane's content: the active tab decides what shows. A chat tab renders a
  // ChatSession keyed to its own session, so several run at once; every terminal
  // stays mounted (portalled by TerminalHost) so scrollback survives.
  const renderPaneContent = (pane) => {
    const active = activeTab(pane);
    return (
      <>
        {active?.kind === 'chat' && (
          <ChatSession
            key={active.key}
            projectId={currentProject.id}
            projectName={currentProject.name}
            defaultWorkspace={currentProject.default_workspace}
            sessions={sessions}
            tab={active}
            app={app}
            approvals={approvals}
            onRespond={handleRespond}
            focused={pane.id === workspace.focused}
            onSessionAdopted={handleSessionAdopted}
            onSessionsChanged={refreshSessions}
            onTitle={handleTabTitle}
            onOpenSession={openSessionTab}
            onShowChanges={() => setExplorerMode('changes')}
            draft={!active.sessionId && chatDraft?.projectId === currentProject.id ? chatDraft : null}
            onDraftConsumed={() => setChatDraft(null)}
          />
        )}
        {active?.kind === 'file' && (
          <FileView
            projectID={currentProject.id}
            sessionID={focusedChatSessionId}
            path={active.ref}
            app={app}
          />
        )}
        {active?.kind === 'diff' && (
          <DiffView
            projectID={currentProject.id}
            sessionID={focusedChatSessionId}
            path={active.path ?? active.ref}
            commit={active.commit}
            app={app}
          />
        )}
        {active?.kind === 'terminal' && (
          <div className="terminal-slot" data-pane={pane.id} ref={slotRef} />
        )}
        {active?.kind === 'browser' && (
          <BrowserView sessionId={active.ref} />
        )}
        {!active && (
          <div className="workspace-pane__empty">Open a file, diff, or terminal here.</div>
        )}
      </>
    );
  };

  const totalPanes = workspace.rows.reduce((n, r) => n + r.panes.length, 0);
  // The grid toggle is worth showing only when there is something to rearrange:
  // more than one pane to collapse, or more than one tab to tile.
  const canToggleGrid = totalPanes > 1 || allTabs(workspace).length > 1;
  // Maximize is only offered, and only honoured, in a grid.
  const canMaximize = totalPanes > 1;
  const maximizedPane = canMaximize && maximizedPaneId
    ? workspace.rows.flatMap((r) => r.panes).find((p) => p.id === maximizedPaneId)
    : null;

  // renderPane draws one pane: its tab strip and the active tab's content. Used
  // for every pane in the grid and for the single maximized pane.
  const renderPane = (pane) => {
    const focused = pane.id === workspace.focused;
    const paneClass = [
      'workspace-pane',
      totalPanes > 1 && focused ? 'workspace-pane--focused' : '',
      dropPane === pane.id ? 'workspace-pane--drop' : '',
    ].filter(Boolean).join(' ');
    return (
      <div
        key={pane.id}
        className={paneClass}
        onMouseDownCapture={() => focusCenterPane(pane.id)}
        onDragOver={(e) => {
          if (!dragTab) return;
          e.preventDefault();
          if (dropPane !== pane.id) setDropPane(pane.id);
        }}
        onDrop={(e) => {
          e.preventDefault();
          const d = dragTab;
          setDropPane(null);
          setDragTab(null);
          if (d) moveCenterTab(d.fromPane, d.key, pane.id);
        }}
      >
        <TabBar
          tabs={pane.tabs}
          activeKey={pane.activeKey}
          onSelect={(key) => selectCenterTab(pane.id, key)}
          onClose={(key) => closeCenterTab(pane.id, key)}
          onPin={(key) => pinCenterTab(pane.id, key)}
          onRename={renameCenterTab}
          onNewTab={currentProject ? () => setWorkspace((s) => openNewChatInto(focusPane(s, pane.id))) : undefined}
          onSplitRight={maximizedPane ? undefined : () => splitCenterRight(pane.id)}
          onSplitDown={maximizedPane ? undefined : () => splitCenterDown(pane.id)}
          onToggleMaximize={canMaximize ? () => toggleMaximizePane(pane.id) : undefined}
          maximized={!!maximizedPane && maximizedPane.id === pane.id}
          onTabDragStart={(key) => setDragTab({ fromPane: pane.id, key })}
          onTabDragEnd={() => { setDragTab(null); setDropPane(null); }}
          onTabDrop={(beforeKey) => {
            const d = dragTab;
            setDropPane(null);
            setDragTab(null);
            if (d) moveCenterTab(d.fromPane, d.key, pane.id, beforeKey);
          }}
          onCloseOthers={(key) => closeCenterOthers(pane.id, key)}
          onCloseRight={(key) => closeCenterRight(pane.id, key)}
          onCopyPath={copyCenterPath}
        />
        <div className="workspace-pane__content">
          {renderPaneContent(pane)}
        </div>
      </div>
    );
  };

  const shellClass = [
    'shell',
    leftCollapsed ? 'shell--left-collapsed' : '',
  ].filter(Boolean).join(' ');

  return (
    <ThemeProvider>
      <div className={shellClass}>
        {deploymentUnavailable && (
          <div className="deployment-banner" role="alert">
            <span className="deployment-banner__text">
              Cannot reach {authStatus.server_url}. Your login still works; while you are
              signed in, prompts go only there, so this app waits for it rather than using
              local models.
            </span>
            <span className="deployment-banner__detail">{authStatus.unavailable_detail}</span>
            <button type="button" className="deployment-banner__retry" onClick={refreshAuthStatus}>
              Retry
            </button>
          </div>
        )}
        <div className="shell__body">

          <Sidebar
            width={sidebarWidth}
            view={view}
            onHome={handleGoHome}
            onSchedules={() => setView('schedules')}
            onIssues={serverMode ? () => setView('issues') : undefined}
            projects={projects}
            currentProject={currentProject}
            sessionsByProject={sessionsByProject}
            highlightSessionId={highlightSessionId}
            sessionFilter={sessionFilter}
            onSessionFilterChange={setSessionFilter}
            onCreateProject={() => setShowCreateModal(true)}
            projectActions={{
              onSelectSession: handleSelectSession,
              onNewChat: handleNewChatInProject,
              onRename: handleRenameProject,
              onDelete: handleDeleteProject,
              onClearSessions: handleClearProjectSessions,
              onRenameSession: handleRenameSession,
              onDeleteSession: handleDeleteSession,
              onPinSession: handlePinSession,
            }}
            explorer={{
              app,
              sessionID: focusedChatSessionId,
              mode: explorerMode,
              onModeChange: setExplorerMode,
              onOpenFile: openFileTab,
              onOpenDiff: openDiffTab,
              onOpenCommitDiff: openCommitDiffTab,
            }}
            account={{
              authStatus,
              localMode,
              onSignIn: () => setSignInOpen(true),
              onSignOut: handleLogout,
            }}
          />

          <div
            className="sidebar-resizer"
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar"
            onMouseDown={startSidebarResize}
          />

          <main className="shell__main">
            <div className="shell__content">
              {(error || projectNotices.length > 0) && (
                <div className="workspace-banners">
                  {projectNotices.map((line, i) => (
                    <div key={`notice-${i}`} className="workspace-banner">{line}</div>
                  ))}
                  {error && (
                    <div className="workspace-banner workspace-banner--error">
                      <span>{error}</span>
                      <button type="button" onClick={() => setError(null)} aria-label="Dismiss">✕</button>
                    </div>
                  )}
                </div>
              )}
              {view === 'schedules' ? (
                <SchedulesView app={app} />
              ) : view === 'issues' ? (
                <IssuesView
                  app={app}
                  projects={projects}
                  currentProject={currentProject}
                  onStartChat={handleStartIssueChat}
                />
              ) : !currentProject ? (
                <HomeDashboard
                  recentSessions={recentSessions}
                  recentProjects={recentProjects}
                  projectById={projectById}
                  onSelectSession={handleSelectSession}
                  onOpenProject={handleNewChatInProject}
                  onCreateProject={() => setShowCreateModal(true)}
                />
              ) : (
                <div className={`workspace-grid${totalPanes > 1 ? ' workspace-grid--split' : ''}`}>
                  {workspace.rows.map((row) => (
                    <div key={row.id} className="workspace-grid__row">
                      {row.panes.map((pane) => (
                        // The maximized pane floats in the overlay below; its grid
                        // cell holds a dimmed placeholder so the layout keeps its
                        // shape (and the pane is never rendered — or slotted —
                        // twice).
                        maximizedPane && maximizedPane.id === pane.id
                          ? <div key={pane.id} className="workspace-pane workspace-pane--placeholder" aria-hidden />
                          : renderPane(pane)
                      ))}
                    </div>
                  ))}
                  {maximizedPane && (
                    <div
                      className="workspace-maximize-overlay"
                      onClick={(e) => { if (e.target === e.currentTarget) setMaximizedPaneId(null); }}
                    >
                      <div className="workspace-maximize-overlay__card">
                        {renderPane(maximizedPane)}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
            {/* The status bar is a global first-class surface: present on Home and
                in a project alike. Launchpad and theme live here always; the
                workspace controls (new terminal, grid/tab) appear only with a
                project open. */}
            <div className="workspace-statusbar">
              <button
                type="button"
                className="workspace-statusbar__btn"
                onClick={() => setLeftCollapsed((v) => !v)}
                aria-pressed={!leftCollapsed}
                title={leftCollapsed ? 'Show sidebar' : 'Hide sidebar'}
                aria-label={leftCollapsed ? 'Show sidebar' : 'Hide sidebar'}
              >
                <span aria-hidden><SidebarIcon /></span>
              </button>
              <span className="workspace-statusbar__status">
                {view === 'schedules'
                  ? 'Schedules'
                  : view === 'issues'
                    ? 'Issues'
                    : currentProject
                    ? `${currentProject.name}${totalPanes > 1
                      ? ` · ${totalPanes} panes`
                      : (focusedActiveTab?.title ? ` · ${focusedActiveTab.title}` : '')}`
                    : 'Home'}
              </span>
              {currentProject && view === 'workbench' && (
                <button
                  type="button"
                  className="workspace-statusbar__btn"
                  onClick={openTerminalTab}
                  title="New terminal"
                  aria-label="New terminal"
                >
                  <span aria-hidden>{'>_'}</span>
                </button>
              )}
              {currentProject && view === 'workbench' && canToggleGrid && (
                <button
                  type="button"
                  className="workspace-statusbar__btn"
                  title={totalPanes > 1 ? 'Collapse panes into tabs' : 'Tile tabs into a grid'}
                  aria-label={totalPanes > 1 ? 'Collapse panes into tabs' : 'Tile tabs into a grid'}
                  onClick={toggleGrid}
                >
                  <span aria-hidden>{totalPanes > 1 ? <SplitRightIcon /> : <GridIcon />}</span>
                </button>
              )}
              {browserList.length > 0 && (
                <button
                  type="button"
                  className="workspace-statusbar__browser"
                  title={`Open the live browser view\n${browserList.map((b) => b.url).filter(Boolean).join('\n')}`}
                  onClick={() => openBrowserTab(browserList[0].id)}
                >
                  <span aria-hidden>🌐</span>{' '}
                  {browserList.length === 1
                    ? (browserList[0].title || browserList[0].url || 'Browser')
                    : `${browserList.length} browser pages`}
                </button>
              )}
              <LaunchpadButton />
              <ThemeStatusButton />
            </div>
          </main>
        </div>
      </div>

      {/* Terminals are portalled into a per-terminal host element (moved between
          slot and park with appendChild), so they stay mounted — and keep their
          scrollback — across tab switches, pane moves, grid re-tiling, and
          project switches. */}
      <div className="terminal-park" ref={setTermPark} aria-hidden />
      <TerminalHost hosts={termHosts} activeById={terminalActiveById} metaById={terminalMetaById} />

      {showCreateModal && (
        <CreateProjectModal
          app={app}
          onCreate={handleOpenProjectFolder}
          onClose={() => setShowCreateModal(false)}
        />
      )}

      {confirmState && (
        <ConfirmModal
          title={confirmState.title}
          message={confirmState.message}
          confirmLabel={confirmState.confirmLabel}
          onConfirm={confirmState.onConfirm}
          onCancel={() => setConfirmState(null)}
        />
      )}

    </ThemeProvider>
  );
}
