import { useEffect, useMemo, useRef } from 'react';
import { useTheme } from '@buildmax/gui';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SerializeAddon } from '@xterm/addon-serialize';
import '@xterm/xterm/css/xterm.css';
import { EventsOn } from '../lib/wailsRuntime';
import { getApp } from '../lib/app';
import { decodeBase64ToBytes } from '../lib/terminalBytes';
import { terminalThemeFor } from '../lib/terminalTheme';

// How much scrollback to persist per terminal, and how long after output settles
// before writing a snapshot. The debounce coalesces bursts; a restart loses at
// most this much of the very last output.
const SNAPSHOT_SCROLLBACK = 1000;
const SNAPSHOT_DEBOUNCE_MS = 1500;

// TerminalPane renders one shell strand: an xterm emulator bound to the Go PTY
// with id `id`. It owns its emulator for the strand's life so scrollback and
// cursor survive tab switches — the parent hides an inactive pane rather than
// unmounting it, and does not close the backend strand on unmount.
//
// projectId/restoreKey identify the terminal for snapshot persistence: its buffer
// is serialized and saved (debounced) so a restart can restore what it showed.
// restoreContent, when present, is the previous session's serialized buffer,
// written once at mount above the fresh shell (see App.respawnTerminalTabs).
export function TerminalPane({ id, active, onExit, projectId, restoreKey, restoreContent }) {
  const containerRef = useRef(null);
  const termRef = useRef(null);
  const fitRef = useRef(null);
  const doFitRef = useRef(null);
  // The terminal follows the app's light/dark theme. The create effect seeds the
  // emulator from the palette captured at mount (via a ref, so a later theme
  // change does not re-run it and lose scrollback); a separate effect repaints
  // the live emulator on change.
  const { theme } = useTheme();
  const termTheme = useMemo(() => terminalThemeFor(theme), [theme]);
  const themeRef = useRef(termTheme);
  // Keep the latest onExit without re-running the create effect, which would
  // tear down and recreate the emulator on every parent render.
  const onExitRef = useRef(onExit);
  useEffect(() => { onExitRef.current = onExit; });
  // Snapshot identity, kept current for the save closure without re-running the
  // create effect. restoreContent is read only at mount, so it stays a ref seed.
  const snapMetaRef = useRef({ projectId, restoreKey });
  useEffect(() => { snapMetaRef.current = { projectId, restoreKey }; });
  const restoreRef = useRef(restoreContent);

  // Create the emulator once and wire it to the backend strand.
  useEffect(() => {
    const app = getApp();
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      fontSize: 13,
      // Keep a deep scrollback so a session's earlier command output stays
      // reachable after switching tabs; the default (1000) is easy to exceed.
      scrollback: 10000,
      theme: themeRef.current,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    const serialize = new SerializeAddon();
    term.loadAddon(serialize);
    term.open(containerRef.current);
    termRef.current = term;
    fitRef.current = fit;
    // Fit only when the host has a sane, measurable size. While its tab is hidden
    // the host is parked at 0×0, where FitAddon clamps its proposal to its floor
    // (cols 2, rows 1); resizing the emulator that small reflows the whole buffer
    // to a sliver, which overflows the scrollback cap and permanently evicts
    // earlier output (and resizes the PTY, so the shell repaints). Ask the addon
    // what it would resize to and skip a clamped/degenerate proposal, so scrollback
    // survives a switch — a real pane is always far wider and taller than the floor.
    const doFit = () => {
      let dims;
      try { dims = fit.proposeDimensions(); } catch { return; }
      if (!dims || !Number.isFinite(dims.cols) || !Number.isFinite(dims.rows)) return;
      if (dims.cols <= 2 || dims.rows <= 1) return;
      try { fit.fit(); } catch { /* ignore */ }
    };
    doFitRef.current = doFit;
    doFit();

    // Restore the previous session's contents above the fresh shell, once, after
    // fitting so it lays out at the real width. The live prompt appends below.
    const restore = restoreRef.current;
    if (restore) term.write(restore.endsWith('\n') ? restore : `${restore}\r\n`);

    // Persist the buffer a short while after output settles, so a restart can
    // restore what the terminal showed. Coalesces bursts; the latest state wins.
    let saveTimer = null;
    const saveSnapshot = () => {
      const { projectId: pid, restoreKey: rk } = snapMetaRef.current;
      if (!pid || !rk || !app?.SaveTerminalSnapshot) return;
      let content;
      try { content = serialize.serialize({ scrollback: SNAPSHOT_SCROLLBACK }); } catch { return; }
      app.SaveTerminalSnapshot(pid, rk, content)?.catch?.(() => {});
    };
    const scheduleSave = () => {
      if (saveTimer) return;
      saveTimer = setTimeout(() => { saveTimer = null; saveSnapshot(); }, SNAPSHOT_DEBOUNCE_MS);
    };

    const dataSub = term.onData((chunk) => app?.TerminalWrite?.(id, chunk));
    const resizeSub = term.onResize(({ cols, rows }) => app?.TerminalResize?.(id, cols, rows));

    const offData = EventsOn('desktop/terminal/data', (p) => {
      if (!p || p.id !== id) return;
      term.write(decodeBase64ToBytes(p.chunk));
      scheduleSave();
    });
    const offExit = EventsOn('desktop/terminal/exit', (p) => {
      if (!p || p.id !== id) return;
      const code = typeof p.code === 'number' ? ` (${p.code})` : '';
      term.write(`\r\n\x1b[90m[process exited${code}]\x1b[0m\r\n`);
      onExitRef.current?.(id, p.code);
    });

    const ro = new ResizeObserver(doFit);
    ro.observe(containerRef.current);

    return () => {
      ro.disconnect();
      offData();
      offExit();
      dataSub?.dispose?.();
      resizeSub?.dispose?.();
      // Flush a final snapshot while the buffer is still alive; a closed terminal's
      // snapshot is pruned on the next restore, so this only helps a live one.
      if (saveTimer) clearTimeout(saveTimer);
      saveSnapshot();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [id]);

  // When this pane becomes the active tab, refit to the now-visible area and
  // focus it. A hidden pane has no measurable size, so fit is deferred here.
  useEffect(() => {
    if (!active) return undefined;
    const raf = requestAnimationFrame(() => {
      doFitRef.current?.();
      termRef.current?.focus();
    });
    return () => cancelAnimationFrame(raf);
  }, [active]);

  // Repaint the live emulator when the app theme changes. Assigning options.theme
  // re-tints the existing buffer in place, so scrollback is untouched.
  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = termTheme;
  }, [termTheme]);

  return (
    <div
      className={`terminal-pane${active ? '' : ' terminal-pane--hidden'}`}
      // Match the gutter (the pane's padding) to the emulator background so the
      // terminal reads as one surface in both light and dark themes.
      style={{ background: termTheme.background }}
      ref={containerRef}
    />
  );
}
