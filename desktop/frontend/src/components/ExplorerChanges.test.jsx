import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ExplorerChanges } from './ExplorerChanges';

afterEach(cleanup);

function appWith(files) {
  return { GetWorkspaceDiff: () => Promise.resolve({ files }) };
}

describe('ExplorerChanges', () => {
  it('lists changed files and opens a diff on click', async () => {
    const onOpenDiff = vi.fn();
    render(
      <ExplorerChanges
        projectID="p1"
        sessionID=""
        app={appWith([{ path: 'internal/a.go', status: 'M', additions: 3, deletions: 1 }])}
        onOpenDiff={onOpenDiff}
      />,
    );
    const row = await screen.findByTitle('internal/a.go');
    fireEvent.click(row);
    expect(onOpenDiff).toHaveBeenCalledWith('internal/a.go');
  });

  it('shows an empty state when there are no changes', async () => {
    render(<ExplorerChanges projectID="p1" sessionID="" app={appWith([])} onOpenDiff={() => {}} />);
    expect(await screen.findByText('No uncommitted changes.')).toBeTruthy();
  });

  it('degrades when the binding is missing', async () => {
    render(<ExplorerChanges projectID="p1" sessionID="" app={{}} onOpenDiff={() => {}} />);
    expect(await screen.findByText(/Rebuild the desktop app/)).toBeTruthy();
  });
});
