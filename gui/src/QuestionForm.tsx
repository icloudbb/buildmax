import { useEffect, useState, type FormEvent } from "react"
import { useGuiT } from "./messages"

// The shapes mirror agent.Question and agent.QuestionOption on the wire, so a
// surface passes the payload it received straight through.
export interface QuestionOption {
  label: string
  description?: string
}

export interface Question {
  question: string
  header?: string
  options?: QuestionOption[]
  multi_select?: boolean
}

/** One answer per question, in order, or a dismissal of the whole set. */
export type QuestionAnswer = { answers: string[] } | { declined: true }

export interface QuestionFormProps {
  questions: Question[]
  onAnswer: (answer: QuestionAnswer) => void
  // keys binds the digit and Escape shortcuts on the window. A surface showing
  // several forms at once enables it on one, so a key press never answers two.
  keys?: boolean
  title?: string
}

const NO_OPTIONS: QuestionOption[] = []

// A key typed into a text field is text, not a choice: a digit in a composer or
// an answer box must not pick an option.
function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  const tag = el?.tagName
  return tag === "INPUT" || tag === "TEXTAREA" || Boolean(el?.isContentEditable)
}

/**
 * QuestionForm answers an AskUser question set, one question at a time: its
 * options as buttons (checkable when the question is multi-select), a field for
 * an answer of the user's own under the question it answers, and a dismiss for
 * the whole set. Answering moves to the next unanswered question; the set is
 * sent once every question has an answer. Presentation only: the surface frames
 * it and delivers the answer.
 */
export function QuestionForm({ questions, onAnswer, keys = true, title: titleProp }: QuestionFormProps) {
  const t = useGuiT()
  const title = titleProp ?? t("gui.question.title")
  const [current, setCurrent] = useState(0)
  const [answers, setAnswers] = useState<string[]>(() => questions.map(() => ""))
  const [checked, setChecked] = useState<boolean[][]>(() => questions.map((q) => (q.options ?? []).map(() => false)))
  const [texts, setTexts] = useState<string[]>(() => questions.map(() => ""))

  const q = questions[current] ?? { question: "" }
  const options = q.options ?? NO_OPTIONS
  const multi = Boolean(q.multi_select)

  function answer(value: string) {
    const next = answers.map((a, i) => (i === current ? value : a))
    setAnswers(next)
    for (let step = 1; step <= next.length; step++) {
      const i = (current + step) % next.length
      if (!next[i]) {
        setCurrent(i)
        return
      }
    }
    onAnswer({ answers: next })
  }

  function toggle(n: number) {
    setChecked((prev) => prev.map((row, i) => (i === current ? row.map((on, j) => (j === n ? !on : on)) : row)))
  }

  // The checked options plus anything typed, for a multi-select question; the
  // typed text alone otherwise.
  function composed(): string {
    const typed = (texts[current] ?? "").trim()
    if (!multi) return typed
    const picked = options.filter((_, j) => checked[current]?.[j]).map((o) => o.label)
    if (typed) picked.push(typed)
    return picked.join(", ")
  }

  // Re-bound every render so the handler sees the current question.
  useEffect(() => {
    if (!keys) return undefined
    function onKey(e: KeyboardEvent) {
      if (isTyping(e.target)) return
      if (e.key === "Escape") {
        onAnswer({ declined: true })
        return
      }
      const n = Number.parseInt(e.key, 10)
      if (n >= 1 && n <= options.length) {
        if (multi) toggle(n - 1)
        else answer(options[n - 1].label)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    const value = composed()
    if (value) answer(value)
  }

  return (
    <div className="bm-question">
      <div className="bm-question__head">
        <span className="bm-question__title">{title}</span>
        {questions.length > 1 && (
          <div className="bm-question__tabs" role="tablist">
            {questions.map((qq, i) => (
              <button
                key={i}
                type="button"
                role="tab"
                aria-selected={i === current}
                className={`bm-question__tab${i === current ? " bm-question__tab--active" : ""}`}
                onClick={() => setCurrent(i)}
              >
                {qq.header || t("gui.question.tab", { n: i + 1 })}
                {answers[i] ? " ✓" : ""}
              </button>
            ))}
          </div>
        )}
        {questions.length === 1 && q.header && <span className="bm-question__header">{q.header}</span>}
      </div>
      <div className="bm-question__text">{q.question}</div>

      {options.length > 0 && (
        <div className="bm-question__options">
          {options.map((o, i) => (
            <button
              key={`${current}-${o.label}`}
              type="button"
              className={`bm-question__option${multi && checked[current]?.[i] ? " bm-question__option--checked" : ""}`}
              aria-pressed={multi ? Boolean(checked[current]?.[i]) : undefined}
              // The pending question blocks the run, so the keyboard-driving
              // form takes focus to make its answer path pointer-free.
              // eslint-disable-next-line jsx-a11y/no-autofocus
              autoFocus={keys && i === 0}
              onClick={() => (multi ? toggle(i) : answer(o.label))}
            >
              <span className="bm-question__num">{multi ? (checked[current]?.[i] ? "☑" : "☐") : i + 1}</span>
              <span className="bm-question__label">{o.label}</span>
              {o.description && <span className="bm-question__desc">{o.description}</span>}
            </button>
          ))}
        </div>
      )}

      <form className="bm-question__footer" onSubmit={submit}>
        <input
          key={current}
          className="bm-question__input"
          value={texts[current] ?? ""}
          onChange={(e) => setTexts((prev) => prev.map((t, i) => (i === current ? e.target.value : t)))}
          placeholder={options.length > 0 ? t("gui.question.ownAnswer") : t("gui.question.answer")}
          aria-label={t("gui.question.answerLabel")}
          // As above: the blocking question takes focus in keyboard mode.
          // eslint-disable-next-line jsx-a11y/no-autofocus
          autoFocus={keys && options.length === 0}
        />
        <button type="submit" className="bm-question__btn bm-question__btn--primary" disabled={!composed()}>
          {multi ? t("gui.question.confirm") : t("gui.question.send")}
        </button>
        <button type="button" className="bm-question__btn" onClick={() => onAnswer({ declined: true })}>
          {t("gui.question.dismiss")}
        </button>
      </form>
    </div>
  )
}
