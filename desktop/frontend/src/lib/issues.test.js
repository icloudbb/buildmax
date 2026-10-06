import { describe, expect, it } from 'vitest';
import { translate } from '../i18n';
import { commentAuthorLabel, issueChatPrompt, issueStatusLabel } from './issues';

const t = (key, vars) => translate('en', key, vars);

describe('issueChatPrompt', () => {
  it('carries the issue, its description, and its sub-issues, marked as other people\'s words', () => {
    const text = issueChatPrompt({
      issue: { id: 'i_1', title: 'Fix the importer', status: 'in_progress', space_name: 'Data' },
      description: '  CSV rows with quotes break.  ',
      children: [{ title: 'Add a test', status: 'done' }],
    }, t);
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
    }, t);
    expect(text).not.toContain('Sub-issues');
    expect(text.split('\n\n')).toHaveLength(3);
  });
});

describe('labels', () => {
  it('names statuses and comment authors in words', () => {
    expect(issueStatusLabel('in_progress', t)).toBe('In progress');
    expect(commentAuthorLabel({ author_kind: 'user', mine: true }, t)).toBe('You');
    expect(commentAuthorLabel({ author_kind: 'user', mine: false }, t)).toBe('Member');
    expect(commentAuthorLabel({ author_kind: 'local_agent', mine: false }, t)).toBe('Local agent');
  });

  it('follows the translator, and passes an unknown status through', () => {
    const zh = (key, vars) => translate('zh-CN', key, vars);
    expect(issueStatusLabel('in_progress', zh)).toBe('进行中');
    expect(commentAuthorLabel({ author_kind: 'local_agent' }, zh)).toBe('本地 Agent');
    expect(issueStatusLabel('blocked', zh)).toBe('blocked');
  });
});
