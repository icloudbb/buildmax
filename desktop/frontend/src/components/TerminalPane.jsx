import { useEffect, useMemo, useRef } from 'react';
import { useTheme } from '@buildmax/gui';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { EventsOn } from '../lib/wailsRuntime';
import { getApp } from '../lib/app';
import { decodeBase64ToBytes } from '../lib/terminalBytes';
import { terminalThemeFor } from '../lib/terminalTheme';

// TerminalPane renders one shell strand: an xterm emulator bound to the Go PTY
// with id `id`. It owns its emulator for the strand's life so scrollback and
// cursor survive tab switches — the parent hides an inactive pane rather than
// unmounting it, and does not close the backend strand on unmount.
export function TerminalPane({ id, active, onExit }) {
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

    const dataSub = term.onData((chunk) => app?.TerminalWrite?.(id, chunk));
    const resizeSub = term.onResize(({ cols, rows }) => app?.TerminalResize?.(id, cols, rows));

    const offData = EventsOn('desktop/terminal/data', (p) => {
      if (!p || p.id !== id) return;
      term.write(decodeBase64ToBytes(p.chunk));
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
