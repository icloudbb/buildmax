import { describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useChatStart } from './useChatStart';
import { emptyWorkspace, focusedPane, openInFocused } from './panes';
import { activeTab } from './tabs';

const greeter = { id: 'p_greeter', name: 'greeter' };
const notes = { id: 'p_notes', name: 'notes' };
const issueText = 'Work on this issue from the "Alpha" space.';

// A layout restored from a previous visit: a sent chat and a terminal, with the
// terminal in front, which is what the audit saw instead of a new chat.
function restoredLayout() {
  let ws = openInFocused(emptyWorkspace, { kind: 'chat', ref: 'ses_old', sessionId: 'ses_old', title: 'Old chat' });
  ws = openInFocused(ws, { kind: 'terminal', ref: 'pty_1', title: 'Terminal 1' });
  return ws;
}

function front(ws) {
  return activeTab(focusedPane(ws));
}

function setup(currentProjectId) {
  const enterProject = vi.fn();
  let workspace = restoredLayout();
  const setWorkspace = vi.fn((update) => { workspace = update(workspace); });
  const hook = renderHook(
    ({ id }) => useChatStart({ currentProjectId: id, enterProject, setWorkspace }),
    { initialProps: { id: currentProjectId } },
  );
  return { hook, enterProject, setWorkspace, workspace: () => workspace };
}

describe('useChatStart', () => {
  it('opens a new chat with the draft in the current project', () => {
    const { hook, enterProject, workspace } = setup(greeter.id);
    act(() => hook.result.current.startChat(greeter, issueText));

    expect(enterProject).toHaveBeenCalledWith(greeter);
    const tab = front(workspace());
    expect(tab).toMatchObject({ kind: 'chat', sessionId: '' });
    expect(hook.result.current.draftFor(tab, greeter.id)?.text).toBe(issueText);
  });

  it('opens a new chat with the draft once another project comes on screen', () => {
    // Reached from Home or another project: the chosen project is not current.
    const { hook, enterProject, setWorkspace } = setup(null);
    act(() => hook.result.current.startChat(greeter, issueText));

    expect(enterProject).toHaveBeenCalledWith(greeter);
    // Nothing is opened into the outgoing layout.
    expect(setWorkspace).not.toHaveBeenCalled();

    hook.rerender({ id: greeter.id });
    const seeded = hook.result.current.seedWorkspace(restoredLayout(), greeter.id, null);
    const tab = front(seeded);
    expect(tab).toMatchObject({ kind: 'chat', sessionId: '' });
    expect(hook.result.current.draftFor(tab, greeter.id)?.text).toBe(issueText);
  });

  it('offers the draft once, and only to its own project\'s unsent chat', () => {
    const { hook, workspace } = setup(greeter.id);
    act(() => hook.result.current.startChat(greeter, issueText));
    const tab = front(workspace());

    expect(hook.result.current.draftFor(tab, notes.id)).toBeNull();
    expect(hook.result.current.draftFor({ ...tab, sessionId: 'ses_new' }, greeter.id)).toBeNull();

    act(() => hook.result.current.consumeDraft());
    expect(hook.result.current.draftFor(tab, greeter.id)).toBeNull();
  });

  it('restores a project opened without a chat start, and forgets a start the person moved on from', () => {
    const { hook } = setup(null);
    act(() => hook.result.current.startChat(greeter, issueText));

    // The person went to another project before greeter came on screen.
    const elsewhere = hook.result.current.seedWorkspace(restoredLayout(), notes.id, null);
    expect(front(elsewhere)).toMatchObject({ kind: 'terminal' });

    const later = hook.result.current.seedWorkspace(restoredLayout(), greeter.id, null);
    expect(front(later)).toMatchObject({ kind: 'terminal' });
  });

  it('shows the selected session, and a new chat when a layout has no chat', () => {
    const { hook } = setup(null);
    const selected = hook.result.current.seedWorkspace(restoredLayout(), greeter.id, { id: 'ses_old', title: 'Old chat' });
    expect(front(selected)).toMatchObject({ kind: 'chat', sessionId: 'ses_old' });

    const blank = hook.result.current.seedWorkspace(emptyWorkspace, greeter.id, null);
    expect(front(blank)).toMatchObject({ kind: 'chat', sessionId: '' });
  });
});
