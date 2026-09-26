import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { DiffView } from './DiffView';

// Rendering a patch is FilePatch's job; this checks which diff DiffView reads.
vi.mock('./FilePatch', () => ({ FilePatch: ({ file }) => <pre>{file.patch}</pre> }));

afterEach(cleanup);

describe('DiffView', () => {
  it('reads a commit file diff when the tab carries a commit', async () => {
    const app = {
      GetWorkspaceDiff: vi.fn(),
      GetCommitFileDiff: vi.fn(() => Promise.resolve({
        path: 'a.go', status: 'modified', additions: 1, deletions: 0,
        patch: 'diff --git a/a.go b/a.go\n@@ -1 +1,2 @@\n one\n+two\n',
      })),
    };
    render(<DiffView projectID="p1" sessionID="s1" path="a.go" commit="abc1234" app={app} />);
    expect(await screen.findByText(/\+two/)).toBeTruthy();
    expect(app.GetCommitFileDiff).toHaveBeenCalledWith('p1', 's1', 'abc1234', 'a.go');
    expect(app.GetWorkspaceDiff).not.toHaveBeenCalled();
  });
});
