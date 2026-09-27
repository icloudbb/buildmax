import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { QuestionPanel } from './QuestionPanel';

afterEach(cleanup);

// The form itself is tested in @buildmax/gui; this checks the Desktop frame
// passes the request's questions through and reports the answer.
describe('QuestionPanel', () => {
  it('shows the request’s questions and reports the answer', () => {
    const onAnswer = vi.fn();
    const request = { question_id: '1', session_id: 's1', questions: [{ question: 'Which database?', options: [{ label: 'Postgres' }] }] };
    render(<QuestionPanel request={request} onAnswer={onAnswer} />);
    expect(screen.getByRole('dialog', { name: 'Question from the agent' })).toBeTruthy();
    fireEvent.click(screen.getByText('Postgres'));
    expect(onAnswer).toHaveBeenCalledWith({ answers: ['Postgres'] });
  });
});
