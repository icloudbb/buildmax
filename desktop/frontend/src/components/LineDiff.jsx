import { Fragment } from 'react';

const MARKER = { add: '+', del: '−', context: ' ' };

// LineDiff renders lib/linediff hunks: line numbers, a +/− marker on every
// changed line so the meaning never rests on color alone, and the text with
// its whitespace intact. numbers is false when the texts are fragments, not
// whole files, so their line numbers would point at the wrong lines.
export function LineDiff({ hunks, numbers = true, label }) {
  return (
    <div className={`line-diff${numbers ? '' : ' line-diff--plain'}`} role="table" aria-label={label}>
      {hunks.map((hunk, h) => (
        <Fragment key={h}>
          {h > 0 && (
            <div className="line-diff__row line-diff__row--gap" role="row">
              <span className="line-diff__text" role="cell">⋯</span>
            </div>
          )}
          {hunk.rows.map((row, i) => (
            <div key={i} className={`line-diff__row line-diff__row--${row.kind}`} role="row">
              {numbers && <span className="line-diff__num" role="cell">{row.oldLine ?? ''}</span>}
              {numbers && <span className="line-diff__num" role="cell">{row.newLine ?? ''}</span>}
              <span className="line-diff__marker" role="cell">{MARKER[row.kind]}</span>
              <code className="line-diff__text" role="cell">{row.text || ' '}</code>
            </div>
          ))}
        </Fragment>
      ))}
    </div>
  );
}
