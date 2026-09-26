import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { ApprovalPanel } from './ApprovalPanel';

afterEach(cleanup);

describe('ApprovalPanel', () => {
  // A browser session grant covers one origin; the prompt must say so rather
  // than read as consent to every later navigation.
  it('names the target a session grant covers', () => {
    render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          session_id: 's1',
          tool_name: 'BrowserNavigate',
          args: { url: 'http://localhost:3000/login' },
          target: 'http://localhost:3000',
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText(/Allow session covers only/)).toBeTruthy();
    expect(screen.getByText('http://localhost:3000')).toBeTruthy();
  });

  it('adds no target line for a tool-wide grant', () => {
    render(
      <ApprovalPanel
        request={{ tool_name: 'Write', args: { file_path: 'a.go' } }}
        onRespond={() => {}}
      />,
    );
    expect(screen.queryByText(/Allow session covers only/)).toBeNull();
  });
});
