// Pending prompts, one per session: tool approvals and AskUser questions. Each
// desktop/approval-request or desktop/question-request carries the asking run's
// session_id and an id the answer must quote (approval_id, question_id), so
// concurrent sessions of one project each get, and answer, only their own
// prompt. A session runs one turn at a time and a turn waits on one prompt at a
// time, so a session has at most one pending of each kind.

function withRequest(pending, request, idKey) {
  const sid = request?.session_id ?? '';
  if (!sid || !request?.[idKey]) return pending;
  return { ...pending, [sid]: request };
}

function withoutRequest(pending, sessionId, id, idKey) {
  const current = pending[sessionId ?? ''];
  if (!current) return pending;
  if (id && current[idKey] !== id) return pending;
  const next = { ...pending };
  delete next[sessionId];
  return next;
}

// withApproval records a request under its session. A request without a
// session id cannot be routed to a chat tab and is ignored.
export function withApproval(pending, request) {
  return withRequest(pending, request, 'approval_id');
}

// withoutApproval drops a session's pending request. With an approvalId it drops
// only that request, so answering a stale prompt never clears a newer one.
export function withoutApproval(pending, sessionId, approvalId) {
  return withoutRequest(pending, sessionId, approvalId, 'approval_id');
}

// withQuestion and withoutQuestion are the same for AskUser questions.
export function withQuestion(pending, request) {
  return withRequest(pending, request, 'question_id');
}

export function withoutQuestion(pending, sessionId, questionId) {
  return withoutRequest(pending, sessionId, questionId, 'question_id');
}

// waitingOn indexes the sessions, and their projects, that are blocked on the
// user — an approval or a question up — so a surface not showing that chat can
// still say so.
export function waitingOn(...pendings) {
  const sessions = new Set();
  const projects = new Set();
  for (const pending of pendings) {
    for (const request of Object.values(pending ?? {})) {
      if (request?.session_id) sessions.add(request.session_id);
      if (request?.project_id) projects.add(request.project_id);
    }
  }
  return { sessions, projects };
}
