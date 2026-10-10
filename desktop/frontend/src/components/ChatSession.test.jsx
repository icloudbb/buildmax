import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@buildmax/gui';

// Capture the event handlers ChatSession registers so the test can emit tagged
// stream events and assert they route by session_id.
const handlers = {};
vi.mock('../lib/wailsRuntime', () => ({
  EventsOn: (name, cb) => { (handlers[name] ??= []).push(cb); return () => {}; },
  EventsOff: () => {},
}));
// The input and info panel are irrelevant to routing; stub them out. The input
// stub surfaces only the approval it is handed, and answers it on click. It
// also hands the test its onSend, so a test can send as the composer would.
const composer = {};
vi.mock('./ChatInput', () => ({
  ChatInput: ({ approvalRequest, onRespond, approvalKeys, onSend }) => {
    composer.send = onSend;
    return approvalRequest ? (
      <button type="button" data-keys={String(approvalKeys)} onClick={() => onRespond('once')}>
        approve {approvalRequest.tool_name}
      </button>
    ) : null;
  },
}));
vi.mock('./InfoPanel', () => ({ InfoPanel: () => null }));

import { ChatSession } from './ChatSession';

function emit(name, payload) {
  act(() => { (handlers[name] || []).forEach((cb) => cb(payload)); });
}

const app = {
  GetSession: () => Promise.resolve({ messages: [], title: 'S' }),
  GetRunStatus: () => Promise.resolve(null),
  QueuedMessages: () => Promise.resolve([]),
};

function chatSession(sessionId, { approvals = {}, onRespond = () => {}, focused, app: appOverride } = {}) {
  const tab = { kind: 'chat', ref: sessionId, sessionId, key: `chat:${sessionId}` };
  return (
    <ChatSession
      key={tab.key}
      projectId="p" projectName="P" defaultWorkspace="/w" sessions={[]}
      tab={tab} app={appOverride ?? app} approvals={approvals} focused={focused}
      onRespond={onRespond} onSessionAdopted={() => {}} onSessionsChanged={() => {}}
      onTitle={() => {}} onOpenSession={() => {}} onShowChanges={() => {}}
    />
  );
}

function renderSession(sessionId, opts) {
  return render(<ThemeProvider>{chatSession(sessionId, opts)}</ThemeProvider>);
}

afterEach(() => { cleanup(); Object.keys(handlers).forEach((k) => delete handlers[k]); });

describe('ChatSession event routing', () => {
  it('handles a stream event tagged with its own session id', async () => {
    renderSession('s1');
    await waitFor(() => expect(handlers['desktop/message-dequeued']?.length).toBeGreaterThan(0));

    // A dequeued prompt for this session joins its transcript.
    emit('desktop/message-dequeued', { session_id: 's1', prompt: 'mine to run', queued: [] });

    expect(await screen.findByText('mine to run')).toBeTruthy();
  });

  it('ignores stream events for another session', async () => {
    renderSession('s1');
    await waitFor(() => expect(handlers['desktop/message-dequeued']?.length).toBeGreaterThan(0));

    emit('desktop/message-dequeued', { session_id: 'other', prompt: 'not mine', queued: [] });

    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByText('not mine')).toBeNull();
  });
});

describe('ChatSession approval routing', () => {
  const approvals = {
    s1: { approval_id: '7', project_id: 'p', session_id: 's1', tool_name: 'Write', args: {} },
    s2: { approval_id: '8', project_id: 'p', session_id: 's2', tool_name: 'Bash', args: {} },
  };

  it('shows each session only its own approval and answers it by id', () => {
    const onRespond = vi.fn();
    render(
      <ThemeProvider>
        {chatSession('s1', { approvals, onRespond })}
        {chatSession('s2', { approvals, onRespond })}
        {chatSession('s3', { approvals, onRespond })}
      </ThemeProvider>,
    );

    // One prompt per asking session; the idle one shows none.
    expect(screen.getAllByRole('button', { name: /approve/ })).toHaveLength(2);

    fireEvent.click(screen.getByRole('button', { name: 'approve Bash' }));
    expect(onRespond).toHaveBeenCalledTimes(1);
    expect(onRespond).toHaveBeenCalledWith(approvals.s2, 'once');
  });

  it('gives approval shortcuts only to the focused pane', () => {
    render(
      <ThemeProvider>
        {chatSession('s1', { approvals, focused: true })}
        {chatSession('s2', { approvals, focused: false })}
      </ThemeProvider>,
    );
    expect(screen.getByRole('button', { name: 'approve Write' }).dataset.keys).toBe('true');
    expect(screen.getByRole('button', { name: 'approve Bash' }).dataset.keys).toBe('false');
  });

  it('shows no approval in a new chat that has not adopted a session', () => {
    renderSession('', { approvals: { ...approvals, '': { approval_id: '9', session_id: '' } } });
    expect(screen.queryByRole('button', { name: /approve/ })).toBeNull();
  });
});

// A run's transcript as the backend persists it, keyed by session id. The
// bridge announces a new chat's id before the run writes the prompt, so a
// freshly adopted session reads back empty (run_lifecycle.go, OnStart).
function runApp(persisted) {
  return {
    GetSession: vi.fn((id) => Promise.resolve({ messages: persisted[id] ?? [], title: 'S' })),
    GetRunStatus: () => Promise.resolve(null),
    QueuedMessages: () => Promise.resolve([]),
    SendMessageStream: vi.fn(() => Promise.resolve(0)),
  };
}

// Let pending promise callbacks (GetSession, SendMessageStream) settle.
async function settle() {
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
}

function emptyAssistantBubbles(container) {
  return [...container.querySelectorAll('.bm-chat-thread__row--assistant .page-chat__msg-content')]
    .filter((el) => el.textContent.trim() === '');
}

const EMPTY_CHAT = 'Type a message below to start a new chat.';

describe('ChatSession sent message', () => {
  it('keeps the first message of a new chat on screen through session adoption', async () => {
    const persisted = {};
    const app = runApp(persisted);
    const { container } = renderSession('', { app });
    await settle();
    expect(screen.getByText(EMPTY_CHAT)).toBeTruthy();

    await act(async () => { await composer.send('add a goodbye line'); });
    expect(app.SendMessageStream).toHaveBeenCalledWith('p', '', 'add a goodbye line');
    expect(screen.getByText('add a goodbye line')).toBeTruthy();
    expect(screen.queryByText(EMPTY_CHAT)).toBeNull();

    // The run announces its new session before it has written the prompt.
    emit('desktop/session-adopted', { session_id: 'new1' });
    await settle();
    expect(screen.getByText('add a goodbye line')).toBeTruthy();
    expect(screen.queryByText(EMPTY_CHAT)).toBeNull();

    // The turn streams text and runs a tool; the sent message stays throughout.
    emit('desktop/llm-start', { session_id: 'new1' });
    emit('desktop/stream-delta', { session_id: 'new1', delta: 'Editing main.go' });
    emit('desktop/tool-start', { session_id: 'new1', tool_call_id: 'c1', tool_name: 'Edit', args: '{"file_path":"main.go"}' });
    await settle();
    expect(screen.getByText('add a goodbye line')).toBeTruthy();
    expect(screen.getByText('Editing main.go')).toBeTruthy();
    emit('desktop/tool-end', { session_id: 'new1', tool_call_id: 'c1' });
    emit('desktop/llm-start', { session_id: 'new1' });
    await settle();
    expect(screen.getByText('add a goodbye line')).toBeTruthy();
    expect(emptyAssistantBubbles(container)).toHaveLength(0);

    // The turn ends without closing text; the reload from disk shows no blank
    // bubble for it.
    persisted.new1 = [
      { role: 'user', content: 'add a goodbye line' },
      { role: 'assistant', content: 'Editing main.go', tool_calls: [{ id: 'c1', name: 'Edit', arguments: '{"file_path":"main.go"}' }] },
      { role: 'tool', tool_call_id: 'c1', content: 'edited' },
      { role: 'assistant', content: '' },
    ];
    emit('desktop/stream-done', { session_id: 'new1', reply: '' });
    await settle();
    expect(app.GetSession).toHaveBeenCalledWith('new1');
    expect(screen.getByText('add a goodbye line')).toBeTruthy();
    expect(screen.getByText('Editing main.go')).toBeTruthy();
    expect(emptyAssistantBubbles(container)).toHaveLength(0);
  });

  it('shows a later message in an existing session at once and keeps it through the turn', async () => {
    const persisted = {
      s1: [
        { role: 'user', content: 'first question' },
        { role: 'assistant', content: 'first answer' },
      ],
    };
    const app = runApp(persisted);
    const { container } = renderSession('s1', { app });
    expect(await screen.findByText('first answer')).toBeTruthy();

    await act(async () => { await composer.send('second question'); });
    expect(app.SendMessageStream).toHaveBeenCalledWith('p', 's1', 'second question');
    expect(screen.getByText('first answer')).toBeTruthy();
    expect(screen.getByText('second question')).toBeTruthy();
    // Waiting for the reply shows no blank bubble.
    expect(emptyAssistantBubbles(container)).toHaveLength(0);

    emit('desktop/llm-start', { session_id: 's1' });
    emit('desktop/tool-start', { session_id: 's1', tool_call_id: 'c2', tool_name: 'Write', args: '{"file_path":"a.txt"}' });
    emit('desktop/stream-delta', { session_id: 's1', delta: 'Done.' });
    await settle();
    expect(screen.getByText('second question')).toBeTruthy();

    persisted.s1 = [
      ...persisted.s1,
      { role: 'user', content: 'second question' },
      { role: 'assistant', content: 'Done.', tool_calls: [{ id: 'c2', name: 'Write', arguments: '{"file_path":"a.txt"}' }] },
      { role: 'tool', tool_call_id: 'c2', content: 'wrote' },
    ];
    emit('desktop/stream-done', { session_id: 's1', reply: 'Done.' });
    await settle();
    expect(screen.getByText('second question')).toBeTruthy();
    expect(screen.getByText('Done.')).toBeTruthy();
    expect(emptyAssistantBubbles(container)).toHaveLength(0);
  });
});
