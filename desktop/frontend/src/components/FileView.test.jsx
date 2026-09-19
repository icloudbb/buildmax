import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@buildmax/gui';
import { FileView } from './FileView';

// Highlighting loads shiki lazily; stub it so tests do not pull the grammars.
vi.mock('../lib/highlight', () => ({ highlightToHtml: () => Promise.resolve(null) }));

afterEach(cleanup);

function appWith(readRes, write) {
  return {
    ReadWorkspaceFile: () => Promise.resolve(readRes),
    WriteWorkspaceFile: write,
  };
}

function renderView(app, path = 'a.txt') {
  return render(
    <ThemeProvider>
      <FileView projectID="p" sessionID="" path={path} app={app} />
    </ThemeProvider>,
  );
}

describe('FileView editing', () => {
  it('edits and saves a file through WriteWorkspaceFile', async () => {
    const write = vi.fn((_p, _s, _path, content) => Promise.resolve({ content, error: null }));
    renderView(appWith({ content: 'hello' }, write));

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByLabelText('Edit a.txt'), { target: { value: 'hello world' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(write).toHaveBeenCalledWith('p', '', 'a.txt', 'hello world'));
    // Leaves edit mode and shows the saved content.
    await waitFor(() => expect(screen.queryByLabelText('Edit a.txt')).toBeNull());
  });

  it('surfaces a save error and stays in edit mode', async () => {
    const write = vi.fn(() => Promise.resolve({ error: 'permission denied' }));
    renderView(appWith({ content: 'hi' }, write));

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText('permission denied')).toBeTruthy();
    expect(screen.getByLabelText('Edit a.txt')).toBeTruthy();
  });

  it('offers no Edit for a truncated file', async () => {
    renderView(appWith({ content: 'x', truncated: true }, vi.fn()));
    await screen.findByText('a.txt');
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });

  it('offers no Edit when the write binding is missing', async () => {
    renderView({ ReadWorkspaceFile: () => Promise.resolve({ content: 'x' }) });
    await screen.findByText('a.txt');
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });
});
