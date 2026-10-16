import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, LOCALES, LOCALE_NAMES, ThemeToggle, useLocale } from '@buildmax/gui';
import { getApp } from './lib/app';
import { signInLayout } from './lib/signInLayout';
import { EventsOn } from './lib/wailsRuntime';
import { useT } from './i18n';

// The shape of an address, never a value: someone signing in to a deployment
// has its address from whoever runs it.
const SERVER_URL_EXAMPLE = 'https://buildmax.example.com';

// How a browser sign-in ended; emitted by the Go side (sign_in.go).
const EVENT_BROWSER_SIGN_IN = 'desktop/browser-sign-in';

/**
 * Sign in to a server.
 *
 * This is an action, not a gate: the app already works without it, running the
 * agent here against the models set up on this machine. Signing in switches to
 * that deployment's models and connects the account to a space's work, and
 * signing out switches back. See docs/design/client-modes.md.
 *
 * There are two ways in. The browser one works for every account a server
 * admits, single sign-on included: Desktop starts a request, the person
 * confirms it in Portal, and the Go side polls until they do. The password
 * form is the native one, and `local_login` decides who may use it, so the
 * page reads the server's methods and leads with whichever way works there
 * (design enterprise-identity-and-access §14.7). A login code is how a new
 * account is claimed and how a forgotten password is recovered — BuildMax has
 * no mail channel, so an operator issues that code by hand.
 *
 * The page replaces the whole workbench, so it carries its own theme and
 * language controls.
 */
export default function LoginPage({ onLogin, onCancel, expiredDetail = '', accountDisabled = false, knownServerURL = '' }) {
  const t = useT();
  const { locale, setLocale } = useLocale();
  const [mode, setMode] = useState('password');
  const [serverURL, setServerURL] = useState(knownServerURL);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [otp, setOtp] = useState('');
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  // The methods of the server last asked: { server, status: 'loading' |
  // 'ready' | 'failed', methods, error }. Only an answer for the address in
  // the field lays the page out.
  const [methods, setMethods] = useState(null);
  const [adminPassword, setAdminPassword] = useState(false);
  // The request being confirmed in the browser, while the waiting panel shows.
  const [waiting, setWaiting] = useState(null);
  const [starting, setStarting] = useState(false);
  const [browserError, setBrowserError] = useState(null);
  // How the last browser sign-in ended when it did not sign in: 'denied',
  // 'expired', or a message.
  const [ended, setEnded] = useState(null);
  const [copied, setCopied] = useState(false);
  const waitingRef = useRef(null);
  // The latest onLogin, so a parent re-rendering does not resubscribe the wait.
  const onLoginRef = useRef(onLogin);
  useEffect(() => {
    onLoginRef.current = onLogin;
  }, [onLogin]);

  const app = useMemo(() => getApp(), []);
  const server = serverURL.trim();
  const credential = mode === 'password' ? password : otp.trim();

  const loadMethods = useCallback((url) => {
    const target = url.trim();
    if (!target || !app?.GetAuthMethods) return;
    setMethods({ server: target, status: 'loading' });
    app.GetAuthMethods(target).then(
      (answer) => setMethods((current) => (current?.server === target ? { server: target, status: 'ready', methods: answer } : current)),
      (err) => setMethods((current) => (current?.server === target ? { server: target, status: 'failed', error: err?.message ?? String(err) } : current)),
    );
  }, [app]);

  // A server this machine was configured with fills the field; with none it
  // stays empty rather than guessing. An ended login already names its
  // server, and signing in again means that one.
  useEffect(() => {
    if (knownServerURL) {
      loadMethods(knownServerURL);
      return undefined;
    }
    if (!app?.GetDefaultServerURL) return undefined;
    let cancelled = false;
    app.GetDefaultServerURL().then((url) => {
      if (cancelled || !url) return;
      setServerURL((current) => current || url);
      loadMethods(url);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [app, knownServerURL, loadMethods]);

  // While the browser sign-in waits, its outcome arrives as one event.
  useEffect(() => {
    waitingRef.current = waiting;
    if (!waiting) return undefined;
    return EventsOn(EVENT_BROWSER_SIGN_IN, (result) => {
      if (result?.outcome === 'signed_in') {
        waitingRef.current = null;
        onLoginRef.current?.(result.status);
        return;
      }
      setWaiting(null);
      setCopied(false);
      setEnded(result?.outcome === 'denied' || result?.outcome === 'expired' ? result.outcome : (result?.message || 'failed'));
    });
  }, [waiting]);

  // Leaving the page stops the wait; nothing would be listening for it.
  useEffect(() => () => {
    if (waitingRef.current) getApp()?.CancelBrowserSignIn?.();
  }, []);

  // Nothing is offered until the server says how its accounts sign in, so an
  // SSO account is never shown a password form that will refuse it.
  const answered = methods?.server === server ? methods : null;
  const ready = answered?.status === 'ready';
  const unreachable = answered?.status === 'failed';
  const checking = answered?.status === 'loading';
  const layout = signInLayout(ready ? answered.methods : null);

  function checkServer(e) {
    e?.preventDefault();
    if (server && methods?.server !== server) loadMethods(server);
  }

  async function handleSubmit(e) {
    e.preventDefault();
    if (!server || !email.trim() || !credential || loading || !app) return;
    setError(null);
    setLoading(true);
    try {
      const status =
        mode === 'password'
          ? await app.DoLoginWithPassword(server, email.trim(), password)
          : await app.DoLogin(server, email.trim(), otp.trim());
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

  async function startBrowserSignIn() {
    if (!server || starting || !app?.StartBrowserSignIn) return;
    setBrowserError(null);
    setEnded(null);
    setStarting(true);
    try {
      const started = await app.StartBrowserSignIn(server);
      setCopied(false);
      setWaiting(started);
    } catch (err) {
      setBrowserError(err?.message ?? String(err));
    } finally {
      setStarting(false);
    }
  }

  function cancelBrowserSignIn() {
    app?.CancelBrowserSignIn?.();
    setWaiting(null);
    setCopied(false);
  }

  function copyLink() {
    app?.CopyBrowserSignInLink?.().then(() => setCopied(true), () => {});
  }

  const prefs = (
    <div className="login-page__prefs">
      <div className="login-page__locales" role="group" aria-label={t('shell.language')}>
        {LOCALES.map((option) => (
          <button
            key={option}
            type="button"
            className="login-page__locale"
            aria-pressed={locale === option}
            lang={option}
            onClick={() => setLocale(option)}
          >
            {LOCALE_NAMES[option]}
          </button>
        ))}
      </div>
      <ThemeToggle />
    </div>
  );

  if (waiting) {
    return (
      <div className="login-page">
        {prefs}
        <div className="login-page__card">
          <h1 className="login-page__title">BuildMax</h1>
          <p className="login-page__subtitle">{t('login.waiting.instruction')}</p>
          <p className="login-page__label" id="login-code-label">{t('login.waiting.code')}</p>
          <p className="login-page__code" aria-labelledby="login-code-label">{waiting.user_code}</p>
          <p className="login-page__waiting" role="status">{t('login.waiting.status')}</p>
          <div className="login-page__form">
            <Button onClick={() => app?.OpenBrowserSignInAgain?.()}>
              {t('login.waiting.reopen')}
            </Button>
            <Button onClick={copyLink}>
              {t('login.waiting.copy')}
            </Button>
            {copied ? <p className="login-page__field-hint" role="status">{t('login.waiting.copied')}</p> : null}
            <Button variant="tertiary" onClick={cancelBrowserSignIn}>
              {t('login.waiting.cancel')}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  const busy = loading || starting;
  const browserLabel = layout.provider ? t('login.browser.provider', { provider: layout.provider }) : t('login.browser');
  const browserButton = (primary) => (
    <>
      <Button
        variant={primary ? 'primary' : 'secondary'}
        onClick={startBrowserSignIn}
        disabled={busy || !server}
      >
        {starting ? t('login.browser.starting') : browserLabel}
      </Button>
      {browserError ? <p className="login-page__error" role="alert">{browserError}</p> : null}
    </>
  );

  const passwordForm = (
    <form onSubmit={handleSubmit} className="login-page__form">
      <label className="login-page__label" htmlFor="login-email">{t('login.email')}</label>
      <input
        id="login-email"
        type="email"
        className="login-page__input"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        required
        autoComplete="email"
        disabled={busy}
      />

      {mode === 'code' ? (
        <>
          <label className="login-page__label" htmlFor="login-otp">{t('login.code')}</label>
          <input
            id="login-otp"
            type="text"
            className="login-page__input"
            value={otp}
            onChange={(e) => setOtp(e.target.value)}
            placeholder="bmxlogin_…"
            autoComplete="one-time-code"
            required
            disabled={busy}
          />
          <p className="login-page__field-hint">{t('login.codeHint')}</p>
        </>
      ) : (
        <>
          <label className="login-page__label" htmlFor="login-password">{t('login.password')}</label>
          <input
            id="login-password"
            type="password"
            className="login-page__input"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            autoComplete="current-password"
            disabled={busy}
          />
        </>
      )}

      {error && <p className="login-page__error" role="alert">{error}</p>}
      <Button
        type="submit"
        variant={layout.password === 'first' ? 'primary' : 'secondary'}
        disabled={busy || !server || !email.trim() || !credential}
      >
        {loading ? t('login.signingIn') : t('login.signIn')}
      </Button>
      <Button variant="tertiary" onClick={switchMode} disabled={busy}>
        {mode === 'password' ? t('login.toCode') : t('login.toPassword')}
      </Button>
    </form>
  );

  let subtitle = mode === 'password' ? t('login.subtitle.password') : t('login.subtitle.code');
  if (!ready) subtitle = t('login.subtitle.server');
  else if (layout.password !== 'first' && !adminPassword) subtitle = t('login.subtitle.browser');

  let methodsSection;
  if (!ready && !unreachable) {
    methodsSection = (
      <div className="login-page__form">
        <Button type="submit" form="login-server-form" variant="primary" disabled={checking || busy || !server}>
          {checking ? t('login.checking') : t('login.continue')}
        </Button>
      </div>
    );
  } else if (unreachable) {
    methodsSection = (
      <div className="login-page__form">
        <p className="login-page__error" role="alert">
          {t('login.unreachable', { server: answered.server })}
          <span className="login-page__error-detail">{answered.error}</span>
        </p>
        <Button onClick={() => loadMethods(server)}>
          {t('login.retry')}
        </Button>
      </div>
    );
  } else if (layout.password === 'first') {
    methodsSection = passwordForm;
  } else {
    methodsSection = (
      <div className="login-page__form">
        {browserButton(true)}
        {layout.password === 'behind' ? (
          adminPassword ? passwordForm : (
            <Button variant="tertiary" onClick={() => setAdminPassword(true)} disabled={busy}>
              {t('login.adminPassword')}
            </Button>
          )
        ) : null}
      </div>
    );
  }

  let endedMessage = null;
  if (ended === 'denied') endedMessage = t('login.browser.denied');
  else if (ended === 'expired') endedMessage = t('login.browser.expired');
  else if (ended === 'failed') endedMessage = t('login.browser.failed');
  else if (ended) endedMessage = ended;

  return (
    <div className="login-page">
      {prefs}
      <div className="login-page__card">
        <h1 className="login-page__title">BuildMax</h1>
        {expiredDetail ? (
          <p className="login-page__error" role="alert">
            {accountDisabled ? t('login.accountDisabled') : t('login.sessionEnded')}
          </p>
        ) : null}
        <p className="login-page__subtitle">{subtitle}</p>
        <form id="login-server-form" className="login-page__form login-page__server" onSubmit={checkServer}>
          <label className="login-page__label" htmlFor="login-server">{t('login.serverURL')}</label>
          <input
            id="login-server"
            type="text"
            className="login-page__input"
            value={serverURL}
            onChange={(e) => setServerURL(e.target.value)}
            onBlur={() => checkServer()}
            placeholder={SERVER_URL_EXAMPLE}
            autoComplete="url"
            required
            disabled={busy}
          />
          <p className="login-page__field-hint">
            {t('login.serverHint')}
          </p>
        </form>
        {endedMessage ? <p className="login-page__error login-page__ended" role="alert">{endedMessage}</p> : null}

        {methodsSection}

        <div className="login-page__divider"><span>{t('login.or')}</span></div>
        {ready && layout.password === 'first' ? (
          <div className="login-page__form login-page__alternative">{browserButton(false)}</div>
        ) : null}
        <Button className="login-page__local" onClick={onCancel} disabled={busy}>
          {expiredDetail ? t('login.signOutLocal') : t('login.keepLocal')}
        </Button>
        <p className="login-page__local-hint">
          {t('login.localHint')}
          {expiredDetail ? t('login.localHint.expired') : t('login.localHint.stay')}
        </p>
      </div>
    </div>
  );
}
