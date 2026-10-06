import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { LocaleProvider } from '@buildmax/gui';
import { Explorer } from './Explorer';

afterEach(() => {
  cleanup();
  localStorage.removeItem('buildmax_locale');
});

// A repository with nothing uncommitted and no history yet, so both groups of
// the Changes mode settle on their empty copy.
const app = {
  GetWorkspaceDiff: vi.fn(() => Promise.resolve({ files: [] })),
  ListCommits: vi.fn(() => Promise.resolve({ commits: [], has_more: false, branch: 'main' })),
};

function renderExplorer(mode) {
  return (
    <Explorer
      projectID="p1"
      projectName="buildmax"
      sessionID=""
      app={app}
      mode={mode}
      onModeChange={vi.fn()}
      open
      onToggle={vi.fn()}
      onOpenFile={vi.fn()}
      onOpenDiff={vi.fn()}
      onOpenCommitDiff={vi.fn()}
    />
  );
}

describe('the Explorer in Chinese', () => {
  it('renders the Changes mode in the chosen locale', async () => {
    localStorage.setItem('buildmax_locale', 'zh-CN');
    render(<LocaleProvider>{renderExplorer('changes')}</LocaleProvider>);

    expect(screen.getByRole('region', { name: 'buildmax 工作区' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '文件' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '变更', pressed: true })).toBeTruthy();
    expect(await screen.findByText('没有未提交的变更。')).toBeTruthy();
    expect(await screen.findByText('还没有提交。')).toBeTruthy();
    expect(screen.getByTitle('当前分支：main')).toBeTruthy();
  });

  it('keeps English outside a provider', async () => {
    render(renderExplorer('changes'));
    expect(screen.getByRole('region', { name: 'buildmax workspace' })).toBeTruthy();
    expect(await screen.findByText('No uncommitted changes.')).toBeTruthy();
    expect(await screen.findByText('No commits yet.')).toBeTruthy();
  });
});
