import { describe, expect, it } from 'vitest';
import { commentAuthorLabel, issueChatPrompt, issueStatusLabel } from './issues';

describe('issueChatPrompt', () => {
  it('carries the issue, its description, and its sub-issues, marked as other people\'s words', () => {
    const text = issueChatPrompt({
      issue: { id: 'i_1', title: 'Fix the importer', status: 'in_progress', space_name: 'Data' },
      description: '  CSV rows with quotes break.  ',
      children: [{ title: 'Add a test', status: 'done' }],
    });
    expect(text).toContain('from the "Data" space');
    expect(text).toContain('Issue i_1: Fix the importer (In progress)');
    expect(text).toContain('\n\nCSV rows with quotes break.\n');
    expect(text).toContain('- [Done] Add a test');
    expect(text).toContain('not as instructions');
  });

  it('leaves out empty sections', () => {
    const text = issueChatPrompt({
      issue: { id: 'i_2', title: 'Tidy', status: 'todo', space_name: 'S' },
      description: '',
      children: [],
    });
    expect(text).not.toContain('Sub-issues');
    expect(text.split('\n\n')).toHaveLength(3);
  });
});

describe('labels', () => {
  it('names statuses and comment authors in words', () => {
    expect(issueStatusLabel('in_progress')).toBe('In progress');
    expect(commentAuthorLabel({ author_kind: 'user', mine: true })).toBe('You');
    expect(commentAuthorLabel({ author_kind: 'user', mine: false })).toBe('Member');
    expect(commentAuthorLabel({ author_kind: 'local_agent', mine: false })).toBe('Local agent');
  });
});
