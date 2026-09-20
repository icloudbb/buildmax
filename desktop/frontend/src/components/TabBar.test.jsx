import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { TabBar } from './TabBar';

afterEach(cleanup);

const tabs = [
  { key: 'chat:s1', kind: 'chat', title: 'New Chat' },
  { key: 'terminal:t1', kind: 'terminal', title: 'Terminal 1' },
  { key: 'file:a.go', kind: 'file', title: 'a.go', preview: true },
];

describe('TabBar', () => {
  it('renders one tab per entry with the active one selected', () => {
    render(<TabBar tabs={tabs} activeKey="terminal:t1" onSelect={() => {}} onClose={() => {}} />);
    expect(screen.getAllByRole('tab')).toHaveLength(3);
    expect(screen.getByRole('tab', { selected: true }).textContent).toContain('Terminal 1');
  });

  it('reports the clicked tab', () => {
    const onSelect = vi.fn();
    render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={onSelect} onClose={() => {}} />);
    fireEvent.click(screen.getByRole('tab', { name: /a\.go/ }));
    expect(onSelect).toHaveBeenCalledWith('file:a.go');
  });

  it('reports a close request without selecting', () => {
    const onSelect = vi.fn();
    const onClose = vi.fn();
    render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={onSelect} onClose={onClose} />);
    fireEvent.click(screen.getByLabelText('Close Terminal 1'));
    expect(onClose).toHaveBeenCalledWith('terminal:t1');
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('omits the close button on a non-closable tab', () => {
    const pinned = [{ key: 'chat:current', kind: 'chat', title: 'New Chat', closable: false }];
    render(<TabBar tabs={pinned} activeKey="chat:current" onSelect={() => {}} onClose={() => {}} />);
    expect(screen.queryByLabelText('Close New Chat')).toBeNull();
  });

  it('pins a tab on double-click', () => {
    const onPin = vi.fn();
    render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={() => {}} onClose={() => {}} onPin={onPin} />);
    fireEvent.doubleClick(screen.getByRole('tab', { name: /a\.go/ }));
    expect(onPin).toHaveBeenCalledWith('file:a.go');
  });

  it('renders nothing when there are no tabs', () => {
    const { container } = render(<TabBar tabs={[]} activeKey={null} onSelect={() => {}} onClose={() => {}} />);
    expect(container.firstChild).toBeNull();
  });

  describe('context menu', () => {
    const openMenu = (name) => fireEvent.contextMenu(screen.getByRole('tab', { name }).closest('.workspace-tabs__tab'));

    it('offers bulk closes and reports them for the right tab', () => {
      const onCloseOthers = vi.fn();
      const onCloseRight = vi.fn();
      render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={() => {}} onClose={() => {}} onCloseOthers={onCloseOthers} onCloseRight={onCloseRight} />);
      openMenu(/Terminal 1/);
      fireEvent.click(screen.getByText('Close others'));
      expect(onCloseOthers).toHaveBeenCalledWith('terminal:t1');
    });

    it('shows copy-path only for a file or diff tab', () => {
      render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={() => {}} onClose={() => {}} onCopyPath={() => {}} />);
      openMenu(/New Chat/);
      expect(screen.queryByText('Copy relative path')).toBeNull();
      openMenu(/a\.go/);
      expect(screen.getByText('Copy relative path')).toBeTruthy();
      expect(screen.getByText('Copy absolute path')).toBeTruthy();
    });

    it('copies the relative and absolute paths for a file tab', () => {
      const onCopyPath = vi.fn();
      render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={() => {}} onClose={() => {}} onCopyPath={onCopyPath} />);
      openMenu(/a\.go/);
      fireEvent.click(screen.getByText('Copy relative path'));
      expect(onCopyPath).toHaveBeenCalledWith('file:a.go', false);
      openMenu(/a\.go/);
      fireEvent.click(screen.getByText('Copy absolute path'));
      expect(onCopyPath).toHaveBeenCalledWith('file:a.go', true);
    });

    it('reports the tab to drop before when a drag lands on its left half', () => {
      const onTabDrop = vi.fn();
      render(<TabBar tabs={tabs} activeKey="chat:s1" onSelect={() => {}} onClose={() => {}} onTabDrop={onTabDrop} onTabDragStart={() => {}} />);
      const target = screen.getByRole('tab', { name: /a\.go/ }).closest('.workspace-tabs__tab');
      // A real rect so the left/right split is meaningful; the drop at clientX 10
      // is on the left half, so the tab drops before a.go itself.
      target.getBoundingClientRect = () => ({ left: 0, top: 0, width: 100, height: 20, right: 100, bottom: 20 });
      fireEvent.drop(target, { clientX: 10 });
      expect(onTabDrop).toHaveBeenCalledWith('file:a.go');
    });
  });
});
