// A line diff of two texts, grouped into hunks with surrounding context, for
// showing a change that has not been made yet (a tool approval) where no Git
// patch exists. Rows carry their text without a +/- marker and keep every
// space and tab, so a renderer can show them exactly.

// Above this many cells the LCS table costs more than a prompt should: the
// differing middle is shown as removed then added instead. Trimming the common
// prefix and suffix first keeps an ordinary edit far below it.
const MAX_LCS_CELLS = 1_000_000;

// splitLines splits text into lines. A final newline ends the last line rather
// than starting an empty one, and CRLF reads as LF, as the Edit tool reads it.
export function splitLines(text) {
  const value = String(text ?? '').replace(/\r\n/g, '\n');
  if (value === '') return [];
  const lines = value.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  return lines;
}

// diffLines returns the hunks that turn oldText into newText:
// [{ rows: [{ kind: 'context'|'del'|'add', text, oldLine, newLine }] }].
// Line numbers are 1-based; a deleted row has no newLine and an added row no
// oldLine. context is how many unchanged lines surround each change.
export function diffLines(oldText, newText, { context = 3 } = {}) {
  const ops = lineOps(splitLines(oldText), splitLines(newText));
  const changed = [];
  ops.forEach((op, i) => { if (op.kind !== 'context') changed.push(i); });
  if (!changed.length) return [];

  const hunks = [];
  let start = Math.max(0, changed[0] - context);
  let end = Math.min(ops.length, changed[0] + context + 1);
  for (const i of changed.slice(1)) {
    if (i - context <= end) {
      end = Math.min(ops.length, i + context + 1);
      continue;
    }
    hunks.push({ rows: ops.slice(start, end) });
    start = Math.max(0, i - context);
    end = Math.min(ops.length, i + context + 1);
  }
  hunks.push({ rows: ops.slice(start, end) });
  return hunks;
}

// countChanges totals the added and removed rows of diffLines' hunks.
export function countChanges(hunks) {
  let added = 0;
  let removed = 0;
  for (const hunk of hunks) {
    for (const row of hunk.rows) {
      if (row.kind === 'add') added += 1;
      else if (row.kind === 'del') removed += 1;
    }
  }
  return { added, removed };
}

function lineOps(a, b) {
  let prefix = 0;
  while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) prefix += 1;
  let suffix = 0;
  while (
    suffix < a.length - prefix && suffix < b.length - prefix
    && a[a.length - 1 - suffix] === b[b.length - 1 - suffix]
  ) suffix += 1;

  const kinds = [];
  for (let i = 0; i < prefix; i += 1) kinds.push('context');
  kinds.push(...middleOps(a.slice(prefix, a.length - suffix), b.slice(prefix, b.length - suffix)));
  for (let i = 0; i < suffix; i += 1) kinds.push('context');

  const ops = [];
  let ai = 0;
  let bi = 0;
  for (const kind of kinds) {
    if (kind === 'context') {
      ops.push({ kind, text: a[ai], oldLine: ai + 1, newLine: bi + 1 });
      ai += 1;
      bi += 1;
    } else if (kind === 'del') {
      ops.push({ kind, text: a[ai], oldLine: ai + 1, newLine: null });
      ai += 1;
    } else {
      ops.push({ kind, text: b[bi], oldLine: null, newLine: bi + 1 });
      bi += 1;
    }
  }
  return ops;
}

// middleOps diffs the part of the two texts that differs, by longest common
// subsequence, listing removals before additions within each changed run.
function middleOps(a, b) {
  const n = a.length;
  const m = b.length;
  if (!n || !m || (n + 1) * (m + 1) > MAX_LCS_CELLS) {
    return [...Array(n).fill('del'), ...Array(m).fill('add')];
  }
  // lcs[i][j] is the LCS length of a[i:] and b[j:], flattened row by row.
  const width = m + 1;
  const lcs = new Uint32Array((n + 1) * width);
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      lcs[i * width + j] = a[i] === b[j]
        ? lcs[(i + 1) * width + j + 1] + 1
        : Math.max(lcs[(i + 1) * width + j], lcs[i * width + j + 1]);
    }
  }
  const out = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push('context');
      i += 1;
      j += 1;
    } else if (lcs[(i + 1) * width + j] >= lcs[i * width + j + 1]) {
      out.push('del');
      i += 1;
    } else {
      out.push('add');
      j += 1;
    }
  }
  while (i < n) { out.push('del'); i += 1; }
  while (j < m) { out.push('add'); j += 1; }
  return out;
}
