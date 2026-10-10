import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ApprovalPanel } from './ApprovalPanel';

afterEach(cleanup);

const MAIN_GO = 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("hello")\n}\n';

// The rows of the rendered diff as "<marker><text>", whitespace intact.
function diffRows(container) {
  return [...container.querySelectorAll('.line-diff__row')].map((row) =>
    `${row.querySelector('.line-diff__marker')?.textContent ?? ''}${row.querySelector('.line-diff__text').textContent}`);
}

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

  // The audit's Edit: two indented lines replace one. The prompt shows them
  // as an added hunk against the current file, indentation included.
  it('shows an Edit as a diff against the current file', () => {
    const { container } = render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          tool_name: 'Edit',
          args: {
            file_path: 'src/main.go',
            old_string: '\tfmt.Println("hello")\n',
            new_string: '\tfmt.Println("hello")\n\tfmt.Println("goodbye")\n',
          },
          file: { path: 'src/main.go', exists: true, content: MAIN_GO },
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText('Edits src/main.go:')).toBeTruthy();
    const rows = diffRows(container);
    expect(rows).toContain('+\tfmt.Println("goodbye")');
    expect(rows).toContain(' \tfmt.Println("hello")');
    expect(rows.filter((r) => r.startsWith('+') || r.startsWith('−'))).toEqual(['+\tfmt.Println("goodbye")']);
    expect(container.querySelector('.approval-panel__args')).toBeNull();
  });

  it('says a Write empties a file and shows the lines it removes', () => {
    const { container } = render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          tool_name: 'Write',
          args: { file_path: 'NOTES.txt', content: '' },
          file: { path: 'NOTES.txt', exists: true, content: 'TODO: say goodbye\n  keep this indent\n' },
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText(/Empties NOTES.txt/)).toBeTruthy();
    expect(diffRows(container)).toEqual(['−TODO: say goodbye', '−  keep this indent']);
  });

  it('says a Write creates a new file', () => {
    const { container } = render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          tool_name: 'Write',
          args: { file_path: 'docs/new.md', content: '# Title\n\n- item\n' },
          file: { path: 'docs/new.md', exists: false, content: '' },
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText(/Creates docs\/new.md, a new file/)).toBeTruthy();
    expect(diffRows(container)).toEqual(['+# Title', '+ ', '+- item']);
  });

  it('says a Write overwrites a file and diffs the whole content', () => {
    const { container } = render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          tool_name: 'Write',
          args: { file_path: 'a.txt', content: 'one\nTWO\nthree\n' },
          file: { path: 'a.txt', exists: true, content: 'one\ntwo\nthree\n' },
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText(/Overwrites a.txt/)).toBeTruthy();
    expect(diffRows(container)).toEqual([' one', '−two', '+TWO', ' three']);
  });

  it('says why the current content is missing and still shows the requested change', () => {
    const { container } = render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          tool_name: 'Edit',
          args: { file_path: 'link/secret', old_string: 'a', new_string: 'b' },
          file: { path: 'link/secret', exists: false, content: '', unavailable: 'outside_root' },
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText(/leads outside the project folder/)).toBeTruthy();
    expect(diffRows(container)).toEqual(['−a', '+b']);
    expect(container.querySelector('.line-diff__num')).toBeNull();
  });

  it('warns that an Edit whose text is not in the file will fail', () => {
    render(
      <ApprovalPanel
        request={{
          approval_id: '1',
          tool_name: 'Edit',
          args: { file_path: 'src/main.go', old_string: 'nope', new_string: 'x' },
          file: { path: 'src/main.go', exists: true, content: MAIN_GO },
        }}
        onRespond={() => {}}
      />,
    );
    expect(screen.getByText(/This edit will fail: the text to replace is not in src\/main.go/)).toBeTruthy();
  });

  it('keeps the whitespace of other tools\' arguments', () => {
    const command = 'cd src &&\n  go test ./...';
    const { container } = render(
      <ApprovalPanel request={{ approval_id: '1', tool_name: 'Bash', args: { command } }} onRespond={() => {}} />,
    );
    expect(container.querySelector('.approval-panel__arg-val pre').textContent).toBe(command);
  });

  // D5: a session grant on one prompt must not make the next prompt's default
  // the session grant, even with the pointer left resting where that button was.
  it('defaults every approval to allow once, whatever the last one answered', () => {
    const onRespond = vi.fn();
    const first = { approval_id: '1', tool_name: 'Write', args: { file_path: 'a.txt', content: 'a' } };
    const second = { approval_id: '2', tool_name: 'Write', args: { file_path: 'b.txt', content: 'b' } };

    const { rerender } = render(<ApprovalPanel key={first.approval_id} request={first} onRespond={onRespond} />);
    const session = screen.getByRole('button', { name: /Allow session/ });
    fireEvent.mouseEnter(session);
    fireEvent.click(session);
    expect(onRespond).toHaveBeenLastCalledWith('session');

    rerender(<ApprovalPanel key={second.approval_id} request={second} onRespond={onRespond} />);
    fireEvent.mouseEnter(screen.getByRole('button', { name: /Allow session/ }));
    expect(screen.getByRole('button', { name: /Allow once/ }).className).toContain('approval-panel__btn--allow');
    expect(screen.getByRole('button', { name: /Allow session/ }).className).toContain('approval-panel__btn--muted');
    fireEvent.keyDown(window, { key: 'Enter' });
    expect(onRespond).toHaveBeenLastCalledWith('once');
  });
});
