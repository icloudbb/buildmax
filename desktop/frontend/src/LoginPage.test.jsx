import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { LocaleProvider, ThemeProvider } from '@buildmax/gui';
import LoginPage from './LoginPage';

// The server this machine was configured with, as GetDefaultServerURL answers:
// empty when none was.
let configuredServer = '';
// Further bound methods a test adds: the server's sign-in methods and the
// browser sign-in.
let bridge = {};
vi.mock('./lib/app', () => ({
  getApp: () => ({ GetDefaultServerURL: () => Promise.resolve(configuredServer), ...bridge }),
}));

// The Go side's events, delivered by the test.
const listeners = {};
vi.mock('./lib/wailsRuntime', () => ({
  EventsOn: (name, callback) => {
    listeners[name] = callback;
    return () => {
      if (listeners[name] === callback) delete listeners[name];
    };
  },
}));

afterEach(() => {
  cleanup();
  configuredServer = '';
  bridge = {};
  for (const name of Object.keys(listeners)) delete listeners[name];
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
  it('leads with a password, as Portal does, and offers a login code for a new account or a lost password', async () => {
    serverWith({ local_login: 'all', oidc: { enabled: false } });
    renderPage();
    expect(await screen.findByLabelText('Password')).toBeTruthy();
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

  it('offers no way in until the server says how its accounts sign in', async () => {
    let answer;
    bridge = { GetAuthMethods: vi.fn(() => new Promise((resolve) => { answer = resolve; })) };
    renderPage();
    expect(screen.getByRole('button', { name: 'Continue' }).disabled).toBe(true);
    expect(screen.queryByLabelText('Password')).toBeNull();

    const field = screen.getByLabelText('Server address');
    fireEvent.change(field, { target: { value: 'https://sso.example' } });
    fireEvent.submit(field.closest('form'));
    expect(screen.getByRole('button', { name: 'Checking how this server signs you in…' }).disabled).toBe(true);
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(screen.queryByRole('button', { name: /browser/ })).toBeNull();

    await act(async () => answer({ local_login: 'off', oidc: { enabled: true, display_name: 'Okta' } }));
    expect(screen.getByRole('button', { name: 'Continue with Okta in your browser' })).toBeTruthy();
    expect(screen.queryByLabelText('Password')).toBeNull();
  });

  it('starts from the server this machine was configured with', async () => {
    configuredServer = 'https://buildmax.corp.example';
    renderPage();
    await waitFor(() => expect(screen.getByLabelText('Server address').value).toBe('https://buildmax.corp.example'));
  });

  it('switches language and theme in place', async () => {
    serverWith({ local_login: 'all', oidc: { enabled: false } });
    renderPage();
    await screen.findByLabelText('Password');
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
  it('renders the sign-in form in the chosen locale', async () => {
    localStorage.setItem('buildmax_locale', 'zh-CN');
    bridge = { GetAuthMethods: vi.fn(() => Promise.resolve({ local_login: 'all', oidc: { enabled: false } })) };
    renderPage({ expiredDetail: 'account is disabled', accountDisabled: true, knownServerURL: 'https://buildmax.example.com' });
    expect(screen.getByRole('alert').textContent).toMatch(/管理员停用了你的账户/);
    expect(screen.getByLabelText('服务器地址')).toBeTruthy();
    expect(await screen.findByLabelText('密码')).toBeTruthy();
    expect(screen.getByRole('button', { name: '登录' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '退出登录，仅在本机使用' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '忘记密码，或已有登录码？' }));
    expect(screen.getByLabelText('登录码')).toBeTruthy();
    expect(screen.getByText('使用管理员提供的登录码登录。')).toBeTruthy();
  });
});

// A server at this address, answering GET /api/auth/methods with `methods`,
// and a browser sign-in that starts with code WDJB-MJHT.
function serverWith(methods) {
  configuredServer = 'https://buildmax.corp.example';
  bridge = {
    GetAuthMethods: vi.fn(() => Promise.resolve(methods)),
    StartBrowserSignIn: vi.fn(() => Promise.resolve({
      user_code: 'WDJB-MJHT',
      verification_uri: 'https://buildmax.corp.example/#/sign-in',
      verification_uri_complete: 'https://buildmax.corp.example/#/sign-in/WDJB-MJHT',
      expires_in: 600,
    })),
    OpenBrowserSignInAgain: vi.fn(() => Promise.resolve()),
    CopyBrowserSignInLink: vi.fn(() => Promise.resolve()),
    CancelBrowserSignIn: vi.fn(() => Promise.resolve()),
  };
}

const sso = { enabled: true, display_name: 'Okta' };

describe('LoginPage laid out by the server\'s methods', () => {
  it('local_login all: the password form leads, with the browser below', async () => {
    serverWith({ local_login: 'all', oidc: { enabled: false } });
    renderPage();
    await waitFor(() => expect(bridge.GetAuthMethods).toHaveBeenCalledWith('https://buildmax.corp.example'));
    const browser = await screen.findByRole('button', { name: 'Sign in with your browser' });
    const password = screen.getByLabelText('Password');
    expect(password.compareDocumentPosition(browser) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Forgot your password, or have a login code?' })).toBeTruthy();
  });

  it('local_login system_admins: the browser leads and the password waits for an administrator', async () => {
    serverWith({ local_login: 'system_admins', oidc: sso });
    renderPage();
    const browser = await screen.findByRole('button', { name: 'Continue with Okta in your browser' });
    expect(browser.className).toBe('login-page__submit');
    expect(screen.queryByLabelText('Password')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Administrator sign-in with a password' }));
    expect(screen.getByLabelText('Password')).toBeTruthy();
  });

  it('local_login off: the browser is the only way in', async () => {
    serverWith({ local_login: 'off', oidc: { enabled: false } });
    renderPage();
    await screen.findByRole('button', { name: 'Sign in with your browser' });
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(screen.queryByLabelText('Email')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Administrator sign-in with a password' })).toBeNull();
  });

  it('shows the server as unreachable rather than guessing which methods to offer', async () => {
    serverWith(null);
    let down = true;
    bridge.GetAuthMethods = vi.fn(() => (down
      ? Promise.reject(new Error('request failed: connection refused'))
      : Promise.resolve({ local_login: 'all', oidc: { enabled: false } })));
    renderPage();
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toMatch(/Cannot reach https:\/\/buildmax\.corp\.example/);
    expect(alert.textContent).toMatch(/connection refused/);
    expect(screen.queryByLabelText('Password')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Sign in with your browser' })).toBeNull();

    down = false;
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByLabelText('Password')).toBeTruthy();
  });

  it('reads the methods of an address typed in once the field is left', async () => {
    bridge = { GetAuthMethods: vi.fn(() => Promise.resolve({ local_login: 'off', oidc: sso })) };
    renderPage();
    const field = screen.getByLabelText('Server address');
    fireEvent.change(field, { target: { value: ' https://typed.example ' } });
    fireEvent.blur(field);
    await screen.findByRole('button', { name: 'Continue with Okta in your browser' });
    expect(bridge.GetAuthMethods).toHaveBeenCalledWith('https://typed.example');
  });
});

describe('LoginPage waiting for a browser sign-in', () => {
  async function startWaiting(props = {}) {
    serverWith({ local_login: 'off', oidc: sso });
    renderPage(props);
    fireEvent.click(await screen.findByRole('button', { name: 'Continue with Okta in your browser' }));
    await screen.findByText('WDJB-MJHT');
    expect(bridge.StartBrowserSignIn).toHaveBeenCalledWith('https://buildmax.corp.example');
  }

  it('shows the code and its actions, and Cancel returns to the form', async () => {
    await startWaiting();
    expect(screen.getByText(/Confirm this sign-in in the browser window that opened/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Open browser again' }));
    expect(bridge.OpenBrowserSignInAgain).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Copy link' }));
    expect(await screen.findByText(/Link copied/)).toBeTruthy();
    expect(bridge.CopyBrowserSignInLink).toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(bridge.CancelBrowserSignIn).toHaveBeenCalled();
    expect(screen.queryByText('WDJB-MJHT')).toBeNull();
    expect(screen.getByRole('button', { name: 'Continue with Okta in your browser' })).toBeTruthy();
  });

  it('signs in when the browser confirms', async () => {
    const onLogin = vi.fn();
    await startWaiting({ onLogin });
    const status = { logged_in: true, server_url: 'https://buildmax.corp.example', email: 'alice@example.com' };
    act(() => listeners['desktop/browser-sign-in']({ outcome: 'signed_in', status }));
    expect(onLogin).toHaveBeenCalledWith(status);
  });

  it('returns to the form saying the sign-in was denied', async () => {
    await startWaiting();
    act(() => listeners['desktop/browser-sign-in']({ outcome: 'denied' }));
    expect(screen.queryByText('WDJB-MJHT')).toBeNull();
    expect(screen.getByRole('alert').textContent).toBe('Sign-in was denied in the browser.');
  });

  it('says an expired request expired, in Chinese too', async () => {
    localStorage.setItem('buildmax_locale', 'zh-CN');
    serverWith({ local_login: 'off', oidc: sso });
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '在浏览器中通过 Okta 继续' }));
    await screen.findByText('WDJB-MJHT');
    expect(screen.getByText('请在已打开的浏览器窗口中确认此次登录，并核对页面显示的代码与此一致。')).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新打开浏览器' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '复制链接' })).toBeTruthy();

    act(() => listeners['desktop/browser-sign-in']({ outcome: 'expired' }));
    expect(screen.getByRole('alert').textContent).toBe('登录请求已过期，请重新开始。');
    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    expect(screen.getByRole('alert').textContent).toBe('The sign-in request expired. Start again.');
  });

  it('stops waiting when the page goes away', async () => {
    await startWaiting();
    cleanup();
    expect(bridge.CancelBrowserSignIn).toHaveBeenCalled();
  });
});
