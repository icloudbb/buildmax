import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { QuestionForm, type Question } from "./QuestionForm"

afterEach(cleanup);

const db: Question = {
  header: 'Database',
  question: 'Which database?',
  options: [{ label: 'Postgres', description: 'what prod runs' }, { label: 'SQLite' }],
};
const features: Question = {
  header: 'Features',
  question: 'Which features?',
  multi_select: true,
  options: [{ label: 'Auth' }, { label: 'Billing' }, { label: 'Search' }],
};
const name: Question = { header: 'Name', question: 'What is the service called?' };



describe("QuestionForm", () => {
  it('answers a single question with the option clicked', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[db]} onAnswer={onAnswer} />);
    expect(screen.getByText('what prod runs')).toBeTruthy();
    fireEvent.click(screen.getByText('SQLite'));
    expect(onAnswer).toHaveBeenCalledWith({ answers: ['SQLite'] });
  });

  it('picks an option by its number, but not while typing', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[db]} onAnswer={onAnswer} />);
    fireEvent.keyDown(screen.getByLabelText('Your answer'), { key: '2' });
    expect(onAnswer).not.toHaveBeenCalled();
    fireEvent.keyDown(document.body, { key: '2' });
    expect(onAnswer).toHaveBeenCalledWith({ answers: ['SQLite'] });
  });

  it('sends an answer of the user’s own', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[db]} onAnswer={onAnswer} />);
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: ' MySQL 8 ' } });
    fireEvent.click(screen.getByText('Send'));
    expect(onAnswer).toHaveBeenCalledWith({ answers: ['MySQL 8'] });
  });

  it('sends every checked option of a multi-select question', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[features]} onAnswer={onAnswer} />);
    fireEvent.click(screen.getByText('Auth'));
    fireEvent.keyDown(document.body, { key: '3' });
    expect(onAnswer).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('Confirm'));
    expect(onAnswer).toHaveBeenCalledWith({ answers: ['Auth, Search'] });
  });

  it('walks a question set and sends it once every question is answered', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[db, features, name]} onAnswer={onAnswer} />);
    expect(screen.getByRole('tab', { name: 'Database' })).toBeTruthy();
    fireEvent.click(screen.getByText('Postgres'));
    expect(screen.getByText('Which features?')).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Database ✓' })).toBeTruthy();
    fireEvent.click(screen.getByText('Billing'));
    fireEvent.click(screen.getByText('Confirm'));
    expect(onAnswer).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'orders-api' } });
    fireEvent.click(screen.getByText('Send'));
    expect(onAnswer).toHaveBeenCalledWith({ answers: ['Postgres', 'Billing', 'orders-api'] });
  });

  it('dismisses the whole set', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[name, db]} onAnswer={onAnswer} />);
    fireEvent.click(screen.getByText('Dismiss'));
    expect(onAnswer).toHaveBeenCalledWith({ declined: true });
  });

  it('takes no keys outside the focused pane', () => {
    const onAnswer = vi.fn();
    render(<QuestionForm questions={[db]} onAnswer={onAnswer} keys={false} />);
    fireEvent.keyDown(document.body, { key: '1' });
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(onAnswer).not.toHaveBeenCalled();
  });
});
