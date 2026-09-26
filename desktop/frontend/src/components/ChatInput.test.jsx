import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ChatInput } from './ChatInput';

afterEach(cleanup);

// ChatInput reaches for the Wails runtime on mount. There is none in a test, and
// the real module already degrades to a no-op — this only keeps that explicit.
vi.mock('../lib/wailsRuntime', () => ({
  EventsOn: () => () => {},
  EventsOff: () => {},
}));

// The project and app bindings ChatInput loads its status bar and palette from.
// Callers override the pieces a given test is about.
function makeApp(overrides = {}) {
  return {
    ListJobs: () => Promise.resolve([]),
    GetSlashModels: () => Promise.resolve({ models: [], current: '' }),
    GetSlashSkills: () => Promise.resolve({ skills: [] }),
    GetGitBranch: () => Promise.resolve(''),
    GetSlashCommands: () => Promise.resolve([]),
    ...overrides,
  };
}

function renderInput(props = {}) {
  const { app: appOverride, ...rest } = props;
  return render(
    <ChatInput
      onSend={() => {}}
      onCancel={() => {}}
      loading={false}
      error={null}
      onDismissError={() => {}}
      currentProject={{ id: 'p1' }}
      app={appOverride ?? makeApp()}
      approvalRequest={null}
      onRespond={() => {}}
      toolActivity=""
      runStatus={null}
      sessionId="s1"
      {...rest}
    />,
  );
}

function composer() {
  return screen.getByLabelText('Message');
}

function pressTab() {
  fireEvent(
    composer(),
    new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }),
  );
}

describe('ChatInput suggestion', () => {
  it('accepts the suggestion into the box rather than sending it', () => {
    const onSend = vi.fn();
    const onAcceptSuggestion = vi.fn();
    renderInput({ suggestion: 'yes, commit it', onSend, onAcceptSuggestion });

    expect(composer().placeholder).toBe('yes, commit it');
    pressTab();

    expect(composer().value).toBe('yes, commit it');
    expect(onAcceptSuggestion).toHaveBeenCalledOnce();
    // Accepting fills the box. Sending is still the user's keystroke.
    expect(onSend).not.toHaveBeenCalled();
  });

  it('sends the accepted suggestion on the next Enter', () => {
    const onSend = vi.fn();
    renderInput({ suggestion: 'yes, commit it', onSend, onAcceptSuggestion: () => {} });

    pressTab();
    fireEvent.keyDown(composer(), { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('yes, commit it');
  });

  it('offers nothing when the turn produced no suggestion', () => {
    renderInput({ suggestion: '', onAcceptSuggestion: () => {} });
    pressTab();
    expect(composer().value).toBe('');
  });
});

describe('ChatInput command palette', () => {
  const commands = [
    { name: 'mcp', description: 'MCP servers', requires_session: false },
    { name: 'info', description: 'Session info', requires_session: true },
  ];

  it('lists commands and skills when "/" is typed', async () => {
    const app = makeApp({
      GetSlashCommands: () => Promise.resolve(commands),
      GetSlashSkills: () => Promise.resolve({ skills: [{ name: 'review', description: 'Review code' }] }),
    });
    renderInput({ app });
    fireEvent.change(composer(), { target: { value: '/' } });

    expect(await screen.findByText('/mcp')).toBeTruthy();
    expect(screen.getByText('/info')).toBeTruthy();
    // The skill is listed too, tagged so it is not mistaken for a command.
    expect(screen.getByText('/review')).toBeTruthy();
    expect(screen.getByText('skill')).toBeTruthy();
  });

  it('disables a session-scoped command until there is a session', async () => {
    const app = makeApp({ GetSlashCommands: () => Promise.resolve(commands) });
    renderInput({ app, sessionId: '' });
    fireEvent.change(composer(), { target: { value: '/info' } });

    const info = await screen.findByRole('option', { name: /info/ });
    expect(info.disabled).toBe(true);
  });

  it('dispatches a command on Enter instead of sending it', async () => {
    const onSend = vi.fn();
    const onCompacted = vi.fn();
    const CompactProjectSession = vi.fn(() => Promise.resolve({ summarized: 3, kept: 2 }));
    const app = makeApp({
      GetSlashCommands: () => Promise.resolve([
        { name: 'compact', description: 'Compact', requires_session: true },
      ]),
      CompactProjectSession,
    });
    renderInput({ app, onSend, onCompacted });

    fireEvent.change(composer(), { target: { value: '/compact' } });
    await screen.findByText('/compact');
    fireEvent.keyDown(composer(), { key: 'Enter' });

    await waitFor(() => expect(CompactProjectSession).toHaveBeenCalledWith('p1', 's1'));
    await waitFor(() => expect(onCompacted).toHaveBeenCalled());
    // A command is not a message.
    expect(onSend).not.toHaveBeenCalled();
  });

  it('still loads the model picker when the commands binding is absent', async () => {
    // A running app that has not regenerated its Wails bindings has no
    // GetSlashCommands; calling it must not take the model load down with it.
    const app = makeApp({
      GetSlashModels: () => Promise.resolve({ models: [{ name: 'gpt-4o' }], current: 'gpt-4o' }),
    });
    delete app.GetSlashCommands;
    renderInput({ app });

    expect(await screen.findByText('gpt-4o')).toBeTruthy();
  });

  it('sends a "/name" that is not a command as a normal message', async () => {
    const onSend = vi.fn();
    const app = makeApp({ GetSlashCommands: () => Promise.resolve(commands) });
    renderInput({ app, onSend });

    // A trailing space closes the palette, so Enter submits rather than picking.
    fireEvent.change(composer(), { target: { value: '/review ' } });
    fireEvent.keyDown(composer(), { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('/review');
  });

  it('marks the switched-to model as active on the next open', async () => {
    // Regression: the picker used to read a per-entry is_current computed once at
    // load, so after a switch it still checked the originally-current model.
    const SetProjectModel = vi.fn(() => Promise.resolve());
    const app = makeApp({
      GetSlashModels: () => Promise.resolve({
        models: [{ name: 'gpt-4o' }, { name: 'claude' }],
        current: 'gpt-4o',
      }),
      SetProjectModel,
      GetRunStatus: () => Promise.resolve(null),
    });
    renderInput({ app });

    // Open the picker and switch to the second model.
    fireEvent.click(await screen.findByTitle('gpt-4o'));
    fireEvent.click(await screen.findByRole('option', { name: /claude/ }));
    await waitFor(() => expect(SetProjectModel).toHaveBeenCalledWith('p1', 's1', 'claude'));

    // Reopen: the newly selected model is the active option, not the first.
    fireEvent.click(await screen.findByTitle('claude'));
    const active = await screen.findByRole('option', { name: /claude/ });
    expect(active.getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('option', { name: /gpt-4o/ }).getAttribute('aria-selected')).toBe('false');
  });
});

describe('ChatInput draft', () => {
  it('fills the composer from a handed-in draft once, without sending it', async () => {
    const onSend = vi.fn();
    const onDraftConsumed = vi.fn();
    renderInput({ onSend, draft: { text: 'Work on this issue', seq: 1 }, onDraftConsumed });
    await waitFor(() => expect(composer().value).toBe('Work on this issue'));
    expect(onDraftConsumed).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
  });
});
