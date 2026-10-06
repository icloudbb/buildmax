import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { LocaleProvider } from '@buildmax/gui';
import { HomeDashboard } from './HomeDashboard';
import { CreateProjectModal } from './Modals';
import { TabBar } from './TabBar';

afterEach(() => {
  cleanup();
  localStorage.removeItem('buildmax_locale');
});

const project = { id: 'p1', name: 'buildmax', default_workspace: '/src/buildmax' };
const untitled = { id: 's1', project_id: 'p1', title: '', created_at: '' };

function renderHome() {
  return (
    <HomeDashboard
      recentSessions={[untitled]}
      recentProjects={[project]}
      projectById={new Map([[project.id, project]])}
      onSelectSession={vi.fn()}
      onOpenProject={vi.fn()}
      onCreateProject={vi.fn()}
    />
  );
}

function inChinese(ui) {
  localStorage.setItem('buildmax_locale', 'zh-CN');
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

describe('the home-to-chat journey in Chinese', () => {
  it('renders the dashboard in English outside a provider', () => {
    render(renderHome());
    expect(screen.getByRole('heading', { name: 'Continue your work' })).toBeTruthy();
    expect(screen.getByText('Chat')).toBeTruthy();
  });

  it('renders the dashboard in the chosen locale', () => {
    inChinese(renderHome());
    expect(screen.getByRole('heading', { name: '继续你的工作' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '最近的对话' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '新建项目' })).toBeTruthy();
    expect(screen.getByText('对话')).toBeTruthy();
  });

  it('renders the create-project dialog in the chosen locale', () => {
    inChinese(<CreateProjectModal app={{}} onCreate={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole('dialog', { name: '新建项目' })).toBeTruthy();
    expect(screen.getByPlaceholderText('我的项目')).toBeTruthy();
    expect(screen.getByRole('button', { name: '创建项目' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '取消' })).toBeTruthy();
  });

  // Default tab titles are saved in English, so translating them is a matter
  // of drawing them, not of what the layout stores.
  it('translates default tab titles where they are drawn', () => {
    const tabs = [
      { key: 'chat:new-1', kind: 'chat', title: 'New Chat' },
      { key: 'terminal:t1', kind: 'terminal', title: 'Terminal 2' },
      { key: 'chat:s2', kind: 'chat', title: 'Fix login redirect' },
    ];
    inChinese(<TabBar tabs={tabs} activeKey="chat:new-1" onSelect={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole('tab', { name: /新对话/ })).toBeTruthy();
    expect(screen.getByRole('tab', { name: /终端 2/ })).toBeTruthy();
    expect(screen.getByRole('tab', { name: /Fix login redirect/ })).toBeTruthy();
  });
});
