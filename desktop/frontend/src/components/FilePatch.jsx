import { useTheme } from '@buildmax/gui';
import { useEffect, useState } from 'react';
import { displayDiffPath, highlightDiffRows, parsePatchLines, statusGlyph, statusTitle } from '../lib/format';
import { highlightToLines } from '../lib/highlight';

// FilePatch renders one changed file's patch: a header and the syntax-highlighted
// diff rows. It is the shared content of both the Explorer's diff tab and the
// legacy inspector diff panel, so the two never diverge. Highlighting is
// per-hunk and swaps in once ready; the parsed patch renders immediately.
export function FilePatch({ file }) {
  const { theme } = useTheme();
  const [highlightedRows, setHighlightedRows] = useState(null);

  useEffect(() => {
    let cancelled = false;
    setHighlightedRows(null);
    if (!file || file.binary || !file.patch) return undefined;
    highlightDiffRows(parsePatchLines(file.patch), file.path, theme, highlightToLines)
      .then((rows) => { if (!cancelled) setHighlightedRows(rows); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [file, theme]);

  if (!file) return <p className="diff-drawer__empty">Select a changed file.</p>;

  return (
    <>
      <div className="diff-drawer__viewer-header">
        <span className={`diff-drawer__status diff-drawer__status--${file.status}`}>
          {statusGlyph(file.status)}
        </span>
        <span className="diff-drawer__viewer-path">{displayDiffPath(file)}</span>
        <span className="diff-drawer__viewer-kind">{statusTitle(file.status)}</span>
      </div>
      {file.binary ? (
        <p className="diff-drawer__empty">Binary file changed.</p>
      ) : file.patch ? (
        <div className="diff-code" role="table">
          {(highlightedRows ?? parsePatchLines(file.patch)).map((row, idx) => (
            <div key={idx} className={`diff-code__row diff-code__row--${row.kind}`} role="row">
              <span className="diff-code__line" role="cell">{row.oldLine}</span>
              <span className="diff-code__line" role="cell">{row.newLine}</span>
              <code className="diff-code__text" role="cell">
                {row.tokens
                  ? row.tokens.map((t, i) => <span key={i} style={{ color: t.color }}>{t.content}</span>)
                  // Line-content kinds carry a leading +/-/space diff marker that
                  // highlighted tokens never include; strip it here too so the
                  // text does not shift once tokens arrive.
                  : ((row.kind === 'add' || row.kind === 'del' || row.kind === 'context' ? row.text.slice(1) : row.text) || ' ')}
              </code>
            </div>
          ))}
          {file.truncated && <div className="diff-code__truncated">Diff truncated.</div>}
        </div>
      ) : (
        <p className="diff-drawer__empty">No text diff available.</p>
      )}
    </>
  );
}
