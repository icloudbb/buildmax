// Pure helpers for the Issues view (components/IssuesView.jsx). Display text
// comes from the caller's translator `t`.

export const ISSUE_STATUSES = ['todo', 'in_progress', 'done'];

export function issueStatusLabel(status, t) {
  if (ISSUE_STATUSES.includes(status)) return t(`issues.status.${status}`);
  return status ?? '';
}

// commentAuthorLabel names who wrote a comment. The server returns author kinds,
// not member names, so another person reads as "Member"; "Local agent" is a
// report a session posted under someone's login, which is not the person.
export function commentAuthorLabel(comment, t) {
  if (comment.author_kind === 'user') return t(comment.mine ? 'issues.author.you' : 'issues.author.member');
  if (comment.author_kind === 'agent') return t('issues.author.agent');
  if (comment.author_kind === 'local_agent') return t('issues.author.localAgent');
  if (comment.author_kind === 'system') return t('issues.author.system');
  return comment.author_kind || t('issues.author.unknown');
}

// issueChatPrompt is the draft a new chat starts from. It is the whole hand-off
// from an Issue to a local session: visible and editable before anything is
// sent, and part of the conversation afterwards, so resuming the chat keeps it.
// It is the person's own message, so it is drafted in their interface language.
// The Issue's text is marked as other people's words, the same caution the
// CLI's issue prompt layer gives an Agent.
export function issueChatPrompt(detail, t) {
  const { issue, description, children } = detail;
  const lines = [
    t('issues.prompt.intro', { space: issue.space_name }),
    '',
    t('issues.prompt.heading', { id: issue.id, title: issue.title, status: issueStatusLabel(issue.status, t) }),
  ];
  const body = (description ?? '').trim();
  if (body) lines.push('', body);
  if (children?.length) {
    lines.push('', t('issues.prompt.subIssues'));
    for (const child of children) {
      lines.push(t('issues.prompt.child', { status: issueStatusLabel(child.status, t), title: child.title }));
    }
  }
  lines.push('', t('issues.prompt.caution'));
  return lines.join('\n');
}
