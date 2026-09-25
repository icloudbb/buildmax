import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import LoginPage from './LoginPage';

// A settings.yaml default that is not the server the ended login was on.
vi.mock('./lib/app', () => ({
  getApp: () => ({ GetDefaultServerURL: () => Promise.resolve('http://settings-default:5678') }),
}));

afterEach(cleanup);

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
