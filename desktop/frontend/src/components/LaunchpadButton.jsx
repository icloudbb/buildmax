import { useCallback, useEffect, useRef, useState } from 'react';
import { getApp } from '../lib/app';

// RocketIcon — the launchpad glyph.
function RocketIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M5 15c-1.5 1.5-2 5-2 5s3.5-.5 5-2" />
      <path d="M13.5 4.5C16 2 20 2 22 2s0 4-2.5 6.5L14 14l-4-4 3.5-5.5Z" />
      <path d="M10 10l-4 1 3 3 1-4Z" />
      <circle cx="15.5" cy="8.5" r="1.2" />
    </svg>
  );
}

// LaunchpadButton is a global status-bar control: a popover of user-defined
// quick-launch applications. Clicking an entry hands its target to the OS via the
// Go backend; entries are stored on the Go side (launchpad.json), so they survive
// independent of the webview cache and are shared across every window.
export function LaunchpadButton() {
  const [entries, setEntries] = useState([]);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  // The URL form is revealed on demand so the common case (a listed app) stays a
  // single click; adding a website is a name + address the user types.
  const [urlForm, setUrlForm] = useState(false);
  const [urlName, setUrlName] = useState('');
  const [urlValue, setUrlValue] = useState('');
  const rootRef = useRef(null);

  const reload = useCallback(async () => {
    const app = getApp();
    if (!app?.ListLaunchpadEntries) return;
    try {
      setEntries(await app.ListLaunchpadEntries() ?? []);
    } catch {
      /* leave the last known list in place */
    }
  }, []);

  useEffect(() => { reload(); }, [reload]);

  // A closed popover forgets an in-progress URL entry, so it reopens clean.
  useEffect(() => {
    if (!open) {
      setUrlForm(false);
      setUrlName('');
      setUrlValue('');
    }
  }, [open]);

  // Close the popover on an outside click or Escape.
  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e) => { if (rootRef.current && !rootRef.current.contains(e.target)) setOpen(false); };
    const onKey = (e) => { if (e.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const launch = useCallback(async (id) => {
    const app = getApp();
    if (!app?.LaunchEntry) return;
    try {
      await app.LaunchEntry(id);
      setOpen(false);
    } catch {
      /* the app may have moved or been removed; leave the popover open */
    }
  }, []);

  const remove = useCallback(async (id) => {
    const app = getApp();
    if (!app?.RemoveLaunchpadEntry) return;
    try {
      await app.RemoveLaunchpadEntry(id);
      await reload();
    } catch {
      /* ignore */
    }
  }, [reload]);

  const addApp = useCallback(async () => {
    const app = getApp();
    if (!app?.PickLaunchpadTarget || !app?.AddLaunchpadEntry) return;
    setBusy(true);
    try {
      const target = await app.PickLaunchpadTarget();
      if (target) {
        await app.AddLaunchpadEntry('', target, []);
        await reload();
      }
    } catch {
      /* the picker was cancelled or failed; nothing to add */
    } finally {
      setBusy(false);
    }
  }, [reload]);

  const closeUrlForm = useCallback(() => {
    setUrlForm(false);
    setUrlName('');
    setUrlValue('');
  }, []);

  const addUrl = useCallback(async (e) => {
    e?.preventDefault?.();
    const app = getApp();
    if (!app?.AddLaunchpadEntry) return;
    let target = urlValue.trim();
    if (!target) return;
    // A bare host is meant as a website; default it to https so the OS opens it
    // in the browser rather than treating it as a file path.
    if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(target)) target = `https://${target}`;
    try {
      await app.AddLaunchpadEntry(urlName.trim(), target, []);
      closeUrlForm();
      await reload();
    } catch {
      /* leave the form open so the user can correct the address */
    }
  }, [urlName, urlValue, reload, closeUrlForm]);

  return (
    <div className="launchpad" ref={rootRef}>
      <button
        type="button"
        className="workspace-statusbar__btn"
        title="Launchpad"
        aria-label="Launchpad"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span aria-hidden><RocketIcon /></span>
      </button>
      {open && (
        <div className="launchpad-popover" role="menu">
          <div className="launchpad-popover__title">Launchpad</div>
          {entries.length === 0 ? (
            <div className="launchpad-empty">No applications yet.</div>
          ) : (
            entries.map((e) => (
              <div className="launchpad-item" key={e.id}>
                <button
                  type="button"
                  className="launchpad-item__launch"
                  title={e.target}
                  onClick={() => launch(e.id)}
                >
                  {e.name}
                </button>
                <button
                  type="button"
                  className="launchpad-item__remove"
                  title="Remove"
                  aria-label={`Remove ${e.name}`}
                  onClick={() => remove(e.id)}
                >
                  ×
                </button>
              </div>
            ))
          )}
          <div className="launchpad-actions">
            <button
              type="button"
              className="launchpad-add"
              onClick={addApp}
              disabled={busy}
            >
              {busy ? 'Adding…' : 'Add application…'}
            </button>
            {urlForm ? (
              <form className="launchpad-urlform" onSubmit={addUrl}>
                <input
                  className="launchpad-urlform__input"
                  type="text"
                  placeholder="Name (optional)"
                  value={urlName}
                  onChange={(ev) => setUrlName(ev.target.value)}
                />
                <input
                  className="launchpad-urlform__input"
                  type="text"
                  placeholder="https://…"
                  value={urlValue}
                  onChange={(ev) => setUrlValue(ev.target.value)}
                  autoFocus
                />
                <div className="launchpad-urlform__row">
                  <button type="submit" className="launchpad-urlform__save" disabled={!urlValue.trim()}>Add</button>
                  <button type="button" className="launchpad-urlform__cancel" onClick={closeUrlForm}>Cancel</button>
                </div>
              </form>
            ) : (
              <button
                type="button"
                className="launchpad-add"
                onClick={() => setUrlForm(true)}
              >
                Add URL…
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
