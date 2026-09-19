import { compareRecent } from './lib/format';
import { getApp } from './lib/app';
import { HomeDashboard } from './components/HomeDashboard';
import { CreateProjectModal } from './components/Modals';
import { ProjectItem } from './components/ProjectItem';
import { TerminalHost } from './components/TerminalHost';
import { ChatSession } from './components/ChatSession';
import { TabBar } from './components/TabBar';
import { Explorer } from './components/Explorer';
import { FileView } from './components/FileView';
import { DiffView } from './components/DiffView';
import { activeTab, tabIdentity } from './lib/tabs';
import {
  emptyWorkspace, openInFocused, focusPaneTab, focusPane, pinPaneTab, closePaneTab,
  splitRight, splitDown, moveTab, allTabs, pruneForPersist, isWorkspace,
  collapse, tile,
} from './lib/panes';

import { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import { Avatar, ThemeProvider, useTheme } from '@buildmax/gui';
import { EventsOn } from './lib/wailsRuntime';
import LoginPage from './LoginPage';

// Sidebar layout is a per-machine preference, remembered across runs. Storage
// can be unavailable (private windows, cleared data), so every access is guarded
// and falls back to the default.
const SIDEBAR_MIN_WIDTH = 180;
const SIDEBAR_MAX_WIDTH = 480;
const SIDEBAR_DEFAULT_WIDTH = 288;
const LS_SIDEBAR_COLLAPSED = 'bm.desktop.sidebarCollapsed';
const LS_SIDEBAR_WIDTH = 'bm.desktop.sidebarWidth';
// Workspace layout is remembered per project (terminals excluded — their PTYs do
// not survive a restart). Restoring reopens the chat, file, and diff tabs and the
// pane grid the user last left.
const workspaceStorageKey = (projectId) => `bm.desktop.workspace.${projectId}`;

function readStored(key, fallback) {
  try {
    const v = localStorage.getItem(key);
    return v == null ? fallback : JSON.parse(v);
  } catch {
    return fallback;
  }
}

function writeStored(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* storage may be unavailable; the preference just does not persist */
  }
}

function clampSidebarWidth(w) {
  const n = Number(w);
  if (!Number.isFinite(n)) return SIDEBAR_DEFAULT_WIDTH;
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, n));
}

function SunIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
    </svg>
  );
}

// GridIcon — tile tabs into a grid of panes.
function GridIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <rect x="3" y="3" width="8" height="8" rx="1" />
      <rect x="13" y="3" width="8" height="8" rx="1" />
      <rect x="3" y="13" width="8" height="8" rx="1" />
      <rect x="13" y="13" width="8" height="8" rx="1" />
    </svg>
  );
}

// SplitRightIcon — the split-right glyph, reused here for collapsing a grid
// back into one pane. A single frame divided left from right reads cleaner than
// a tab-strip drawing at this size.
function SplitRightIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <rect x="3" y="3" width="18" height="18" rx="1" />
      <path d="M12 3v18" />
    </svg>
  );
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

  const [projects, setProjects] = useState([]);
  const [projectsLoaded, setProjectsLoaded] = useState(false);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showAllProjects, setShowAllProjects] = useState(false);
  const [sessionFilter, setSessionFilter] = useState('');
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const userMenuRef = useRef(null);

  const [leftCollapsed, setLeftCollapsed] = useState(() => readStored(LS_SIDEBAR_COLLAPSED, false) === true);
  const [workspace, setWorkspace] = useState(emptyWorkspace);
  // The pane currently under a tab being dragged, highlighted as the drop target.
  const [dropPane, setDropPane] = useState(null);
  const [explorerMode, setExplorerMode] = useState('directory'); // 'directory' | 'changes'
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

  const PROJECT_PAGE_SIZE = 10;

  // The project for the next new chat (set when user clicks + on a project,
  // cleared once a session is created). For existing sessions the project is
  // derived from session.workspace.
  const [newChatProject, setNewChatProject] = useState(null);

  useEffect(() => {
    if (!userMenuOpen) return;
    function handleClickOutside(e) {
      if (userMenuRef.current && !userMenuRef.current.contains(e.target)) {
        setUserMenuOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [userMenuOpen]);

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

  const EV_APPROVAL_REQUEST = 'desktop/approval-request';

  // Approval prompts are project-level (the approval handler is per project, not
  // per session). Each chat tab shows the pending request while it is running; a
  // response resolves it for the project.
  const [approvalRequest, setApprovalRequest] = useState(null);

  useEffect(() => {
    const unsub = EventsOn(EV_APPROVAL_REQUEST, (payload) => setApprovalRequest(payload));
    return () => unsub?.();
  }, []);

  useEffect(() => {
    if (getApp()) { setWailsReady(true); return; }
    const id = setTimeout(() => setWailsReady(true), 150);
    return () => clearTimeout(id);
  }, []);

  const app = getApp();

  useEffect(() => {
    if (!wailsReady || !app) return;
    app.GetAuthStatus()
      .then((status) => setAuthStatus(status))
      .catch(() => setAuthStatus({ logged_in: false }));
  }, [wailsReady, app]);

  // Center workspace tabs: each chat tab is one session, terminals/file/diff are
  // peer tabs. A chat tab carries a `sessionId` ('' for a not-yet-sent new chat);
  // its `ref` is the session id, or `new-N` for a new chat that keeps its identity
  // across adoption so the ChatSession is not remounted.
  const termSeqRef = useRef(0);
  const newChatSeqRef = useRef(0);
  // Which project the current `workspace` belongs to, so the save effect writes
  // it under the right key even across the switch that swaps it out.
  const workspaceProjectRef = useRef(null);

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

  // On a project switch, reap the old project's terminals, restore that project's
  // saved layout (or a fresh workspace), and make sure the selected session — or a
  // new chat — has a focused tab.
  useEffect(() => {
    setWorkspace((prev) => {
      allTabs(prev)
        .filter((t) => t.kind === 'terminal')
        .forEach((t) => getApp()?.TerminalClose?.(t.ref));
      workspaceProjectRef.current = currentProject?.id ?? null;
      if (!currentProject) return emptyWorkspace;
      const saved = readStored(workspaceStorageKey(currentProject.id), null);
      let ws = isWorkspace(saved) ? saved : emptyWorkspace;
      if (selectedId) {
        const title = sessions.find((s) => s.id === selectedId)?.title?.trim() || 'Chat';
        ws = openChatTabInto(ws, selectedId, title);
      } else {
        ws = openNewChatInto(ws);
      }
      return ws;
    });
    // Reseed only when the active project changes; selectedId is read fresh above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentProject?.id]);

  // Persist the current project's layout (terminals excluded) whenever it
  // changes, keyed by the project the workspace belongs to.
  useEffect(() => {
    const pid = workspaceProjectRef.current;
    if (!pid) return;
    writeStored(workspaceStorageKey(pid), pruneForPersist(workspace));
  }, [workspace]);

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
      setWorkspace((s) => openInFocused(s, { kind: 'terminal', ref: id, title: `Terminal ${termSeqRef.current}` }));
    } catch {
      // Opening a shell can fail (e.g. unsupported platform); leave the tabs.
    }
  }, [currentProject]);

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
  const moveCenterTab = useCallback((fromPane, key, toPane) => {
    setWorkspace((s) => moveTab(s, fromPane, key, toPane));
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
    for (const row of workspace.rows) {
      for (const pane of row.panes) {
        for (const t of pane.tabs) {
          if (t.kind !== 'terminal') continue;
          const isActive = pane.activeKey === t.key;
          out.push({ id: t.ref, active: isActive, target: isActive ? (termSlots.get(pane.id) ?? null) : null });
        }
      }
    }
    return out;
  }, [workspace, termSlots]);
  const closeCenterTab = useCallback((paneId, key) => {
    setWorkspace((s) => {
      const tab = allTabs(s).find((t) => t.key === key);
      if (tab?.kind === 'terminal') getApp()?.TerminalClose?.(tab.ref);
      return closePaneTab(s, paneId, key);
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
  const pinCenterTab = useCallback((paneId, key) => setWorkspace((s) => pinPaneTab(s, paneId, key)), []);

  // The login is the mode. Without one the agent runs here against the models in
  // settings.yaml, which needs no server and therefore no sign-in first — so the
  // workbench opens as soon as the status is known, either way.
  const localMode = !authStatus?.logged_in;
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
    setNewChatProject(null);
    const sess = sessions.find((s) => s.id === sessionId);
    setSelectedId(sessionId);
    if (sess && sess.project_id === currentProject?.id) {
      setWorkspace((s) => openChatTabInto(s, sessionId, sess.title?.trim() || 'Chat'));
    }
  }

  function handleNewChatInProject(project) {
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

  async function handleDeleteProject(id) {
    try {
      // A project and its sessions are separate things to destroy. The first
      // attempt keeps the sessions; if the project still owns some, the backend
      // refuses and says how many, and only then is deleting them offered.
      try {
        await app.DeleteProject(id, false);
      } catch (refusal) {
        const held = (sessions ?? []).filter((s) => s.project_id === id).length;
        const ok = window.confirm(
          `This project still has ${held || 'some'} session(s). Delete the project and its sessions? Files in the project folder are not touched.`);
        if (!ok) return;
        void refusal;
        await app.DeleteProject(id, true);
        setSessions((prev) => prev.filter((s) => s.project_id !== id));
      }
      setProjects((prev) => prev.filter((p) => p.id !== id));
      // If the deleted project was in use, clear the workspace (it resets when
      // currentProject becomes null).
      if (currentProject?.id === id) {
        setNewChatProject(null);
        setSelectedId(null);
      }
    } catch (err) {
      setError(err?.message ?? String(err));
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

  async function handleClearProjectSessions(project, projectSessions = []) {
    const ok = window.confirm(`Clear all sessions for ${project.name}? This does not delete files in the project folder.`);
    if (!ok) return;
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
    }
  }

  async function handleRespond(decision) {
    if (!approvalRequest || !app) return;
    const req = approvalRequest;
    setApprovalRequest(null);
    try {
      await app.RespondApproval(req.project_id, decision);
    } catch (err) {
      console.error('RespondApproval failed:', err);
    }
  }

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
            approvalRequest={approvalRequest}
            onRespond={handleRespond}
            onSessionAdopted={handleSessionAdopted}
            onSessionsChanged={refreshSessions}
            onTitle={handleTabTitle}
            onOpenSession={openSessionTab}
            onShowChanges={() => setExplorerMode('changes')}
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
            path={active.ref}
            app={app}
          />
        )}
        {active?.kind === 'terminal' && (
          <div className="terminal-slot" data-pane={pane.id} ref={slotRef} />
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

  const shellClass = [
    'shell',
    leftCollapsed ? 'shell--left-collapsed' : '',
  ].filter(Boolean).join(' ');

  return (
    <ThemeProvider>
      <div className={shellClass}>
        <div className="shell__body">

          <aside className="sidebar" aria-label="Sidebar" style={{ width: sidebarWidth }}>
            <nav className="sidebar__nav" aria-label="Primary">
              <div className="sidebar__projects-header">
                <span className="sidebar__projects-label">Projects</span>
                <div className="sidebar__projects-header-actions">
                  <button
                    type="button"
                    className="sidebar__projects-collapse"
                    onClick={() => setLeftCollapsed(true)}
                    title="Collapse sidebar"
                    aria-label="Collapse sidebar"
                  >
                    «
                  </button>
                  <button
                    type="button"
                    className="sidebar__projects-add"
                    onClick={() => setShowCreateModal(true)}
                    title="New Project"
                    aria-label="New Project"
                  >
                    +
                  </button>
                </div>
              </div>
              <div className="sidebar__session-search">
                <input
                  type="search"
                  className="sidebar__session-search-input"
                  value={sessionFilter}
                  onChange={(e) => setSessionFilter(e.target.value)}
                  placeholder="Search sessions"
                  aria-label="Search sessions"
                />
              </div>

              {projects.length === 0 ? (
                <button
                  type="button"
                  className="sidebar__nav-item sidebar__projects-empty-btn"
                  onClick={() => setShowCreateModal(true)}
                >
                  <span className="sidebar__nav-icon" aria-hidden>+</span>
                  <span>New Project</span>
                </button>
              ) : (
                <>
                  {(showAllProjects ? projects : projects.slice(0, PROJECT_PAGE_SIZE)).map((proj) => (
                    <ProjectItem
                      key={proj.id}
                      project={proj}
                      sessions={sessionsByProject[proj.id] ?? []}
                      isActive={currentProject?.id === proj.id}
                      selectedSessionId={highlightSessionId}
                      onSelectSession={handleSelectSession}
                      onNewChat={() => handleNewChatInProject(proj)}
                      onRename={handleRenameProject}
                      onDelete={handleDeleteProject}
                      onClearSessions={(projectSessions) => handleClearProjectSessions(proj, projectSessions)}
                      onRenameSession={handleRenameSession}
                      onDeleteSession={handleDeleteSession}
                      onPinSession={handlePinSession}
                    />
                  ))}
                  {!showAllProjects && projects.length > PROJECT_PAGE_SIZE && (
                    <button
                      type="button"
                      className="sidebar__show-more"
                      onClick={() => setShowAllProjects(true)}
                    >
                      Show {projects.length - PROJECT_PAGE_SIZE} more…
                    </button>
                  )}
                </>
              )}
            </nav>

            {currentProject && (
              <Explorer
                projectID={currentProject.id}
                sessionID={focusedChatSessionId}
                app={app}
                mode={explorerMode}
                onModeChange={setExplorerMode}
                onOpenFile={openFileTab}
                onOpenDiff={openDiffTab}
              />
            )}

            <div className="sidebar__footer" ref={userMenuRef}>
              <button
                type="button"
                className="sidebar__user-trigger"
                onClick={() => setUserMenuOpen((v) => !v)}
                aria-expanded={userMenuOpen}
                aria-haspopup="menu"
                aria-label="User menu"
              >
                <Avatar
                  label={(authStatus.name?.trim() || authStatus.email || 'Local').slice(0, 1).toUpperCase()}
                  size="sm"
                />
                <span className="sidebar__user-name">
                  {localMode
                    ? 'Local mode'
                    : authStatus.name?.trim() || (authStatus.email ? authStatus.email.split('@')[0] : '')}
                </span>
              </button>
              {userMenuOpen && (
                <div className="sidebar__user-menu" role="menu">
                  <div className="sidebar__user-menu-email">
                    {localMode ? 'Models from settings.yaml' : authStatus.email}
                  </div>
                  <div className="sidebar__user-menu-divider" />
                  <button
                    type="button"
                    className="sidebar__user-menu-item"
                    role="menuitem"
                    onClick={() => {
                      setUserMenuOpen(false);
                      if (localMode) setSignInOpen(true);
                      else handleLogout();
                    }}
                  >
                    {localMode ? 'Sign in to a server' : 'Sign out'}
                  </button>
                </div>
              )}
            </div>
          </aside>

          <div
            className="sidebar-resizer"
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar"
            onMouseDown={startSidebarResize}
          />

          <main className="shell__main">
            {leftCollapsed && (
              <div className="shell__top">
                <button
                  type="button"
                  className="shell__sidebar-toggle"
                  onClick={() => setLeftCollapsed(false)}
                  title="Show sidebar"
                  aria-label="Show sidebar"
                >
                  ☰
                </button>
              </div>
            )}
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
              {!currentProject ? (
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
                      {row.panes.map((pane) => {
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
                              onSplitRight={() => splitCenterRight(pane.id)}
                              onSplitDown={() => splitCenterDown(pane.id)}
                              onTabDragStart={(key) => setDragTab({ fromPane: pane.id, key })}
                              onTabDragEnd={() => { setDragTab(null); setDropPane(null); }}
                            />
                            <div className="workspace-pane__content">
                              {renderPaneContent(pane)}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  ))}
                </div>
              )}
            </div>
            {/* The status bar is a global first-class surface: present on Home and
                in a project alike. Theme lives here always; the workspace controls
                (new terminal, grid/tab) appear only with a project open. */}
            <div className="workspace-statusbar">
              <span className="workspace-statusbar__status">
                {currentProject
                  ? `${currentProject.name}${totalPanes > 1
                    ? ` · ${totalPanes} panes`
                    : (focusedActiveTab?.title ? ` · ${focusedActiveTab.title}` : '')}`
                  : 'Home'}
              </span>
              {currentProject && (
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
              {currentProject && canToggleGrid && (
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
              <ThemeStatusButton />
            </div>
          </main>
        </div>
      </div>

      {/* Terminals are portalled into their pane's slot from here, so they stay
          mounted across tab switches, pane moves, and grid re-tiling. */}
      <div className="terminal-park" ref={setTermPark} aria-hidden />
      <TerminalHost terminals={terminalTargets} park={termParkEl} />

      {showCreateModal && (
        <CreateProjectModal
          app={app}
          onCreate={handleOpenProjectFolder}
          onClose={() => setShowCreateModal(false)}
        />
      )}

    </ThemeProvider>
  );
}
