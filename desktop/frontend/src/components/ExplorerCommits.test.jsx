import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ExplorerCommits } from './ExplorerCommits';

afterEach(cleanup);

const commit = (sha, subject, parents = ['p']) => ({
  sha, subject, parents, author: 'Ada', authored_at: '2026-09-26T08:00:00Z',
});

describe('ExplorerCommits', () => {
  it('lists the history with its branch and opens a commit file diff', async () => {
    const onOpenCommitDiff = vi.fn();
    const app = {
      ListCommits: vi.fn(() => Promise.resolve({
        branch: 'main',
        commits: [commit('aaaaaaaa1', 'Merge pull request #1', ['p1', 'p2']), commit('bbbbbbbb2', 'Fix the login redirect')],
      })),
      GetCommit: vi.fn(() => Promise.resolve({
        files: [{ path: 'internal/auth.go', status: 'modified', additions: 2, deletions: 1 }],
      })),
    };
    render(<ExplorerCommits projectID="p1" sessionID="s1" app={app} onOpenCommitDiff={onOpenCommitDiff} />);

    expect(await screen.findByText('main')).toBeTruthy();
    const merge = screen.getByRole('button', { name: /Merge pull request #1/ });
    expect(merge.className).toContain('explorer__commit--merge');

    const row = screen.getByRole('button', { name: /Fix the login redirect/ });
    fireEvent.click(row);
    expect(row.getAttribute('aria-expanded')).toBe('true');
    expect(app.GetCommit).toHaveBeenCalledWith('p1', 's1', 'bbbbbbbb2');

    fireEvent.click(await screen.findByTitle('internal/auth.go'));
    expect(onOpenCommitDiff).toHaveBeenCalledWith('bbbbbbbb2', 'internal/auth.go', undefined);
  });

  it('loads older commits from where the list ends', async () => {
    const app = {
      ListCommits: vi.fn()
        .mockResolvedValueOnce({ commits: [commit('c1', 'Newest')], has_more: true })
        .mockResolvedValueOnce({ commits: [commit('c2', 'Older')], has_more: false }),
    };
    render(<ExplorerCommits projectID="p1" sessionID="" app={app} onOpenCommitDiff={() => {}} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Load more' }));
    expect(await screen.findByText('Older')).toBeTruthy();
    expect(app.ListCommits).toHaveBeenLastCalledWith('p1', '', 1, 50);
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('shows an empty history as such', async () => {
    const app = { ListCommits: () => Promise.resolve({ commits: [] }) };
    render(<ExplorerCommits projectID="p1" sessionID="" app={app} onOpenCommitDiff={() => {}} />);
    expect(await screen.findByText('No commits yet.')).toBeTruthy();
  });
});
