import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { LocaleProvider } from '@buildmax/gui';
import LoginPage from './LoginPage';

// A settings.yaml default that is not the server the ended login was on.
vi.mock('./lib/app', () => ({
  getApp: () => ({ GetDefaultServerURL: () => Promise.resolve('http://settings-default:5678') }),
}));

afterEach(() => {
  cleanup();
  localStorage.removeItem('buildmax_locale');
});

describe('LoginPage after an ended login', () => {
  it('offers the server the login was on, not the settings default', async () => {
    render(<LoginPage expiredDetail="login has expired" knownServerURL="https://buildmax.example.com" />);
    // Let the default lookup settle; it must not overwrite the known server.
    await Promise.resolve();
    expect(screen.getByLabelText('Server URL').value).toBe('https://buildmax.example.com');
  });

  it('says an administrator disabled the account rather than that the session ended', () => {
    render(<LoginPage expiredDetail="account is disabled" accountDisabled knownServerURL="https://buildmax.example.com" />);
    expect(screen.getByRole('alert').textContent).toMatch(/disabled your account/);
  });
});

describe('LoginPage in Chinese', () => {
  it('renders the sign-in form in the chosen locale', () => {
    localStorage.setItem('buildmax_locale', 'zh-CN');
    render(<LocaleProvider><LoginPage expiredDetail="account is disabled" accountDisabled knownServerURL="https://buildmax.example.com" /></LocaleProvider>);
    expect(screen.getByRole('alert').textContent).toMatch(/管理员停用了你的账户/);
    expect(screen.getByLabelText('服务器 URL')).toBeTruthy();
    expect(screen.getByLabelText('密码')).toBeTruthy();
    expect(screen.getByRole('button', { name: '登录' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '退出登录，仅在本机使用' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '忘记密码，或已有登录码？' }));
    expect(screen.getByLabelText('登录码')).toBeTruthy();
    expect(screen.getByText('使用管理员提供的登录码登录')).toBeTruthy();
  });
});
