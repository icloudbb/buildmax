// Turning values into the strings the interface shows. Nothing here touches
// React or the Go side, which is why it can be read and changed on its own.
// A function that produces words takes the caller's translator and defaults
// to English.

import { translate } from '../i18n';

const english = (key, vars) => translate('en', key, vars);

export function formatSessionMeta(createdAt, t = english) {
  if (!createdAt) return '';
  try {
    const d = new Date(createdAt);
    const now = new Date();
    const diffMs = now - d;
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);
    if (diffMins < 1) return t('home.age.now');
    if (diffMins < 60) return t('home.age.minutes', { n: diffMins });
    if (diffHours < 24) return t('home.age.hours', { n: diffHours });
    if (diffDays < 7) return t('home.age.days', { n: diffDays });
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  } catch {
    return '';
  }
}

export function compareRecent(a, b) {
  return String(b || '').localeCompare(String(a || ''));
}

export function folderBaseName(path) {
  if (!path) return '';
  return path.split(/[/\\]/).filter(Boolean).pop() ?? path;
}

export function formatToolArgs(raw) {
  if (!raw) return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

export function toolDisplayName(name) {
  const first = String(name || '').split('_', 1)[0];
  if (!first) return String(name || '');
  return first.slice(0, 1).toUpperCase() + first.slice(1);
}

export function shortToolArgs(raw) {
  if (!raw) return '';
  if (raw.length <= 40) return raw;
  try {
    const m = JSON.parse(raw);
    for (const k of ['path', 'file', 'filename', 'command']) {
      if (typeof m[k] === 'string') {
        const v = m[k];
        return v.length > 40 ? v.slice(0, 37) + '…' : v;
      }
    }
    for (const v of Object.values(m)) {
      if (typeof v === 'string') return v.length > 40 ? v.slice(0, 37) + '…' : v;
    }
  } catch {
    // Not JSON — fall through to the raw truncation below.
  }
  return raw.slice(0, 37) + '…';
}

export function formatTokenCount(n) {
  const value = Number(n) || 0;
  if (value < 1000) return String(value);
  if (value % 1000 === 0) return `${value / 1000}k`;
  return `${(value / 1000).toFixed(1)}k`;
}

export function formatBytes(n) {
  const value = Number(n) || 0;
  if (value >= 1 << 20) return `${(value / (1 << 20)).toFixed(1)} MB`;
  if (value >= 1 << 10) return `${(value / (1 << 10)).toFixed(1)} KB`;
  return `${value} B`;
}

export function statusGlyph(status) {
  switch (status) {
    case 'added': return '+';
    case 'deleted': return '-';
    case 'renamed': return '↔';
    default: return '●';
  }
}

export function statusTitle(status) {
  switch (status) {
    case 'added': return 'Added';
    case 'deleted': return 'Deleted';
    case 'renamed': return 'Renamed';
    default: return 'Modified';
  }
}

export function displayDiffPath(file) {
  if (file?.status === 'renamed' && file.old_path) return `${file.old_path} → ${file.path}`;
  return file?.path ?? '';
}

export function splitPathForDisplay(path) {
  const value = String(path ?? '');
  const parts = value.split(/[/\\]/);
  const name = parts.pop() || value;
  const dir = parts.length ? `${parts.join('/')}/` : '';
  return { dir, name };
}

export function truncateMiddleText(value, max = 34) {
  const text = String(value ?? '');
  if (text.length <= max) return text;
  if (max <= 1) return '…';
  const head = Math.floor((max - 1) / 2);
  const tail = max - 1 - head;
  return `${text.slice(0, head)}…${text.slice(text.length - tail)}`;
}

const byNodeName = (a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase());

// buildDiffTree groups a flat list of changed files into a directory tree for
// the diff sidebar's tree view. Nodes are either { type: 'dir', name, path,
// children } or { type: 'file', name, file } (file is the original changed-file
// object). Directories sort before files, each case-insensitively by name. A
// directory whose only content is a single subdirectory is merged into one
// "a/b/c" row (compact folders, as common editors do), so a deep refactor does
// not render as a column of near-empty nesting.
export function buildDiffTree(files) {
  const root = { dirs: new Map(), files: [] };
  for (const file of files ?? []) {
    const segments = String(file?.path ?? '').split('/');
    const name = segments.pop() || String(file?.path ?? '');
    let node = root;
    let acc = '';
    for (const segment of segments) {
      if (!segment) continue;
      acc = acc ? `${acc}/${segment}` : segment;
      let child = node.dirs.get(segment);
      if (!child) {
        child = { name: segment, path: acc, dirs: new Map(), files: [] };
        node.dirs.set(segment, child);
      }
      node = child;
    }
    node.files.push({ type: 'file', name, file });
  }
  return finalizeDiffChildren(root);
}

function finalizeDiffChildren(node) {
  const dirs = [...node.dirs.values()].map(finalizeDiffDir).sort(byNodeName);
  const fileNodes = node.files.slice().sort(byNodeName);
  return [...dirs, ...fileNodes];
}

function finalizeDiffDir(node) {
  let dir = { type: 'dir', name: node.name, path: node.path, children: finalizeDiffChildren(node) };
  while (dir.children.length === 1 && dir.children[0].type === 'dir') {
    const only = dir.children[0];
    dir = { type: 'dir', name: `${dir.name}/${only.name}`, path: only.path, children: only.children };
  }
  return dir;
}

export function parseRangeStart(token) {
  const n = Number.parseInt(token.replace(/^[-+]/, '').split(',')[0], 10);
  return Number.isFinite(n) ? n : 0;
}

export function parsePatchLines(patch) {
  const rows = [];
  let oldLine = 0;
  let newLine = 0;
  let hunk = -1;
  for (const raw of String(patch ?? '').replace(/\r\n/g, '\n').split('\n')) {
    if (raw.startsWith('@@')) {
      const parts = raw.split(/\s+/);
      oldLine = parseRangeStart(parts.find((p) => p.startsWith('-')) ?? '');
      newLine = parseRangeStart(parts.find((p) => p.startsWith('+')) ?? '');
      hunk += 1;
      rows.push({ kind: 'hunk', text: raw, oldLine: '', newLine: '', hunk });
      continue;
    }
    if (raw.startsWith('diff --git') || raw.startsWith('index ') || raw.startsWith('--- ') || raw.startsWith('+++ ')) {
      rows.push({ kind: 'header', text: raw, oldLine: '', newLine: '', hunk });
      continue;
    }
    if (raw.startsWith('+')) {
      rows.push({ kind: 'add', text: raw, oldLine: '', newLine: newLine || '', hunk });
      newLine += 1;
      continue;
    }
    if (raw.startsWith('-')) {
      rows.push({ kind: 'del', text: raw, oldLine: oldLine || '', newLine: '', hunk });
      oldLine += 1;
      continue;
    }
    rows.push({ kind: 'context', text: raw, oldLine: oldLine || '', newLine: newLine || '', hunk });
    if (oldLine) oldLine += 1;
    if (newLine) newLine += 1;
  }
  return rows;
}

// Attaches Shiki token lines to each add/del/context row of a parsed patch,
// grouped and tokenized per hunk (rather than per isolated line) so multi-line
// constructs like block comments and strings get correct context. Rows whose
// kind isn't add/del/context (header, hunk marker) are left untouched. Returns
// a new array; rows get a `tokens` field, `undefined` when unavailable.
export async function highlightDiffRows(rows, path, theme, highlightToLines) {
  const byHunk = new Map();
  for (const row of rows) {
    if (row.kind !== 'add' && row.kind !== 'del' && row.kind !== 'context') continue;
    if (!byHunk.has(row.hunk)) byHunk.set(row.hunk, []);
    byHunk.get(row.hunk).push(row);
  }

  const tokensByRow = new Map();
  for (const hunkRows of byHunk.values()) {
    const oldRows = hunkRows.filter((r) => r.kind === 'del' || r.kind === 'context');
    const newRows = hunkRows.filter((r) => r.kind === 'add' || r.kind === 'context');
    const oldText = oldRows.map((r) => r.text.slice(1)).join('\n');
    const newText = newRows.map((r) => r.text.slice(1)).join('\n');
    const [oldLines, newLines] = await Promise.all([
      oldText ? highlightToLines(oldText, path, theme) : [],
      newText ? highlightToLines(newText, path, theme) : [],
    ]);
    oldRows.forEach((r, i) => { if (r.kind === 'del') tokensByRow.set(r, oldLines[i]); });
    newRows.forEach((r, i) => { if (r.kind !== 'del') tokensByRow.set(r, newLines[i]); });
  }

  return rows.map((row) => (tokensByRow.has(row) ? { ...row, tokens: tokensByRow.get(row) } : row));
}
