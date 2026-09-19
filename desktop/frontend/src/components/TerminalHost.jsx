import { createPortal } from 'react-dom';
import { TerminalPane } from './TerminalPane';

// TerminalHost keeps every open terminal's emulator mounted for its whole life
// and portals it into whichever pane currently shows it, or into a hidden park
// when no pane does. Because a portal relocates the DOM node without unmounting
// the component, dragging a terminal tab to another pane — or re-tiling the grid
// around it — never recreates xterm, so scrollback and cursor survive the move.
//
// `terminals` is [{ id, target, active }]: target is the pane's slot element (or
// null to park), active is true when that terminal is its pane's visible tab.
export function TerminalHost({ terminals, park }) {
  return terminals.map(({ id, target, active }) => {
    const dest = target ?? park;
    if (!dest) return null;
    return createPortal(<TerminalPane id={id} active={active} />, dest, id);
  });
}
