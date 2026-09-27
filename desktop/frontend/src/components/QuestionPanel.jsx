import { useEffect, useState } from 'react';

// A key typed into a text field is text, not a choice: a digit in the composer
// or an answer box must not pick an option.
function isTyping(target) {
  const tag = target?.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || target?.isContentEditable;
}

// QuestionPanel shows an AskUser question set, one question at a time: its
// options as buttons (checkable when the question is multi-select), a box for an
// answer of the user's own under the question it answers, and a dismiss for the
// whole set. Answering a question moves to the next unanswered one; the set is
// sent once every question has an answer. Keys mirror the TUI: a digit picks or
// checks an option, Escape dismisses. keys is false for a panel outside the
// focused pane, so one key press never answers two sessions.
export function QuestionPanel({ request, onAnswer, keys = true }) {
  const questions = request.questions ?? [];
  const [current, setCurrent] = useState(0);
  const [answers, setAnswers] = useState(() => questions.map(() => ''));
  const [checked, setChecked] = useState(() => questions.map((q) => (q.options ?? []).map(() => false)));
  const [texts, setTexts] = useState(() => questions.map(() => ''));

  const q = questions[current] ?? { options: [] };
  const options = q.options ?? [];
  const multi = Boolean(q.multi_select);

  function answer(value) {
    const next = answers.map((a, i) => (i === current ? value : a));
    setAnswers(next);
    for (let step = 1; step <= next.length; step++) {
      const i = (current + step) % next.length;
      if (!next[i]) {
        setCurrent(i);
        return;
      }
    }
    onAnswer({ answers: next });
  }

  function toggle(n) {
    setChecked((prev) => prev.map((row, i) => (i === current ? row.map((on, j) => (j === n ? !on : on)) : row)));
  }

  // The checked options plus anything typed, for a multi-select question; the
  // typed text alone otherwise.
  function composed() {
    const typed = texts[current].trim();
    if (!multi) return typed;
    const picked = options.filter((_, j) => checked[current][j]).map((o) => o.label);
    if (typed) picked.push(typed);
    return picked.join(', ');
  }

  // Re-bound every render so the handler sees the current question.
  useEffect(() => {
    if (!keys) return undefined;
    function onKey(e) {
      if (isTyping(e.target)) return;
      if (e.key === 'Escape') {
        onAnswer({ declined: true });
        return;
      }
      const n = Number.parseInt(e.key, 10);
      if (n >= 1 && n <= options.length) {
        if (multi) toggle(n - 1);
        else answer(options[n - 1].label);
      }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  function submit(e) {
    e.preventDefault();
    const value = composed();
    if (value) answer(value);
  }

  return (
    <div className="approval-panel question-panel" role="dialog" aria-label="Question from the agent">
      <div className="approval-panel__header">
        <span className="approval-panel__title question-panel__title">Question</span>
        {questions.length > 1 && (
          <div className="question-panel__tabs" role="tablist">
            {questions.map((qq, i) => (
              <button
                key={i}
                type="button"
                role="tab"
                aria-selected={i === current}
                className={`question-panel__tab${i === current ? ' question-panel__tab--active' : ''}`}
                onClick={() => setCurrent(i)}
              >
                {qq.header || `Q${i + 1}`}{answers[i] ? ' ✓' : ''}
              </button>
            ))}
          </div>
        )}
        {questions.length === 1 && q.header && <span className="question-panel__header">{q.header}</span>}
      </div>
      <div className="question-panel__text">{q.question}</div>

      {options.length > 0 && (
        <div className="question-panel__options">
          {options.map((o, i) => (
            <button
              key={`${current}-${o.label}`}
              type="button"
              className={`question-panel__option${multi && checked[current][i] ? ' question-panel__option--checked' : ''}`}
              aria-pressed={multi ? checked[current][i] : undefined}
              autoFocus={keys && i === 0}
              onClick={() => (multi ? toggle(i) : answer(o.label))}
            >
              <span className="question-panel__num">{multi ? (checked[current][i] ? '☑' : '☐') : i + 1}</span>
              <span className="question-panel__label">{o.label}</span>
              {o.description && <span className="question-panel__desc">{o.description}</span>}
            </button>
          ))}
        </div>
      )}

      <form className="question-panel__footer" onSubmit={submit}>
        <input
          key={current}
          className="question-panel__input"
          value={texts[current]}
          onChange={(e) => setTexts((prev) => prev.map((t, i) => (i === current ? e.target.value : t)))}
          placeholder={options.length > 0 ? 'Or type your own answer…' : 'Type your answer…'}
          aria-label="Your answer"
          autoFocus={keys && options.length === 0}
        />
        <button type="submit" className="approval-panel__btn approval-panel__btn--allow question-panel__send" disabled={!composed()}>
          {multi ? 'Confirm' : 'Send'}
        </button>
        <button type="button" className="approval-panel__btn approval-panel__btn--muted" onClick={() => onAnswer({ declined: true })}>
          Dismiss
        </button>
      </form>
    </div>
  );
}
