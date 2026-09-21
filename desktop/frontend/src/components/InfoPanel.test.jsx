import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { InfoPanel } from './InfoPanel';

describe('InfoPanel fork tree', () => {
  it('shows the current branch and a deleted source from the session index', async () => {
    const app = {
      GetSlashInfo: vi.fn().mockResolvedValue({
        prompt_tokens: 0,
        completion_tokens: 0,
        compactions: 0,
        text_bytes: 0,
        tool_result_bytes: 0,
        user_messages: 0,
        assistant_turns: 0,
        tool_calls: 0,
        tools: [],
      }),
      GetSessionForkTree: vi.fn().mockResolvedValue({
        tree: {
          id: 'deleted-parent',
          missing: true,
          children: [
            { id: 'child-a', title: 'First approach' },
            { id: 'child-b', title: 'Second approach', current: true },
          ],
        },
      }),
    };

    render(<InfoPanel projectID="p1" sessionID="child-b" app={app} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Tree' }));

    expect(await screen.findByText('Source session deleted')).toBeTruthy();
    expect(screen.getByText('First approach')).toBeTruthy();
    expect(screen.getByText('Second approach')).toBeTruthy();
    expect(screen.getByText('current')).toBeTruthy();
    expect(app.GetSessionForkTree).toHaveBeenCalledWith('p1', 'child-b');
  });
});
