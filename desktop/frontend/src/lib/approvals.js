// Pending tool approvals, one per session. Each desktop/approval-request carries
// the asking run's session_id and an approval_id the answer must quote, so
// concurrent sessions of one project each get, and answer, only their own
// prompt. A session runs one turn at a time, so it has at most one pending.

// withApproval records a request under its session. A request without a
// session id cannot be routed to a chat tab and is ignored.
export function withApproval(pending, request) {
  const sid = request?.session_id ?? '';
  if (!sid || !request?.approval_id) return pending;
  return { ...pending, [sid]: request };
}

// withoutApproval drops a session's pending request. With an approvalId it drops
// only that request, so answering a stale prompt never clears a newer one.
export function withoutApproval(pending, sessionId, approvalId) {
  const current = pending[sessionId ?? ''];
  if (!current) return pending;
  if (approvalId && current.approval_id !== approvalId) return pending;
  const next = { ...pending };
  delete next[sessionId];
  return next;
}
