import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

// The run-detail opens the full ChatSession; stub it so this suite tests the
// Schedules wiring, not the chat stack (ChatSession has its own tests).
vi.mock('./ChatSession', () => ({
  ChatSession: ({ tab, projectId }) => (
    <div data-testid="chat-session">{`chat:${projectId || 'none'}:${tab?.sessionId ?? ''}`}</div>
  ),
}));

import { SchedulesView } from './SchedulesView';

afterEach(cleanup);

function baseApp(tasks = [], runs = []) {
  return {
    ListScheduledTasks: vi.fn(() => Promise.resolve(tasks)),
    ListScheduleRuns: vi.fn(() => Promise.resolve(runs)),
    CreateScheduledTask: vi.fn(() => Promise.resolve({})),
    UpdateScheduledTask: vi.fn(() => Promise.resolve({})),
    DeleteScheduledTask: vi.fn(() => Promise.resolve()),
    PreviewScheduledTask: vi.fn(() => Promise.resolve(['2026-01-02T09:00:00Z', '2026-01-03T09:00:00Z'])),
    GetSlashModels: vi.fn(() => Promise.resolve({ current: 'Fast', models: [{ name: 'Fast' }, { name: 'Deep' }] })),
    SetAllScheduledTasksEnabled: vi.fn(() => Promise.resolve([])),
  };
}

const sampleTask = {
  id: 't1',
  name: 'Morning',
  working_dir: '/home/me/work',
  prompt: 'summarize',
  model: '',
  cron_expr: '0 9 * * *',
  timezone: 'UTC',
  enabled: true,
  next_fire_at: '2026-01-02T09:00:00Z',
  last_fire_at: '',
  consecutive_failures: 0,
};

const sampleRun = {
  id: 'r1',
  schedule_id: 't1',
  schedule_name: 'Morning',
  session_id: 'sess1',
  fired_at: '2026-01-01T09:00:00Z',
  status: 'ok',
};

describe('SchedulesView', () => {
  it('lists tasks with their working directory and cron', async () => {
    render(<SchedulesView app={baseApp([sampleTask])} />);
    expect(await screen.findByText('Morning')).toBeTruthy();
    expect(screen.getByText('/home/me/work')).toBeTruthy();
    expect(screen.getByText('0 9 * * *')).toBeTruthy();
    expect(screen.getByText('Enabled')).toBeTruthy();
  });

  it('lists recent runs and opens one in the chat view', async () => {
    const app = baseApp([sampleTask], [sampleRun]);
    render(<SchedulesView app={app} />);
    const run = await screen.findByRole('button', { name: /Morning/ });
    fireEvent.click(run);
    // The run opens the full chat, projectless, bound to the run's session.
    expect(await screen.findByText('chat:none:sess1')).toBeTruthy();
  });

  it('creates a task from the New Schedule modal without a project', async () => {
    const app = baseApp([]);
    render(<SchedulesView app={app} />);
    await screen.findByText(/No scheduled tasks yet/);

    fireEvent.click(screen.getByRole('button', { name: 'New Schedule' }));
    fireEvent.change(await screen.findByPlaceholderText('What should the agent do each time?'), {
      target: { value: 'do the thing' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create schedule' }));

    await waitFor(() =>
      expect(app.CreateScheduledTask).toHaveBeenCalledWith('', '', 'do the thing', '0 9 * * *', 'UTC', ''),
    );
  });

  it('creates a task with a chosen model', async () => {
    const app = baseApp([]);
    render(<SchedulesView app={app} />);
    fireEvent.click(await screen.findByRole('button', { name: 'New Schedule' }));
    await screen.findByPlaceholderText('What should the agent do each time?');
    await waitFor(() => expect(app.GetSlashModels).toHaveBeenCalled());
    fireEvent.change(screen.getByPlaceholderText('What should the agent do each time?'), {
      target: { value: 'nightly' },
    });
    // Pick a specific model rather than the default.
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'Deep' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create schedule' }));
    await waitFor(() =>
      expect(app.CreateScheduledTask).toHaveBeenCalledWith('', '', 'nightly', '0 9 * * *', 'UTC', 'Deep'),
    );
  });

  it('previews the cron cadence in the form', async () => {
    const app = baseApp([]);
    render(<SchedulesView app={app} />);
    fireEvent.click(await screen.findByRole('button', { name: 'New Schedule' }));
    await waitFor(() => expect(app.PreviewScheduledTask).toHaveBeenCalledWith('0 9 * * *', 'UTC'));
    expect(await screen.findByText('Next runs')).toBeTruthy();
  });

  it('pauses every task with one click when any is enabled', async () => {
    const app = baseApp([sampleTask]);
    render(<SchedulesView app={app} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Pause all' }));
    await waitFor(() => expect(app.SetAllScheduledTasksEnabled).toHaveBeenCalledWith(false));
  });

  it('enables every task with one click when none is enabled', async () => {
    const app = baseApp([{ ...sampleTask, enabled: false }]);
    render(<SchedulesView app={app} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Enable all' }));
    await waitFor(() => expect(app.SetAllScheduledTasksEnabled).toHaveBeenCalledWith(true));
  });

  it('pauses a task through UpdateScheduledTask', async () => {
    const app = baseApp([sampleTask]);
    render(<SchedulesView app={app} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Pause' }));
    await waitFor(() =>
      expect(app.UpdateScheduledTask).toHaveBeenCalledWith(
        't1', '/home/me/work', 'Morning', 'summarize', '0 9 * * *', 'UTC', '', false,
      ),
    );
  });

  it('deletes a task only after confirming in the in-app dialog', async () => {
    const app = baseApp([sampleTask]);
    render(<SchedulesView app={app} />);
    // Opening the confirm dialog does not delete on its own.
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    expect(app.DeleteScheduledTask).not.toHaveBeenCalled();
    // Confirming does — no reliance on window.confirm, which the webview drops.
    fireEvent.click(await screen.findByRole('button', { name: 'Delete task' }));
    await waitFor(() => expect(app.DeleteScheduledTask).toHaveBeenCalledWith('t1'));
  });

  it('cancels a delete without calling the backend', async () => {
    const app = baseApp([sampleTask]);
    render(<SchedulesView app={app} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Delete task' })).toBeNull());
    expect(app.DeleteScheduledTask).not.toHaveBeenCalled();
  });
});
