import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SchedulesView } from './SchedulesView';

afterEach(cleanup);

const projects = [{ id: 'p1', name: 'Alpha' }, { id: 'p2', name: 'Beta' }];

function baseApp(tasks = []) {
  return {
    ListScheduledTasks: vi.fn(() => Promise.resolve(tasks)),
    CreateScheduledTask: vi.fn(() => Promise.resolve({})),
    UpdateScheduledTask: vi.fn(() => Promise.resolve({})),
    DeleteScheduledTask: vi.fn(() => Promise.resolve()),
  };
}

const sampleTask = {
  id: 't1',
  name: 'Morning',
  project_id: 'p1',
  prompt: 'summarize',
  cron_expr: '0 9 * * *',
  timezone: 'UTC',
  enabled: true,
  next_fire_at: '2026-01-02T09:00:00Z',
  last_fire_at: '',
  consecutive_failures: 0,
};

describe('SchedulesView', () => {
  it('lists tasks with their project and cron', async () => {
    render(<SchedulesView app={baseApp([sampleTask])} projects={projects} onOpenSession={() => {}} />);
    expect(await screen.findByText('Morning')).toBeTruthy();
    // "Alpha" is both a select option and the card's project label.
    expect(screen.getAllByText('Alpha').length).toBeGreaterThan(1);
    expect(screen.getByText('0 9 * * *')).toBeTruthy();
    expect(screen.getByText('Enabled')).toBeTruthy();
  });

  it('creates a task from the form', async () => {
    const app = baseApp([]);
    render(<SchedulesView app={app} projects={projects} onOpenSession={() => {}} />);
    await screen.findByText('No scheduled tasks yet.');

    fireEvent.change(screen.getByPlaceholderText('What should the agent do each time?'), {
      target: { value: 'do the thing' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create task' }));

    await waitFor(() =>
      expect(app.CreateScheduledTask).toHaveBeenCalledWith('p1', '', 'do the thing', '0 9 * * *', 'UTC'),
    );
  });

  it('pauses a task through UpdateScheduledTask', async () => {
    const app = baseApp([sampleTask]);
    render(<SchedulesView app={app} projects={projects} onOpenSession={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Pause' }));
    await waitFor(() =>
      expect(app.UpdateScheduledTask).toHaveBeenCalledWith('t1', 'Morning', 'summarize', '0 9 * * *', 'UTC', false),
    );
  });

  it('prompts to open a project when there are none', async () => {
    render(<SchedulesView app={baseApp([])} projects={[]} onOpenSession={() => {}} />);
    expect(await screen.findByText('Open a project first, then schedule a task in it.')).toBeTruthy();
  });
});
