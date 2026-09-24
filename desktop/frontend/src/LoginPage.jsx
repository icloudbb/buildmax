import { useEffect, useState } from 'react';
import { getApp } from './lib/app';

// Where a server nobody has named listens on this machine. The Go side answers
// with settings.yaml's server_url when there is one; this is the fallback for
// the moment before that answer arrives, and for a browser with no bindings.
const DEFAULT_SERVER_URL = 'http://localhost:5678';

/**
 * Sign in to a server.
 *
 * This is an action, not a gate: the app already works without it, running the
 * agent here against the models in settings.yaml. Signing in switches to that
 * deployment's models and connects the account to a space's work, and signing
 * out switches back. See docs/design/client-modes.md.
 *
 * Of the two ways in, a password is the everyday one. A login code is how a new
 * account is claimed and how a forgotten password is recovered — BuildMax has
 * no mail channel, so an operator issues that code by hand.
 */
export default function LoginPage({ onLogin, onCancel, expiredDetail = '', accountDisabled = false, knownServerURL = '' }) {
  const [mode, setMode] = useState('password');
  const [serverURL, setServerURL] = useState(knownServerURL || DEFAULT_SERVER_URL);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [otp, setOtp] = useState('');
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);

  const app = getApp();
  const credential = mode === 'password' ? password : otp.trim();

  // The default is a starting point, not an assumption: a deployment behind an
  // ingress publishes one origin for Portal and API, and it is not this one.
  // An ended login already names its server; signing in again means that one.
  useEffect(() => {
    if (knownServerURL || !app?.GetDefaultServerURL) return;
    let cancelled = false;
    app.GetDefaultServerURL().then((url) => {
      if (!cancelled && url) setServerURL(url);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [app, knownServerURL]);

  async function handleSubmit(e) {
    e.preventDefault();
    if (!serverURL.trim() || !email.trim() || !credential || loading || !app) return;
    setError(null);
    setLoading(true);
    try {
      const status =
        mode === 'password'
          ? await app.DoLoginWithPassword(serverURL.trim(), email.trim(), password)
          : await app.DoLogin(serverURL.trim(), email.trim(), otp.trim());
      if (onLogin) onLogin(status);
    } catch (err) {
      setError(err?.message ?? String(err));
    } finally {
      setLoading(false);
    }
  }

  function switchMode() {
    setMode(mode === 'password' ? 'code' : 'password');
    setError(null);
    setPassword('');
    setOtp('');
  }

  return (
    <div className="login-page">
      <div className="login-page__card">
        <h1 className="login-page__title">BuildMax</h1>
        {expiredDetail ? (
          <p className="login-page__error" role="alert">
            {accountDisabled
              ? 'An administrator of this server disabled your account, so this app cannot use its models. Ask them to re-enable it, or return to using this machine on its own.'
              : 'Your session has ended, so this app cannot reach its models. Sign in again, or return to using this machine on its own.'}
          </p>
        ) : null}
        <p className="login-page__subtitle">
          {mode === 'password'
            ? 'Sign in to a BuildMax server to use the models it offers'
            : 'Sign in with a login code from your administrator'}
        </p>
        <form onSubmit={handleSubmit} className="login-page__form">
          <label className="login-page__label" htmlFor="login-server">Server URL</label>
          <input
            id="login-server"
            type="text"
            className="login-page__input"
            value={serverURL}
            onChange={(e) => setServerURL(e.target.value)}
            placeholder={DEFAULT_SERVER_URL}
            required
            disabled={loading}
          />
          <p className="login-page__field-hint">
            The API address. Behind an ingress it is the origin the Portal is on,
            not the server's own port.
          </p>

          <label className="login-page__label" htmlFor="login-email">Email</label>
          <input
            id="login-email"
            type="email"
            className="login-page__input"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoComplete="email"
            disabled={loading}
          />

          {mode === 'password' ? (
            <>
              <label className="login-page__label" htmlFor="login-password">Password</label>
              <input
                id="login-password"
                type="password"
                className="login-page__input"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                autoComplete="current-password"
                disabled={loading}
              />
            </>
          ) : (
            <>
              <label className="login-page__label" htmlFor="login-otp">Login code</label>
              <input
                id="login-otp"
                type="text"
                className="login-page__input"
                value={otp}
                onChange={(e) => setOtp(e.target.value)}
                placeholder="bmxlogin_…"
                autoComplete="one-time-code"
                required
                disabled={loading}
              />
            </>
          )}

          {error && <p className="login-page__error" role="alert">{error}</p>}
          <button
            type="submit"
            className="login-page__submit"
            disabled={loading || !serverURL.trim() || !email.trim() || !credential}
          >
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
          <button
            type="button"
            className="login-page__link"
            onClick={switchMode}
            disabled={loading}
          >
            {mode === 'password'
              ? 'Forgot your password, or have a login code?'
              : 'Sign in with a password'}
          </button>
        </form>
        <div className="login-page__divider"><span>or</span></div>
        <button
          type="button"
          className="login-page__local"
          onClick={onCancel}
          disabled={loading}
        >
          {expiredDetail ? 'Sign out and use this machine on its own' : 'Keep using this machine on its own'}
        </button>
        <p className="login-page__local-hint">
          The agent runs here, with the models in your settings.yaml.
          {expiredDetail
            ? ' Signing out removes the ended session; nothing local is discarded.'
            : ' Nothing is lost by staying — you can sign in whenever you want.'}
        </p>
      </div>
    </div>
  );
}
