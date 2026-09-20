import { createPortal } from 'react-dom';
import { TerminalPane } from './TerminalPane';

// TerminalHost mounts every open terminal's emulator exactly once, each portalled
// into its own stable host element (`hosts`: id -> element). The host never
// changes for a terminal's life; App moves the host element between a pane's slot
// and a hidden park with appendChild. That matters because changing a portal's
// container remounts its child — which would dispose xterm and lose scrollback —
// whereas moving the unchanged container element does not. So a terminal keeps
// its emulator, cursor, and scrollback across tab switches, pane drags, grid
// re-tiling, and project switches; `activeById` only toggles its visibility.
export function TerminalHost({ hosts, activeById }) {
  return [...hosts].map(([id, host]) => (
    createPortal(<TerminalPane id={id} active={!!activeById[id]} />, host, id)
  ));
}
