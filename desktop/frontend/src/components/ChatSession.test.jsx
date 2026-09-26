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
// stub surfaces only the approval it is handed, and answers it on click.
vi.mock('./ChatInput', () => ({
  ChatInput: ({ approvalRequest, onRespond, approvalKeys }) => (approvalRequest ? (
    <button type="button" data-keys={String(approvalKeys)} onClick={() => onRespond('once')}>
      approve {approvalRequest.tool_name}
    </button>
  ) : null),
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

function chatSession(sessionId, { approvals = {}, onRespond = () => {}, focused } = {}) {
  const tab = { kind: 'chat', ref: sessionId, sessionId, key: `chat:${sessionId}` };
  return (
    <ChatSession
      key={tab.key}
      projectId="p" projectName="P" defaultWorkspace="/w" sessions={[]}
      tab={tab} app={app} approvals={approvals} focused={focused}
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
