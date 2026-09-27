import { QuestionForm } from '@buildmax/gui';

// QuestionPanel frames the shared AskUser QuestionForm the way an approval
// prompt is framed: floating above the composer of the chat it belongs to.
// keys is false for a panel outside the focused pane, so one key press never
// answers two sessions.
export function QuestionPanel({ request, onAnswer, keys = true }) {
  return (
    <div className="approval-panel question-panel" role="dialog" aria-label="Question from the agent">
      <QuestionForm questions={request.questions ?? []} onAnswer={onAnswer} keys={keys} />
    </div>
  );
}
