import { describe, expect, it } from 'vitest';
import { withApproval, withoutApproval, withQuestion, withoutQuestion } from './approvals';

const reqA = { approval_id: '1', session_id: 'sA', tool_name: 'Write' };
const reqB = { approval_id: '2', session_id: 'sB', tool_name: 'Bash' };

describe('pending approvals', () => {
  it('keeps concurrent sessions’ requests apart', () => {
    const pending = withApproval(withApproval({}, reqA), reqB);
    expect(pending).toEqual({ sA: reqA, sB: reqB });
  });

  it('ignores a request that cannot be routed or answered', () => {
    expect(withApproval({}, { approval_id: '1', session_id: '' })).toEqual({});
    expect(withApproval({}, { session_id: 'sA' })).toEqual({});
  });

  it('clears only the answered session', () => {
    const pending = withApproval(withApproval({}, reqA), reqB);
    expect(withoutApproval(pending, 'sA', '1')).toEqual({ sB: reqB });
  });

  it('does not let a stale answer clear a newer request', () => {
    const newer = { approval_id: '3', session_id: 'sA', tool_name: 'Edit' };
    const pending = withApproval(withApproval({}, reqA), newer);
    expect(withoutApproval(pending, 'sA', '1')).toBe(pending);
  });

  it('drops a session’s request when its run ends', () => {
    const pending = withApproval(withApproval({}, reqA), reqB);
    expect(withoutApproval(pending, 'sB')).toEqual({ sA: reqA });
    expect(withoutApproval(pending, 'other')).toBe(pending);
  });
});

describe('pending questions', () => {
  const qA = { question_id: '1', session_id: 'sA', questions: [{ question: 'Which DB?' }] };

  it('routes a question by session and clears it only by its own id', () => {
    const pending = withQuestion({}, qA);
    expect(pending).toEqual({ sA: qA });
    expect(withoutQuestion(pending, 'sA', '2')).toBe(pending);
    expect(withoutQuestion(pending, 'sA', '1')).toEqual({});
  });

  it('ignores a question without a question id', () => {
    expect(withQuestion({}, { approval_id: '1', session_id: 'sA' })).toEqual({});
  });
});
