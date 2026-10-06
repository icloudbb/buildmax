import { useTheme } from '@buildmax/gui';
import { useEffect, useState } from 'react';
import { displayDiffPath, highlightDiffRows, parsePatchLines, statusGlyph } from '../lib/format';
import { highlightToLines } from '../lib/highlight';
import { useT } from '../i18n';

const STATUS_KEY = {
  added: 'files.status.added',
  deleted: 'files.status.deleted',
  renamed: 'files.status.renamed',
};

// FilePatch renders one changed file's patch: a header and the syntax-highlighted
// diff rows. It is the shared content of both the Explorer's diff tab and the
// legacy inspector diff panel, so the two never diverge. Highlighting is
// per-hunk and swaps in once ready; the parsed patch renders immediately.
export function FilePatch({ file }) {
  const { theme } = useTheme();
  const t = useT();
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

  if (!file) return <p className="diff-drawer__empty">{t('files.selectChanged')}</p>;

  return (
    <>
      <div className="diff-drawer__viewer-header">
        <span className={`diff-drawer__status diff-drawer__status--${file.status}`}>
          {statusGlyph(file.status)}
        </span>
        <span className="diff-drawer__viewer-path">{displayDiffPath(file)}</span>
        <span className="diff-drawer__viewer-kind">{t(STATUS_KEY[file.status] ?? 'files.status.modified')}</span>
      </div>
      {file.binary ? (
        <p className="diff-drawer__empty">{t('files.binaryChanged')}</p>
      ) : file.patch ? (
        <div className="diff-code" role="table">
          {(highlightedRows ?? parsePatchLines(file.patch)).map((row, idx) => (
            <div key={idx} className={`diff-code__row diff-code__row--${row.kind}`} role="row">
              <span className="diff-code__line" role="cell">{row.oldLine}</span>
              <span className="diff-code__line" role="cell">{row.newLine}</span>
              <code className="diff-code__text" role="cell">
                {row.tokens
                  ? row.tokens.map((tok, i) => <span key={i} style={{ color: tok.color }}>{tok.content}</span>)
                  // Line-content kinds carry a leading +/-/space diff marker that
                  // highlighted tokens never include; strip it here too so the
                  // text does not shift once tokens arrive.
                  : ((row.kind === 'add' || row.kind === 'del' || row.kind === 'context' ? row.text.slice(1) : row.text) || ' ')}
              </code>
            </div>
          ))}
          {file.truncated && <div className="diff-code__truncated">{t('files.diffTruncated')}</div>}
        </div>
      ) : (
        <p className="diff-drawer__empty">{t('files.noTextDiff')}</p>
      )}
    </>
  );
}
