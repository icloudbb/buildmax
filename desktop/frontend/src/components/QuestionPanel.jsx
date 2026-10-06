import { QuestionForm } from '@buildmax/gui';
import { useT } from '../i18n';

// QuestionPanel frames the shared AskUser QuestionForm the way an approval
// prompt is framed: floating above the composer of the chat it belongs to.
// keys is false for a panel outside the focused pane, so one key press never
// answers two sessions.
export function QuestionPanel({ request, onAnswer, keys = true }) {
  const t = useT();
  return (
    <div className="approval-panel question-panel" role="dialog" aria-label={t('chat.question')}>
      <QuestionForm questions={request.questions ?? []} onAnswer={onAnswer} keys={keys} />
    </div>
  );
}
