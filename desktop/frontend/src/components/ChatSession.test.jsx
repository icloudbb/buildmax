import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@buildmax/gui';

// Capture the event handlers ChatSession registers so the test can emit tagged
// stream events and assert they route by session_id.
const handlers = {};
vi.mock('../lib/wailsRuntime', () => ({
  EventsOn: (name, cb) => { (handlers[name] ??= []).push(cb); return () => {}; },
  EventsOff: () => {},
}));
// The input and info panel are irrelevant to routing; stub them out.
vi.mock('./ChatInput', () => ({ ChatInput: () => null }));
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

function renderSession(sessionId) {
  const tab = { kind: 'chat', ref: sessionId, sessionId, key: `chat:${sessionId}` };
  return render(
    <ThemeProvider>
      <ChatSession
        projectId="p" projectName="P" defaultWorkspace="/w" sessions={[]}
        tab={tab} app={app} approvalRequest={null}
        onRespond={() => {}} onSessionAdopted={() => {}} onSessionsChanged={() => {}}
        onTitle={() => {}} onOpenSession={() => {}} onShowChanges={() => {}}
      />
    </ThemeProvider>,
  );
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
