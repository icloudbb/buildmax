import { compareRecent, formatToolArgs, shortToolArgs, toolDisplayName } from './lib/format';
import { addLiveToolCall, addLiveToolResult, appendAssistantForNextLLM, buildToolResultMap, mergeRunStatus } from './lib/messages';
import { getApp } from './lib/app';
import { ChatInput } from './components/ChatInput';
import { Inspector } from './components/Inspector';
import { HomeDashboard } from './components/HomeDashboard';
import { MarkdownMessage } from './components/MarkdownMessage';
import { CreateProjectModal } from './components/Modals';
import { ProjectItem } from './components/ProjectItem';
import { TerminalPane } from './components/TerminalPane';
import { TabBar } from './components/TabBar';
import { emptyTabs, openTab, closeTab, focusTab, activeTab } from './lib/tabs';

import { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import Markdown from 'react-markdown';
import { Avatar, ChatComposer, ChatThread, ThemeProvider, useTheme } from '@buildmax/gui';
import { EventsOn, EventsOff } from './lib/wailsRuntime';
import LoginPage from './LoginPage';

// Inspector column layout is a per-machine preference, remembered across runs.
// Storage can be unavailable (private windows, cleared data), so every access
// is guarded and falls back to the default.
const INSPECTOR_MIN_WIDTH = 260;
const INSPECTOR_MAX_WIDTH = 720;
const INSPECTOR_DEFAULT_WIDTH = 340;
const SIDEBAR_MIN_WIDTH = 180;
const SIDEBAR_MAX_WIDTH = 480;
const SIDEBAR_DEFAULT_WIDTH = 288;
const LS_INSPECTOR = 'bm.desktop.inspector';
const LS_INSPECTOR_WIDTH = 'bm.desktop.inspectorWidth';
const LS_SIDEBAR_COLLAPSED = 'bm.desktop.sidebarCollapsed';
const LS_SIDEBAR_WIDTH = 'bm.desktop.sidebarWidth';

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

function clampInspectorWidth(w) {
  const n = Number(w);
  if (!Number.isFinite(n)) return INSPECTOR_DEFAULT_WIDTH;
  return Math.min(INSPECTOR_MAX_WIDTH, Math.max(INSPECTOR_MIN_WIDTH, n));
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

// Inspector toolbar icons — line icons matching the app's SVG icon style
// (24-grid, currentColor stroke). Folder for the file tree, a page with +/- for
// the diff, a circled i for info.
function FilesIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M3 7a2 2 0 0 1 2-2h3.5l2 2H19a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />
    </svg>
  );
}

function ChangesIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <rect x="5" y="3" width="14" height="18" rx="2" />
      <path d="M12 7v4M10 9h4M10 16h4" />
    </svg>
  );
}

function InfoIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5" />
      <circle cx="12" cy="7.75" r="0.6" fill="currentColor" stroke="none" />
    </svg>
  );
}

// Theme toggle lives in the user menu, not the header: switching light/dark is a
// rare action. Rendered inside ThemeProvider, so it reads the live theme. The
// icon and label name the destination — moon to go dark, sun to go light.
function ThemeMenuItem() {
  const { theme, toggleTheme } = useTheme();
  const dark = theme === 'dark';
  return (
    <button
      type="button"
      className="sidebar__user-menu-item sidebar__user-menu-item--icon"
      role="menuitem"
      onClick={toggleTheme}
    >
      <span className="sidebar__user-menu-icon">{dark ? <SunIcon /> : <MoonIcon />}</span>
      <span>{dark ? 'Light mode' : 'Dark mode'}</span>
    </button>
  );
}

export default function App() {
  const [sessions, setSessions] = useState([]);
  const [selectedId, setSelectedId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [sessionTitle, setSessionTitle] = useState('');
  // What the last rewind or fork left behind, shown at the end of the
  // transcript. It is about the move, not about the conversation, so it is not
  // a message and is not persisted.
  const [historyNotice, setHistoryNotice] = useState(null);
  // Said once when a project is opened: a memory file that will not load, or
  // a project registered beside one whose folder has moved. Neither is an
  // error, and both are invisible if nobody says them here.
  const [projectNotices, setProjectNotices] = useState([]);
  const [loading, setLoading] = useState(false);
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

  // Right-hand inspector column: which content it shows and whether it is open,
  // widened, or expanded to fill the main area. `open`/`view` persist; a run
  // starts un-expanded. Width and the left-sidebar collapse are per-machine
  // preferences too. `expanded` is a transient review state, not remembered.
  const [inspector, setInspector] = useState(() => {
    const saved = readStored(LS_INSPECTOR, null);
    return {
      open: saved?.open === true,
      view: ['files', 'diff', 'info'].includes(saved?.view) ? saved.view : 'diff',
      expanded: false,
    };
  });
  const [inspectorWidth, setInspectorWidth] = useState(() =>
    clampInspectorWidth(readStored(LS_INSPECTOR_WIDTH, INSPECTOR_DEFAULT_WIDTH)),
  );
  const [leftCollapsed, setLeftCollapsed] = useState(() => readStored(LS_SIDEBAR_COLLAPSED, false) === true);
  const [center, setCenter] = useState(emptyTabs);
  const [sidebarWidth, setSidebarWidth] = useState(() =>
    clampSidebarWidth(readStored(LS_SIDEBAR_WIDTH, SIDEBAR_DEFAULT_WIDTH)),
  );

  useEffect(() => { writeStored(LS_INSPECTOR, { open: inspector.open, view: inspector.view }); }, [inspector.open, inspector.view]);
  useEffect(() => { writeStored(LS_INSPECTOR_WIDTH, inspectorWidth); }, [inspectorWidth]);
  useEffect(() => { writeStored(LS_SIDEBAR_COLLAPSED, leftCollapsed); }, [leftCollapsed]);
  useEffect(() => { writeStored(LS_SIDEBAR_WIDTH, sidebarWidth); }, [sidebarWidth]);

  const openInspector = useCallback((view) => {
    setInspector((s) => ({ ...s, open: true, view: view ?? s.view }));
  }, []);
  // The toolbar icons toggle: clicking the active view (when not expanded)
  // closes the column rather than reopening the same thing.
  const toggleInspectorView = useCallback((view) => {
    setInspector((s) => (
      s.open && s.view === view && !s.expanded
        ? { ...s, open: false, expanded: false }
        : { ...s, open: true, view }
    ));
  }, []);
  const closeInspector = useCallback(() => {
    setInspector((s) => ({ ...s, open: false, expanded: false }));
  }, []);
  const toggleInspectorExpand = useCallback(() => {
    setInspector((s) => ({ ...s, open: true, expanded: !s.expanded }));
  }, []);

  const startInspectorResize = useCallback((e) => {
    e.preventDefault();
    const onMove = (ev) => setInspectorWidth(clampInspectorWidth(window.innerWidth - ev.clientX));
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      document.body.style.userSelect = '';
    };
    document.body.style.userSelect = 'none';
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, []);

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

  const historyRef = useRef(null);
  const streamingContentRef = useRef('');

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

  const currentSession = useMemo(() => (
    selectedId ? sessions.find((s) => s.id === selectedId) ?? null : null
  ), [selectedId, sessions]);

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

  const EV_STREAM_DELTA = 'desktop/stream-delta';
  const EV_STREAM_DONE = 'desktop/stream-done';
  const EV_STREAM_ERROR = 'desktop/stream-error';
  const EV_APPROVAL_REQUEST = 'desktop/approval-request';
  const EV_LLM_START = 'desktop/llm-start';
  const EV_TOOL_START = 'desktop/tool-start';
  const EV_TOOL_END = 'desktop/tool-end';
  const EV_RUN_STATUS = 'desktop/run-status';
  const EV_MESSAGE_DEQUEUED = 'desktop/message-dequeued';
  const EV_MESSAGE_BLOCKED = 'desktop/message-blocked';
  const EV_JOB_DELIVERY = 'desktop/job-delivery';
  const EV_JOB_DELIVERY_PENDING = 'desktop/job-delivery-pending';
  const EV_TURN_DIGEST = 'desktop/turn-digest';

  const [approvalRequest, setApprovalRequest] = useState(null);
  const [toolActivity, setToolActivity] = useState('');
  const [runStatus, setRunStatus] = useState(null);
  // Prompts typed during a run, waiting for their own turn. The Go side owns the
  // real queue; this mirrors it so the transcript can show what is still waiting.
  const [queuedMessages, setQueuedMessages] = useState([]);
  // What the last finished turn is worth telling the user: a recap of what it
  // did, and the answer it expects next. Held apart from `messages` on purpose —
  // stream-done reloads that list from the session, and neither of these is in
  // the session or ever goes back to the model.
  const [turnDigest, setTurnDigest] = useState(null);

  useEffect(() => {
    const unsubDelta = EventsOn(EV_STREAM_DELTA, (delta) => {
      if (typeof delta !== 'string') return;
      streamingContentRef.current += delta;
      setMessages((prev) => {
        const next = [...prev];
        const last = next[next.length - 1];
        if (last?.role === 'assistant') {
          next[next.length - 1] = { ...last, content: streamingContentRef.current };
        }
        return next;
      });
    });
    const unsubDone = EventsOn(EV_STREAM_DONE, (payload) => {
      const reply = payload?.reply ?? streamingContentRef.current;
      const sessionId = payload?.session_id ?? '';
      streamingContentRef.current = '';
      setToolActivity('');
      setRunStatus({
        context_tokens: payload?.context_tokens ?? 0,
        context_window: payload?.context_window ?? 0,
        prompt_tokens: payload?.prompt_tokens ?? 0,
        completion_tokens: payload?.completion_tokens ?? 0,
        total_prompt_tokens: payload?.total_prompt_tokens ?? 0,
        total_completion_tokens: payload?.total_completion_tokens ?? 0,
      });
      setMessages((prev) => {
        const next = [...prev];
        const last = next[next.length - 1];
        if (last?.role === 'assistant') {
          // Cancellation before any model output: drop the empty placeholder
          // so the UI does not leave a blank assistant bubble behind.
          if (!reply) return next.slice(0, -1);
          next[next.length - 1] = { ...last, content: reply };
        }
        return next;
      });
      setNewChatProject(null);
      setSelectedId(sessionId || null);
      if (sessionId) {
        getApp()?.GetSession(sessionId).then((detail) => {
          setMessages(detail?.messages ?? []);
          setSessionTitle(detail?.title?.trim() || 'Chat');
        }).catch(() => {});
      }
      getApp()?.ListSessions().then((list) => setSessions(list ?? [])).catch(() => {});
      // The run goroutine only emits stream-done once the queue is drained.
      setQueuedMessages([]);
      setLoading(false);
    });
    const unsubError = EventsOn(EV_STREAM_ERROR, (payload) => {
      streamingContentRef.current = '';
      setToolActivity('');
      setError(payload?.message ?? 'Stream error');
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (last?.role === 'assistant' && last?.content === '') return prev.slice(0, -1);
        return prev;
      });
      setLoading(false);
    });
    const unsubApproval = EventsOn(EV_APPROVAL_REQUEST, (payload) => {
      setApprovalRequest(payload);
    });
    const unsubLLMStart = EventsOn(EV_LLM_START, () => {
      streamingContentRef.current = '';
      setMessages((prev) => appendAssistantForNextLLM(prev));
    });
    const unsubToolStart = EventsOn(EV_TOOL_START, (payload) => {
      const name = payload?.tool_name ?? '';
      const args = payload?.args ? shortToolArgs(payload.args) : '';
      setToolActivity(args ? `⚙ ${name} (${args})` : `⚙ ${name}`);
      if (payload?.tool_call_id) {
        setMessages((prev) => addLiveToolCall(prev, {
          id: payload.tool_call_id,
          name,
          arguments: payload?.args || '',
        }));
      }
    });
    const unsubToolEnd = EventsOn(EV_TOOL_END, (payload) => {
      setToolActivity('');
      setMessages((prev) => addLiveToolResult(prev, payload));
    });
    const unsubRunStatus = EventsOn(EV_RUN_STATUS, (payload) => {
      setRunStatus((prev) => mergeRunStatus(prev, payload));
    });
    // A queued prompt starting its own turn: move it out of the waiting list and
    // into the transcript as a sent message, and keep the run indicator on.
    const unsubDequeued = EventsOn(EV_MESSAGE_DEQUEUED, (payload) => {
      setQueuedMessages(payload?.queued ?? []);
      const prompt = payload?.prompt ?? '';
      if (!prompt) return;
      setError(null);
      setLoading(true);
      streamingContentRef.current = '';
      setMessages((prev) => [...prev, { role: 'user', content: prompt }]);
    });
    // A hook refused a queued message. The run is still going, so this reports the
    // one message and leaves the loading state alone.
    const unsubBlocked = EventsOn(EV_MESSAGE_BLOCKED, (payload) => {
      setQueuedMessages(payload?.queued ?? []);
      setError(`Message blocked by hook: ${payload?.reason ?? 'no reason given'}`);
    });
    // A background delivery turn is starting: mark why the agent is about to
    // speak unprompted. The placeholder is replaced by the persisted envelope
    // message when stream-done reloads the session.
    const unsubJobDelivery = EventsOn(EV_JOB_DELIVERY, (payload) => {
      setError(null);
      setLoading(true);
      streamingContentRef.current = '';
      setMessages((prev) => [...prev, {
        role: 'user',
        source: payload?.source || 'background_event',
        content: `⟳ ${payload?.source ?? 'background event'} from ${payload?.job_id ?? ''} — ${payload?.title ?? ''}`,
      }]);
    });
    // Emitted once per turn, and only when the turn earned something to say.
    const unsubTurnDigest = EventsOn(EV_TURN_DIGEST, (payload) => {
      setTurnDigest({
        recap: payload?.recap ?? '',
        suggestion: payload?.suggestion ?? '',
      });
    });
    return () => {
      EventsOff(EV_STREAM_DELTA, EV_STREAM_DONE, EV_STREAM_ERROR, EV_APPROVAL_REQUEST, EV_LLM_START, EV_TOOL_START, EV_TOOL_END, EV_RUN_STATUS, EV_MESSAGE_DEQUEUED, EV_MESSAGE_BLOCKED, EV_JOB_DELIVERY, EV_TURN_DIGEST);
      unsubTurnDigest?.();
      unsubDelta?.();
      unsubDone?.();
      unsubError?.();
      unsubApproval?.();
      unsubLLMStart?.();
      unsubToolStart?.();
      unsubToolEnd?.();
      unsubRunStatus?.();
      unsubDequeued?.();
      unsubBlocked?.();
      unsubJobDelivery?.();
    };
  }, []);

  // Pull parked background deliveries whenever this session is on screen and
  // idle. The Go side parks per session and cannot know what is on screen;
  // this effect is that knowledge. Re-running on `loading` flips also drains
  // several parked events one turn at a time.
  useEffect(() => {
    if (loading || !wailsReady || !currentProject) return undefined;
    const app = getApp();
    if (!app?.DeliverNextJobEvent) return undefined;
    const pull = () => {
      app.DeliverNextJobEvent(currentProject.id, selectedId || '').catch(() => {});
    };
    pull();
    const unsub = EventsOn(EV_JOB_DELIVERY_PENDING, (p) => {
      if (p?.project_id === currentProject.id && (p?.session_id ?? '') === (selectedId || '')) pull();
    });
    return unsub;
  }, [loading, wailsReady, currentProject, selectedId]);

  // The queue lives per project on the Go side; re-read it when the visible
  // project changes so a queue built before a switch is still shown after it.
  useEffect(() => {
    const a = getApp();
    if (!a || !currentProject?.id || typeof a.QueuedMessages !== 'function') {
      setQueuedMessages([]);
      return;
    }
    let stale = false;
    a.QueuedMessages(currentProject.id)
      .then((list) => { if (!stale) setQueuedMessages(list ?? []); })
      .catch(() => {});
    return () => { stale = true; };
  }, [currentProject?.id]);

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

  // Center workspace tabs: the chat is one tab, terminals are peer tabs. File
  // and diff tabs are a later slice (see the desktop-workspace-tabs proposal).
  const chatTabKey = 'chat:current';
  const termSeqRef = useRef(0);

  // Seed one chat tab per project and reap the previous project's terminals.
  useEffect(() => {
    setCenter((prev) => {
      prev.tabs
        .filter((t) => t.kind === 'terminal')
        .forEach((t) => getApp()?.TerminalClose?.(t.ref));
      if (!currentProject) return emptyTabs;
      return openTab(emptyTabs, {
        kind: 'chat', ref: 'current', title: sessionTitle || 'New Chat', closable: false,
      });
    });
    // Reset the tab set only when the active project changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentProject?.id]);

  // Keep the chat tab's title in step with the active session.
  useEffect(() => {
    setCenter((s) => ({
      ...s,
      tabs: s.tabs.map((t) => (t.key === chatTabKey ? { ...t, title: sessionTitle || 'New Chat' } : t)),
    }));
  }, [sessionTitle]);

  const openTerminalTab = useCallback(async () => {
    const a = getApp();
    if (!a?.TerminalOpen || !currentProject) return;
    try {
      const id = await a.TerminalOpen(currentProject.id);
      termSeqRef.current += 1;
      setCenter((s) => openTab(s, { kind: 'terminal', ref: id, title: `Terminal ${termSeqRef.current}` }));
    } catch {
      // Opening a shell can fail (e.g. unsupported platform); leave the tabs.
    }
  }, [currentProject]);

  const selectCenterTab = useCallback((key) => setCenter((s) => focusTab(s, key)), []);
  const closeCenterTab = useCallback((key) => {
    setCenter((s) => {
      const tab = s.tabs.find((t) => t.key === key);
      if (tab?.kind === 'terminal') getApp()?.TerminalClose?.(tab.ref);
      return closeTab(s, key);
    });
  }, []);

  const activeCenterKind = activeTab(center)?.kind ?? null;

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

  useEffect(() => {
    if (!wailsReady || !app || !currentProject) {
      setRunStatus(null);
      return;
    }
    app.GetRunStatus(currentProject.id, selectedId || '')
      .then((status) => setRunStatus(status ?? null))
      .catch(() => setRunStatus(null));
  }, [wailsReady, app, currentProject, selectedId]);

  // Signing out returns the app to local mode, which is a working state rather
  // than a locked door: the workbench stays open on settings.yaml's models.
  function handleLogout() {
    if (!app) return;
    const signedOut = { logged_in: false };
    app.Logout()
      .then(() => setAuthStatus(signedOut))
      .catch(() => setAuthStatus(signedOut));
  }

  // reloadSession is also how a rewind refreshes the transcript: the session id
  // does not change, so nothing else would re-read it.
  const reloadSession = useCallback((id) => {
    if (!app || !id) return;
    app.GetSession(id)
      .then((detail) => {
        setMessages(detail?.messages ?? []);
        setSessionTitle(detail?.title?.trim() || 'Chat');
      })
      .catch((err) => setError(err?.message ?? String(err)));
  }, [app]);

  useEffect(() => {
    if (selectedId === null) {
      setMessages([]);
      setSessionTitle(newChatProject ? 'New Chat' : '');
      return;
    }
    if (!app) return;
    setError(null);
    reloadSession(selectedId);
  }, [selectedId, app, newChatProject, reloadSession]);

  useEffect(() => {
    if (historyRef.current) {
      historyRef.current.scrollTop = historyRef.current.scrollHeight;
    }
  }, [messages]);

  function handleSelectSession(sessionId) {
    setNewChatProject(null);
    setHistoryNotice(null);
    setSelectedId(sessionId);
    // A digest belongs to one turn of one conversation; it does not follow the
    // user to another.
    setTurnDigest(null);
  }

  function handleRewound(report) {
    reloadSession(selectedId);
    setHistoryNotice({ kind: 'rewind', text: report });
    // The turn the digest described is no longer the last one, and the question
    // the suggestion answered may have been rewound away.
    setTurnDigest(null);
  }

  // A fork is a different session, so this navigates to it. The notice is set
  // after the switch rather than through handleSelectSession, which clears it.
  function handleForked(newSessionId, report) {
    setNewChatProject(null);
    setSelectedId(newSessionId);
    setHistoryNotice({ kind: 'fork', text: report });
    // Not routed through handleSelectSession, so the digest is dropped here too.
    setTurnDigest(null);
    app?.ListSessions().then((list) => setSessions(list ?? [])).catch(() => {});
  }

  // A compaction rewrites the session's model-visible history in place, so the
  // transcript and the context gauge both re-read. The notice says what it did.
  function handleCompacted(result) {
    reloadSession(selectedId);
    const summarized = result?.summarized ?? 0;
    const text = summarized > 0
      ? `Summarized ${summarized} message${summarized === 1 ? '' : 's'}, kept ${result?.kept ?? 0}. Context now ${result?.after_tokens ?? 0} tokens (was ${result?.before_tokens ?? 0}).`
      : (result?.reason || 'Nothing to compact yet.');
    setHistoryNotice({ kind: 'compact', text });
    setTurnDigest(null);
    if (app && selectedId) {
      app.GetRunStatus(currentProject?.id ?? '', selectedId)
        .then((status) => setRunStatus((prev) => mergeRunStatus(prev, status)))
        .catch(() => {});
    }
  }


  function handleNewChatInProject(project) {
    setProjectNotices([]);
    app?.ProjectNotices?.(project.id)
      .then((lines) => setProjectNotices(lines ?? []))
      .catch(() => {});
    setNewChatProject(project);
    setSelectedId(null);
    setMessages([]);
    setSessionTitle('New Chat');
    setTurnDigest(null);
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
      // If the deleted project was in use, clear the chat area.
      if (currentProject?.id === id) {
        setNewChatProject(null);
        setSelectedId(null);
        setMessages([]);
      }
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  async function handleRenameSession(id, title) {
    try {
      await app.RenameSession(id, title);
      setSessions((prev) => prev.map((s) => s.id === id ? { ...s, title } : s));
      if (selectedId === id) setSessionTitle(title || 'Chat');
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  }

  async function handleDeleteSession(id) {
    try {
      await app.DeleteSession(id);
      setSessions((prev) => prev.filter((s) => s.id !== id));
      if (selectedId === id) {
        setSelectedId(null);
        setMessages([]);
        setSessionTitle(newChatProject ? 'New Chat' : '');
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
      if (selectedId && deletedSet.has(selectedId)) {
        setSelectedId(null);
        setMessages([]);
        setSessionTitle('');
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

  async function handleSend(prompt) {
    if (!prompt?.trim() || !app || !currentProject) return;
    const trimmed = prompt.trim();

    // While a run is in flight the message is queued rather than refused. Ask the
    // Go side first: it owns the queue, and its answer says which of the two happened.
    if (loading) {
      try {
        const position = await app.SendMessageStream(currentProject.id, selectedId || '', trimmed);
        if (position > 0) setQueuedMessages((prev) => [...prev, trimmed]);
      } catch (err) {
        setError(err?.message ?? String(err));
      }
      return;
    }

    setLoading(true);
    setError(null);
    setHistoryNotice(null);
    // The recap described the previous turn and the suggestion answered the
    // question it asked. Both are spent the moment a new turn starts.
    setTurnDigest(null);
    streamingContentRef.current = '';
    setRunStatus((prev) => ({ ...(prev ?? {}), prompt_tokens: 0, completion_tokens: 0 }));
    setMessages((prev) => [
      ...prev,
      { role: 'user', content: trimmed },
      { role: 'assistant', content: '' },
    ]);
    try {
      await app.SendMessageStream(currentProject.id, selectedId || '', trimmed);
    } catch (err) {
      setError(err?.message ?? String(err));
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (last?.role === 'assistant' && last?.content === '') return prev.slice(0, -1);
        return prev;
      });
      setLoading(false);
    }
  }

  async function handleCancel() {
    if (!loading || !app || !currentProject) return;
    // Stopping discards the queue on both sides — see App.CancelRun.
    setQueuedMessages([]);
    try {
      await app.CancelRun(currentProject.id);
    } catch (err) {
      // Cancellation is best-effort; surface unexpected failures but do not
      // block UI state — the in-flight run will still complete via stream-done.
      console.error('CancelRun failed:', err);
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

  const toolResults = buildToolResultMap(messages);

  const threadItems = messages.flatMap((m, i) => {
    if (m.role === 'tool') return [];
    const toolCallLines = (m.tool_calls || []).map((tc, j) => {
      const result = toolResults.get(tc.id);
      const state = result ? (result.ok ? 'success' : 'error') : 'pending';
      const args = shortToolArgs(tc.arguments);
      return (
        <details key={tc.id || j} className={`page-chat__tool-call page-chat__tool-call--${state}`}>
          <summary>
            <span className="page-chat__tool-call-dot" aria-hidden />
            <span className="page-chat__tool-call-name">{toolDisplayName(tc.name)}</span>
            {args && <span className="page-chat__tool-call-args">({args})</span>}
          </summary>
          {tc.arguments && (
            <pre className="page-chat__tool-call-block">{formatToolArgs(tc.arguments)}</pre>
          )}
          {result?.content && (
            <div className="page-chat__tool-call-result">
              <MarkdownMessage content={result.content} />
            </div>
          )}
        </details>
      );
    });
    // A user-role message with a source is a background event, not the
    // user's words: label it and collapse the envelope behind a summary.
    if (m.source) {
      return [{
        id: `message-${i}`,
        role: m.role,
        label: 'Background',
        hideAvatar: true,
        body: (
          <details className="page-chat__msg-content">
            <summary>⟳ {m.source}</summary>
            {m.content ? <MarkdownMessage content={m.content} /> : null}
          </details>
        ),
      }];
    }
    return [{
      id: `message-${i}`,
      role: m.role,
      label: m.role === 'user' ? 'You' : m.role,
      hideAvatar: true,
      body: (
        <div className="page-chat__msg-content">
          {m.content ? <MarkdownMessage content={m.content} /> : null}
          {toolCallLines}
        </div>
      ),
    }];
  });

  for (const [i, line] of projectNotices.entries()) {
    threadItems.push({
      id: `project-notice-${i}`,
      role: 'notice',
      label: 'Project',
      hideAvatar: true,
      body: <div className="page-chat__msg-content">{line}</div>,
    });
  }

  if (historyNotice) {
    threadItems.push({
      id: 'history-notice',
      role: 'system',
      label: historyNotice.kind === 'fork' ? 'Forked'
        : historyNotice.kind === 'compact' ? 'Compacted'
        : 'Rewound',
      hideAvatar: true,
      body: <div className="page-chat__msg-content page-chat__history-notice">{historyNotice.text}</div>,
    });
  }

  // Queued messages sit at the end of the transcript, dimmed: typed and accepted,
  // but not sent to the model yet.
  for (const [i, queued] of queuedMessages.entries()) {
    threadItems.push({
      id: `queued-${i}`,
      role: 'user',
      label: 'You (queued)',
      hideAvatar: true,
      body: (
        <div className="page-chat__msg-content page-chat__msg-content--queued">
          <MarkdownMessage content={queued} />
        </div>
      ),
    });
  }

  // The recap closes the transcript as a notice, not a message: it is shown to
  // the user and never said to the agent.
  if (turnDigest?.recap) {
    threadItems.push({
      id: 'turn-recap',
      role: 'notice',
      label: 'Turn recap',
      hideAvatar: true,
      body: <div className="page-chat__recap">{turnDigest.recap}</div>,
    });
  }

  const inspectorOpen = !!currentProject && inspector.open;
  const inspectorExpanded = inspectorOpen && inspector.expanded;
  const shellClass = [
    'shell',
    leftCollapsed ? 'shell--left-collapsed' : '',
    inspectorExpanded ? 'shell--inspector-expanded' : '',
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
                      selectedSessionId={selectedId}
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
                  <ThemeMenuItem />
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
            <div className="shell__top">
              {leftCollapsed && (
                <button
                  type="button"
                  className="shell__sidebar-toggle"
                  onClick={() => setLeftCollapsed(false)}
                  title="Show sidebar"
                  aria-label="Show sidebar"
                >
                  ☰
                </button>
              )}
              <div className="shell__top-titles">
                <span className="shell__title">
                  {currentProject ? (sessionTitle || 'New Chat') : 'Home'}
                </span>
              </div>
              {currentProject && (
                <div className="inspector-tabs" role="group" aria-label="Inspector views">
                  <button
                    type="button"
                    className="inspector-tabs__btn"
                    aria-pressed={inspectorOpen && inspector.view === 'files'}
                    onClick={() => toggleInspectorView('files')}
                    title="Files"
                    aria-label="Files"
                  >
                    <span className="inspector-tabs__icon"><FilesIcon /></span>
                  </button>
                  <button
                    type="button"
                    className="inspector-tabs__btn"
                    aria-pressed={inspectorOpen && inspector.view === 'diff'}
                    onClick={() => toggleInspectorView('diff')}
                    title="Changes"
                    aria-label="Changes"
                  >
                    <span className="inspector-tabs__icon"><ChangesIcon /></span>
                  </button>
                  <button
                    type="button"
                    className="inspector-tabs__btn"
                    aria-pressed={inspectorOpen && inspector.view === 'info'}
                    onClick={() => toggleInspectorView('info')}
                    title="Session info"
                    aria-label="Session info"
                  >
                    <span className="inspector-tabs__icon"><InfoIcon /></span>
                  </button>
                  <button
                    type="button"
                    className="inspector-tabs__btn"
                    onClick={openTerminalTab}
                    title="New terminal"
                    aria-label="New terminal"
                  >
                    <span className="inspector-tabs__icon" aria-hidden>{'>_'}</span>
                  </button>
                </div>
              )}
            </div>
            <div className="shell__content">
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
                <div className="workspace-tabs">
                  <TabBar
                    tabs={center.tabs}
                    activeKey={center.activeKey}
                    onSelect={selectCenterTab}
                    onClose={closeCenterTab}
                  />
                  <div className="workspace-tabs__content">
                    {activeCenterKind === 'chat' && (
                      <div className="page-chat">
                        <ChatThread
                          historyRef={historyRef}
                          ariaLabel="Conversation history"
                          items={threadItems}
                          emptyText="Type a message below to start a new chat."
                        />
                        <section className="page-chat__input" aria-label="Send a message">
                          <ChatInput
                            onSend={handleSend}
                            onCancel={handleCancel}
                            loading={loading}
                            error={error}
                            onDismissError={() => setError(null)}
                            currentProject={currentProject}
                            app={app}
                            approvalRequest={approvalRequest}
                            onRespond={handleRespond}
                            toolActivity={toolActivity}
                            runStatus={runStatus}
                            suggestion={turnDigest?.suggestion ?? ''}
                            onAcceptSuggestion={() => setTurnDigest(null)}
                            sessionId={selectedId || ''}
                            onOpenInspector={openInspector}
                            onRewound={handleRewound}
                            onForked={handleForked}
                            onCompacted={handleCompacted}
                            onCommandError={(msg) => setError(msg)}
                            onRunStatusContext={(status) => {
                              setRunStatus((prev) => ({
                                ...(status ?? {}),
                                prompt_tokens: prev?.prompt_tokens ?? 0,
                                completion_tokens: prev?.completion_tokens ?? 0,
                                total_prompt_tokens: prev?.total_prompt_tokens ?? status?.total_prompt_tokens ?? 0,
                                total_completion_tokens: prev?.total_completion_tokens ?? status?.total_completion_tokens ?? 0,
                              }));
                            }}
                          />
                        </section>
                      </div>
                    )}
                    {center.tabs
                      .filter((t) => t.kind === 'terminal')
                      .map((t) => (
                        <TerminalPane key={t.key} id={t.ref} active={t.key === center.activeKey} />
                      ))}
                  </div>
                </div>
              )}
            </div>
          </main>

          {inspectorOpen && (
            <>
              <div
                className="inspector-resizer"
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize inspector"
                onMouseDown={startInspectorResize}
              />
              <Inspector
                view={inspector.view}
                expanded={inspector.expanded}
                width={inspector.expanded ? null : inspectorWidth}
                projectID={currentProject.id}
                sessionID={selectedId || ''}
                projectName={currentProject.name}
                workspace={currentSession?.workspace || currentProject.default_workspace}
                app={app}
                onToggleExpand={toggleInspectorExpand}
                onClose={closeInspector}
              />
            </>
          )}
        </div>
      </div>

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
