import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { Sidebar } from './Sidebar';

afterEach(cleanup);

const project = { id: 'p1', name: 'buildmax', default_workspace: '/src/buildmax' };
const session = { id: 's1', project_id: 'p1', title: 'Fix login redirect', created_at: '2026-09-26T08:00:00Z' };

function renderSidebar(overrides = {}) {
  const props = {
    width: 288,
    view: 'workbench',
    onHome: vi.fn(),
    onSchedules: vi.fn(),
    projects: [project],
    currentProject: null,
    sessionsByProject: { p1: [session] },
    highlightSessionId: null,
    sessionFilter: '',
    onSessionFilterChange: vi.fn(),
    onCreateProject: vi.fn(),
    projectActions: {
      onSelectSession: vi.fn(),
      onNewChat: vi.fn(),
      onRename: vi.fn(),
      onDelete: vi.fn(),
      onClearSessions: vi.fn(),
      onRenameSession: vi.fn(),
      onDeleteSession: vi.fn(),
      onPinSession: vi.fn(),
    },
    explorer: {
      app: { ListWorkspaceDir: () => Promise.resolve([]) },
      sessionID: '',
      mode: 'files',
      onModeChange: vi.fn(),
      onOpenFile: vi.fn(),
      onOpenDiff: vi.fn(),
    },
    account: { authStatus: {}, localMode: true, onSignIn: vi.fn(), onSignOut: vi.fn() },
    ...overrides,
  };
  render(<Sidebar {...props} />);
  return props;
}

describe('Sidebar', () => {
  it('marks Home current and shows no project section when no project is open', () => {
    const props = renderSidebar();
    const home = screen.getByRole('button', { name: 'Home' });
    expect(home.getAttribute('aria-current')).toBe('page');
    expect(screen.queryByRole('region', { name: 'buildmax workspace' })).toBeNull();
    fireEvent.click(home);
    expect(props.onHome).toHaveBeenCalled();
  });

  it('names the open project on its own section', () => {
    renderSidebar({ currentProject: project, highlightSessionId: 's1' });
    expect(screen.getByRole('button', { name: 'Home' }).getAttribute('aria-current')).toBeNull();
    const section = screen.getByRole('region', { name: 'buildmax workspace' });
    expect(section.textContent).toContain('buildmax');
    expect(screen.getByRole('button', { name: 'Files' }).getAttribute('aria-pressed')).toBe('true');
  });

  it('shows only Schedules as selected while it is the center view', () => {
    renderSidebar({ view: 'schedules', currentProject: project, highlightSessionId: 's1' });
    expect(screen.getByRole('button', { name: 'Schedules' }).getAttribute('aria-current')).toBe('page');
    expect(screen.queryByRole('region', { name: 'buildmax workspace' })).toBeNull();
    // Only Schedules is selected: the session behind it is not.
    fireEvent.click(screen.getByRole('button', { name: 'buildmax' }));
    expect(screen.getByRole('button', { name: /Fix login redirect/ }).getAttribute('aria-current')).toBeNull();
  });

  it('switches the project section mode with its icon buttons', () => {
    const props = renderSidebar({ currentProject: project });
    fireEvent.click(screen.getByRole('button', { name: 'Changes' }));
    expect(props.explorer.onModeChange).toHaveBeenCalledWith('changes');
  });

  it('collapses the project section to its header', () => {
    renderSidebar({ currentProject: project });
    const section = screen.getByRole('region', { name: 'buildmax workspace' });
    const toggle = within(section).getByRole('button', { name: 'buildmax' });
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByLabelText('Workspace files')).toBeNull();
  });

  it('reveals session search on demand and clears it on Escape', () => {
    const props = renderSidebar();
    expect(screen.queryByRole('searchbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Search sessions' }));
    const input = screen.getByRole('searchbox');
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(props.onSessionFilterChange).toHaveBeenCalledWith('');
    expect(screen.queryByRole('searchbox')).toBeNull();
  });
});

describe('Sidebar Issues destination', () => {
  it('is absent in local mode, where there is no Space work to receive', () => {
    renderSidebar();
    expect(screen.queryByRole('button', { name: 'Issues' })).toBeNull();
  });

  it('appears when signed in to a server and opens the Issues view', () => {
    const onIssues = vi.fn();
    renderSidebar({ onIssues, view: 'issues' });
    const issues = screen.getByRole('button', { name: 'Issues' });
    expect(issues.getAttribute('aria-current')).toBe('page');
    fireEvent.click(issues);
    expect(onIssues).toHaveBeenCalled();
  });
});
