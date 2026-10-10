import { describe, expect, it } from 'vitest';
import { countChanges, diffLines, splitLines } from './linediff';

const show = (hunks) => hunks.map((h) => h.rows.map((r) => `${{ add: '+', del: '-', context: ' ' }[r.kind]}${r.text}`));

describe('splitLines', () => {
  it('ends the last line at a final newline and reads CRLF as LF', () => {
    expect(splitLines('')).toEqual([]);
    expect(splitLines('a\n')).toEqual(['a']);
    expect(splitLines('a\r\nb')).toEqual(['a', 'b']);
    expect(splitLines('\n')).toEqual(['']);
  });
});

describe('diffLines', () => {
  it('returns no hunks for identical text', () => {
    expect(diffLines('a\nb\n', 'a\nb\n')).toEqual([]);
  });

  it('keeps whitespace and numbers lines from both sides', () => {
    const hunks = diffLines('func main() {\n\tone()\n}\n', 'func main() {\n\tone()\n\t  two()\n}\n');
    expect(show(hunks)).toEqual([[' func main() {', ' \tone()', '+\t  two()', ' }']]);
    const added = hunks[0].rows[2];
    expect(added).toMatchObject({ oldLine: null, newLine: 3 });
    expect(hunks[0].rows[3]).toMatchObject({ oldLine: 3, newLine: 4 });
  });

  it('splits distant changes into hunks with context around each', () => {
    const before = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join('\n');
    const after = before.replace('line 2\n', 'line two\n').replace('line 18\n', 'line eighteen\n');
    const hunks = diffLines(before, after, { context: 1 });
    expect(show(hunks)).toEqual([
      [' line 1', '-line 2', '+line two', ' line 3'],
      [' line 17', '-line 18', '+line eighteen', ' line 19'],
    ]);
    expect(countChanges(hunks)).toEqual({ added: 2, removed: 2 });
  });

  it('shows emptying a file as every line removed', () => {
    expect(show(diffLines('a\n  b\n', ''))).toEqual([['-a', '-  b']]);
  });
});
