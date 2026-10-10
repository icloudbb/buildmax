import { useCallback, useRef, useState } from 'react';
import { allTabs, focusPaneTab, openInFocused } from './panes';

// Starting a new chat in a project: the sidebar's "+", a project just opened
// from a folder, and an Issue's Start chat, which also hands the chat a draft
// for its composer. The chosen project is often not the current one, and then
// the new chat cannot be opened until that project's layout has been restored,
// which happens later and sometimes asynchronously (its terminals reopen first).
// So the start is recorded here and honoured when the workspace is seeded.

// openChatTabInto focuses an existing chat tab for a session, else opens one.
export function openChatTabInto(ws, sessionId, title) {
  for (const row of ws.rows) {
    for (const pane of row.panes) {
      const t = pane.tabs.find((x) => x.kind === 'chat' && (x.sessionId ?? '') === sessionId);
      if (t) return focusPaneTab(ws, pane.id, t.key);
    }
  }
  return openInFocused(ws, { kind: 'chat', ref: sessionId, sessionId, title: title || 'Chat' });
}

// openNewChatInto keeps at most one not-yet-sent new chat per project (new chats
// serialize on the same run key), focusing it if present. nextRef names the tab
// it opens otherwise; it is called only then, so focusing spends no number.
export function openNewChatInto(ws, nextRef) {
  for (const row of ws.rows) {
    for (const pane of row.panes) {
      const t = pane.tabs.find((x) => x.kind === 'chat' && (x.sessionId ?? '') === '');
      if (t) return focusPaneTab(ws, pane.id, t.key);
    }
  }
  return openInFocused(ws, { kind: 'chat', ref: nextRef(), sessionId: '', title: 'New Chat' });
}

// useChatStart owns that orchestration. enterProject makes a project current
// (App's selection and view state); setWorkspace updates the current project's
// layout. The draft lives here rather than on the tab, so nothing about an
// Issue is saved with the layout or outlives the hand-off.
export function useChatStart({ currentProjectId, enterProject, setWorkspace }) {
  const newChatSeqRef = useRef(0);
  // The project a new chat was started in that is not yet on screen.
  const pendingRef = useRef(null);
  const [draft, setDraft] = useState(null);

  // A new chat tab's ref is `new-N` and survives adoption of its real session
  // id, so the ChatSession is not remounted when the first run names it.
  const newChatInto = useCallback((ws) => openNewChatInto(ws, () => {
    newChatSeqRef.current += 1;
    return `new-${newChatSeqRef.current}`;
  }), []);

  const startChat = useCallback((project, text = '') => {
    setDraft(text ? { projectId: project.id, text, seq: Date.now() } : null);
    if (project.id === currentProjectId) {
      pendingRef.current = null;
      setWorkspace((ws) => newChatInto(ws));
    } else {
      pendingRef.current = project.id;
    }
    enterProject(project);
  }, [currentProjectId, enterProject, setWorkspace, newChatInto]);

  // seedWorkspace picks the chat a project's layout shows as it comes on
  // screen: a chat just started there, else the selected session's tab
  // (selected is { id, title } or null), else a new chat when the layout has
  // none. A start meant for another project is dropped, since the person has
  // moved on from it.
  const seedWorkspace = useCallback((ws, projectId, selected) => {
    const pending = pendingRef.current;
    pendingRef.current = null;
    if (pending === projectId) return newChatInto(ws);
    if (selected) return openChatTabInto(ws, selected.id, selected.title);
    if (!allTabs(ws).some((t) => t.kind === 'chat')) return newChatInto(ws);
    return ws;
  }, [newChatInto]);

  // The draft goes to the project's not-yet-sent chat, which fills its
  // composer once and then calls consumeDraft.
  const draftFor = useCallback(
    (tab, projectId) => (tab?.kind === 'chat' && !tab.sessionId && draft?.projectId === projectId ? draft : null),
    [draft],
  );
  const consumeDraft = useCallback(() => setDraft(null), []);

  return { startChat, seedWorkspace, newChatInto, draftFor, consumeDraft };
}
