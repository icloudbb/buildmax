import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { LocaleProvider, ThemeProvider } from '@buildmax/gui';
import LoginPage from './LoginPage';

// The server this machine was configured with, as GetDefaultServerURL answers:
// empty when none was.
let configuredServer = '';
vi.mock('./lib/app', () => ({
  getApp: () => ({ GetDefaultServerURL: () => Promise.resolve(configuredServer) }),
}));

afterEach(() => {
  cleanup();
  configuredServer = '';
  localStorage.removeItem('buildmax_locale');
  localStorage.removeItem('buildmax_theme');
});

function renderPage(props = {}) {
  return render(
    <LocaleProvider>
      <ThemeProvider>
        <LoginPage {...props} />
      </ThemeProvider>
    </LocaleProvider>,
  );
}

describe('LoginPage', () => {
  it('leads with a password, as Portal does, and offers a login code for a new account or a lost password', () => {
    renderPage();
    expect(screen.getByLabelText('Password')).toBeTruthy();
    expect(screen.queryByLabelText('Login code')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Forgot your password, or have a login code?' }));
    expect(screen.getByLabelText('Login code')).toBeTruthy();
    expect(screen.getByText(/claims a new account or recovers a forgotten password/)).toBeTruthy();
    expect(screen.queryByLabelText('Password')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Sign in with a password' }));
    expect(screen.getByLabelText('Password')).toBeTruthy();
  });

  it('describes the server in a person\'s terms and does not guess an address', async () => {
    const { container } = renderPage();
    await Promise.resolve();
    expect(screen.getByLabelText('Server address').value).toBe('');
    expect(container.textContent).toMatch(/open Portal at/);
    expect(container.textContent).not.toMatch(/settings\.yaml|ingress|localhost/i);
  });

  it('starts from the server this machine was configured with', async () => {
    configuredServer = 'https://buildmax.corp.example';
    renderPage();
    await waitFor(() => expect(screen.getByLabelText('Server address').value).toBe('https://buildmax.corp.example'));
  });

  it('switches language and theme in place', () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: '简体中文' }));
    expect(screen.getByLabelText('服务器地址')).toBeTruthy();
    expect(screen.getByLabelText('密码')).toBeTruthy();
    expect(screen.getByRole('button', { name: '忘记密码，或已有登录码？' })).toBeTruthy();

    const before = document.documentElement.dataset.theme;
    fireEvent.click(screen.getByRole('button', { name: /切换到(深色|浅色)模式/ }));
    expect(document.documentElement.dataset.theme).not.toBe(before);
  });
});

describe('LoginPage after an ended login', () => {
  it('offers the server the login was on, not the configured default', async () => {
    configuredServer = 'http://settings-default:5678';
    renderPage({ expiredDetail: 'login has expired', knownServerURL: 'https://buildmax.example.com' });
    // Let the default lookup settle; it must not overwrite the known server.
    await Promise.resolve();
    expect(screen.getByLabelText('Server address').value).toBe('https://buildmax.example.com');
  });

  it('says an administrator disabled the account rather than that the session ended', () => {
    renderPage({ expiredDetail: 'account is disabled', accountDisabled: true, knownServerURL: 'https://buildmax.example.com' });
    expect(screen.getByRole('alert').textContent).toMatch(/disabled your account/);
  });
});

describe('LoginPage in Chinese', () => {
  it('renders the sign-in form in the chosen locale', () => {
    localStorage.setItem('buildmax_locale', 'zh-CN');
    renderPage({ expiredDetail: 'account is disabled', accountDisabled: true, knownServerURL: 'https://buildmax.example.com' });
    expect(screen.getByRole('alert').textContent).toMatch(/管理员停用了你的账户/);
    expect(screen.getByLabelText('服务器地址')).toBeTruthy();
    expect(screen.getByLabelText('密码')).toBeTruthy();
    expect(screen.getByRole('button', { name: '登录' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '退出登录，仅在本机使用' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '忘记密码，或已有登录码？' }));
    expect(screen.getByLabelText('登录码')).toBeTruthy();
    expect(screen.getByText('使用管理员提供的登录码登录。')).toBeTruthy();
  });
});
