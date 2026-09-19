import { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { EventsOn } from '../lib/wailsRuntime';
import { getApp } from '../lib/app';
import { decodeBase64ToBytes } from '../lib/terminalBytes';

// TerminalPane renders one shell strand: an xterm emulator bound to the Go PTY
// with id `id`. It owns its emulator for the strand's life so scrollback and
// cursor survive tab switches — the parent hides an inactive pane rather than
// unmounting it, and does not close the backend strand on unmount.
export function TerminalPane({ id, active, onExit }) {
  const containerRef = useRef(null);
  const termRef = useRef(null);
  const fitRef = useRef(null);
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
      theme: { background: '#1e1e1e' },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(containerRef.current);
    termRef.current = term;
    fitRef.current = fit;
    try { fit.fit(); } catch { /* container may not be measured yet */ }

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

    const ro = new ResizeObserver(() => { try { fit.fit(); } catch { /* ignore */ } });
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
      try { fitRef.current?.fit(); } catch { /* ignore */ }
      termRef.current?.focus();
    });
    return () => cancelAnimationFrame(raf);
  }, [active]);

  return (
    <div
      className={`terminal-pane${active ? '' : ' terminal-pane--hidden'}`}
      ref={containerRef}
    />
  );
}
