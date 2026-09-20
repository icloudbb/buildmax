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

  const add = useCallback(async () => {
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
          <button
            type="button"
            className="launchpad-add"
            onClick={add}
            disabled={busy}
          >
            {busy ? 'Adding…' : 'Add application…'}
          </button>
        </div>
      )}
    </div>
  );
}
