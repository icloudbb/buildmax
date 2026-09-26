import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { IssuesView } from './IssuesView';

afterEach(cleanup);

const newer = { id: 'i_new', space_id: 's_b', space_name: 'Beta', title: 'Fix importer', status: 'in_progress', version: 5, updated_at: '2026-09-20T00:00:00Z' };
const older = { id: 'i_old', space_id: 's_a', space_name: 'Alpha', title: 'Tidy docs', status: 'todo', version: 2, updated_at: '2026-09-01T00:00:00Z' };

function detailOf(issue) {
  return {
    issue,
    description: 'Quoted CSV rows break.',
    children: [{ id: 'i_c', title: 'Add a test', status: 'done' }],
    comments: [{ author_kind: 'user', mine: true, body: 'Looking at it', created_at: '2026-09-21T00:00:00Z' }],
    omitted_comments: 0,
  };
}

function makeApp(overrides = {}) {
  return {
    ListMyIssues: vi.fn(() => Promise.resolve({ issues: [newer, older], warnings: [] })),
    GetIssueDetail: vi.fn(() => Promise.resolve(detailOf(newer))),
    SetIssueStatus: vi.fn(() => Promise.resolve({ issue: { ...newer, status: 'done', version: 6 }, conflict: false })),
    CommentOnIssue: vi.fn(() => Promise.resolve()),
    ...overrides,
  };
}

const projects = [{ id: 'p1', name: 'importer' }, { id: 'p2', name: 'docs' }];

function renderView(app = makeApp(), onStartChat = vi.fn()) {
  render(<IssuesView app={app} projects={projects} currentProject={null} onStartChat={onStartChat} />);
  return { app, onStartChat };
}

async function openNewer() {
  fireEvent.click(await screen.findByRole('button', { name: /Fix importer/ }));
  await screen.findByRole('heading', { name: 'Fix importer' });
}

describe('IssuesView', () => {
  it('lists open work grouped by status and says when a space could not be read', async () => {
    renderView(makeApp({
      ListMyIssues: vi.fn(() => Promise.resolve({ issues: [newer, older], warnings: ['space Gamma: boom'] })),
    }));
    const list = await screen.findByRole('region', { name: 'Your issues' });
    expect(within(list).getByRole('heading', { name: /In progress/ })).toBeTruthy();
    expect(within(list).getByRole('heading', { name: /To do/ })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('space Gamma');
  });

  it('moves status with the version it read and reloads after a conflict', async () => {
    const app = makeApp({ SetIssueStatus: vi.fn(() => Promise.resolve({ conflict: true })) });
    renderView(app);
    await openNewer();
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    await screen.findByText(/changed since it was loaded/);
    expect(app.SetIssueStatus).toHaveBeenCalledWith('s_b', 'Beta', 'i_new', 'done', 5);
    await waitFor(() => expect(app.GetIssueDetail).toHaveBeenCalledTimes(2));
  });

  it('posts a comment as the person and re-reads the thread', async () => {
    const app = makeApp();
    renderView(app);
    await openNewer();
    fireEvent.change(screen.getByLabelText('Add a comment'), { target: { value: 'Fixed in the importer PR' } });
    fireEvent.click(screen.getByRole('button', { name: 'Post comment' }));
    await waitFor(() => expect(app.CommentOnIssue).toHaveBeenCalledWith('s_b', 'i_new', 'Fixed in the importer PR'));
    await waitFor(() => expect(app.GetIssueDetail).toHaveBeenCalledTimes(2));
  });

  it('starts a chat in the chosen project with the issue as an editable draft', async () => {
    const { onStartChat } = renderView();
    await openNewer();
    fireEvent.change(screen.getByLabelText('Project'), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: 'Start chat' }));
    expect(onStartChat).toHaveBeenCalledTimes(1);
    const [project, text] = onStartChat.mock.calls[0];
    expect(project.id).toBe('p2');
    expect(text).toContain('Issue i_new: Fix importer');
    expect(text).toContain('Quoted CSV rows break.');
  });

  it('reports an inbox that could not load rather than an empty one', async () => {
    renderView(makeApp({ ListMyIssues: vi.fn(() => Promise.reject(new Error('server down'))) }));
    expect((await screen.findByRole('alert')).textContent).toContain('server down');
    expect(screen.queryByText('No open issues are assigned to you.')).toBeNull();
  });
});
