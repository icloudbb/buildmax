// What an Edit or Write approval would do to its file, worked out from the
// tool's arguments and the file's current state, which the approval request
// carries as `file`. The arithmetic mirrors the tools (internal/tool
// edit_file.go and write_file.go) so the prompt shows the change the tool will
// make, or says plainly that it will fail.

// proposedChange returns null for any other tool, otherwise
// { effect, path, before, after, plain, replaceAll, reason }:
// - effect: 'edit', 'create', 'overwrite', 'empty', 'write' (a Write whose
//   file state is unknown), or, for an Edit that cannot apply,
//   'edit-missing', 'edit-ambiguous', or 'edit-no-file'.
// - before/after: the texts to diff.
// - plain: true when before/after are the arguments alone rather than whole
//   files, so line numbers would mislead.
// - reason: why the current content is not shown (the request's
//   file.unavailable), or ''.
export function proposedChange(request) {
  const tool = request?.tool_name;
  if (tool !== 'Edit' && tool !== 'Write') return null;
  const args = request.args ?? {};
  const file = request.file ?? null;
  const path = file?.path || String(args.file_path ?? '');
  const reason = file ? (file.unavailable ?? '') : 'unreadable';
  const known = Boolean(file) && !reason;
  const current = known ? normalize(file.content) : '';

  if (tool === 'Write') {
    const content = String(args.content ?? '');
    const exists = Boolean(file?.exists);
    let effect = 'write'; // nothing is known about the file
    if (file) effect = !exists ? 'create' : content === '' ? 'empty' : 'overwrite';
    // Without the current content an overwrite can only show what is written.
    return { effect, path, before: known ? current : '', after: content, plain: !known && effect !== 'create', replaceAll: false, reason };
  }

  const oldString = normalize(args.old_string);
  const newString = normalize(args.new_string);
  const replaceAll = args.replace_all === true;
  const fallback = { path, before: oldString, after: newString, plain: true, replaceAll, reason };
  if (file && !file.exists && !reason) return { ...fallback, effect: 'edit-no-file' };
  if (!known) return { ...fallback, effect: 'edit' };

  const count = occurrences(current, oldString);
  if (count === 0) return { ...fallback, effect: 'edit-missing' };
  if (count > 1 && !replaceAll) return { ...fallback, effect: 'edit-ambiguous' };
  // A replacer function: a string replacement would expand `$&` and the like.
  const after = replaceAll
    ? current.replaceAll(oldString, () => newString)
    : current.replace(oldString, () => newString);
  return { effect: 'edit', path, before: current, after, plain: false, replaceAll, reason: '' };
}

function normalize(text) {
  return String(text ?? '').replace(/\r\n/g, '\n');
}

// occurrences counts as Go's strings.Count does, including an empty needle.
function occurrences(haystack, needle) {
  if (needle === '') return haystack.length + 1;
  return haystack.split(needle).length - 1;
}
