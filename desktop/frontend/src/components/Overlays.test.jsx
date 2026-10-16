import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { ConfirmModal, CreateProjectModal, MCPModal, PluginsModal } from './Modals';
import { HistoryModal } from './HistoryModal';
import { JobsDrawer } from './JobsDrawer';

// Every Desktop dialog and drawer is gui's BaseModal or Drawer, which owns the
// overlay contract: focus moves in on open, Tab stays inside, Escape closes,
// and focus returns to whatever opened it. One test per overlay kind proves
// each Desktop overlay gets that contract, rather than its own key handling.

afterEach(cleanup);

// Opener renders a trigger button that mounts the overlay, the way every
// Desktop caller does, and unmounts it on close.
function Opener({ label = 'open', overlay }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>{label}</button>
      {open && overlay(() => setOpen(false))}
    </>
  );
}

async function openFrom(label) {
  const trigger = screen.getByRole('button', { name: label });
  trigger.focus();
  fireEvent.click(trigger);
  return trigger;
}

function focusables(dialog) {
  return Array.from(dialog.querySelectorAll('button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'));
}

// expectTrapped tabs off the last focusable and back from the first, and
// expects both to wrap inside the dialog.
function expectTrapped(dialog) {
  const items = focusables(dialog);
  items[items.length - 1].focus();
  fireEvent.keyDown(document, { key: 'Tab' });
  expect(document.activeElement).toBe(items[0]);
  fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
  expect(document.activeElement).toBe(items[items.length - 1]);
}

function expectClosesBackTo(trigger, name) {
  fireEvent.keyDown(document, { key: 'Escape' });
  expect(screen.queryByRole('dialog', { name })).toBeNull();
  expect(document.activeElement).toBe(trigger);
}

describe('Desktop overlays', () => {
  it('a form dialog (New Project) starts at its first field, traps focus, and returns it', async () => {
    render(<Opener overlay={(close) => <CreateProjectModal app={{}} onCreate={vi.fn()} onClose={close} />} />);
    const trigger = await openFrom('open');
    const dialog = screen.getByRole('dialog', { name: 'New Project' });
    expect(document.activeElement).toBe(within(dialog).getByLabelText('Name'));
    expectTrapped(dialog);
    expectClosesBackTo(trigger, 'New Project');
  });

  it('a confirmation traps focus and returns it on Escape', async () => {
    const onCancel = vi.fn();
    function Confirming() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>Delete</button>
          {open && (
            <ConfirmModal
              title="Delete this?"
              message="It cannot be undone."
              onConfirm={vi.fn()}
              onCancel={() => { onCancel(); setOpen(false); }}
            />
          )}
        </>
      );
    }
    render(<Confirming />);
    const trigger = await openFrom('Delete');
    const dialog = screen.getByRole('dialog', { name: 'Delete this?' });
    expect(dialog.contains(document.activeElement)).toBe(true);
    expectTrapped(dialog);
    expectClosesBackTo(trigger, 'Delete this?');
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('a list dialog (MCP servers) traps focus and returns it', async () => {
    const app = { GetSlashMCP: vi.fn(() => Promise.resolve({ servers: [] })) };
    render(<Opener label="/mcp" overlay={(close) => <MCPModal projectID="p1" app={app} onClose={close} />} />);
    const trigger = await openFrom('/mcp');
    const dialog = screen.getByRole('dialog', { name: 'MCP Servers' });
    await within(dialog).findByText(/no mcp servers/i);
    expect(dialog.contains(document.activeElement)).toBe(true);
    expectTrapped(dialog);
    expectClosesBackTo(trigger, 'MCP Servers');
  });

  it('the history picker traps focus and returns it', async () => {
    const app = { GetHistoryPoints: vi.fn(() => Promise.resolve({ rewind: [], fork: [] })) };
    render(
      <Opener
        label="/history"
        overlay={(close) => (
          <HistoryModal projectID="p1" sessionID="s1" app={app} draft="" onRewound={vi.fn()} onForked={vi.fn()} onClose={close} />
        )}
      />,
    );
    const trigger = await openFrom('/history');
    const dialog = screen.getByRole('dialog', { name: 'History' });
    await waitFor(() => expect(app.GetHistoryPoints).toHaveBeenCalled());
    expect(dialog.contains(document.activeElement)).toBe(true);
    expectTrapped(dialog);
    expectClosesBackTo(trigger, 'History');
  });

  it('a confirmation over a dialog answers Escape alone and returns focus inside the dialog', async () => {
    const app = {
      GetPlugins: vi.fn(() => Promise.resolve({
        plugins: [{ name: 'local-tools', source: 'repository', state: 'active', path: '/src/local-tools' }],
      })),
      UninstallPlugin: vi.fn(() => Promise.resolve()),
    };
    render(<Opener label="/plugins" overlay={(close) => <PluginsModal projectID="p1" app={app} onClose={close} />} />);
    const trigger = await openFrom('/plugins');
    const plugins = screen.getByRole('dialog', { name: 'Plugins' });
    const remove = await within(plugins).findByRole('button', { name: 'Remove' });
    remove.focus();
    fireEvent.click(remove);

    const confirm = screen.getByRole('dialog', { name: 'Remove plugin' });
    expect(confirm.contains(document.activeElement)).toBe(true);
    expectTrapped(confirm);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Remove plugin' })).toBeNull();
    expect(screen.getByRole('dialog', { name: 'Plugins' })).toBeTruthy();
    expect(document.activeElement).toBe(remove);
    expect(app.UninstallPlugin).not.toHaveBeenCalled();

    expectClosesBackTo(trigger, 'Plugins');
  });

  it('a drawer (background jobs) traps focus and returns it', async () => {
    const app = {
      ListJobs: vi.fn(() => Promise.resolve([
        { id: 'j1', command: 'npm test', state: 'running', running: true, created_at: '2026-01-01T00:00:00Z' },
      ])),
      GetJobOutput: vi.fn(() => Promise.resolve({ data: 'ok' })),
      StopJob: vi.fn(() => Promise.resolve()),
    };
    render(<Opener label="/jobs" overlay={(close) => <JobsDrawer projectID="p1" app={app} onClose={close} />} />);
    const trigger = await openFrom('/jobs');
    const drawer = screen.getByRole('dialog', { name: 'Background Jobs' });
    await within(drawer).findByRole('button', { name: 'Stop' });
    await act(async () => {});
    expect(drawer.contains(document.activeElement)).toBe(true);
    expectTrapped(drawer);
    expectClosesBackTo(trigger, 'Background Jobs');
  });
});
