import { describe, expect, it } from 'vitest';
import { FORK, REWIND, consequenceText, moveReport, pointLabel, visiblePoints } from './HistoryModal';
import { translate } from '../i18n';

const t = (key, vars) => translate('en', key, vars);

const head = { item_id: 'i3', role: 'assistant', content: 'you are welcome', messages: 0 };
const afterTool = { item_id: 'i2', role: 'user', content: 'thanks', messages: 2 };
const beforeTool = {
  item_id: 'i1', role: 'user', content: 'write notes.txt', messages: 4,
  tools: [{ name: 'Write', interrupted: false }],
};
const points = { rewind: [beforeTool, afterTool], fork: [beforeTool, afterTool, head] };

describe('visiblePoints', () => {
  // The Go side decides which messages each operation may be pointed at: a
  // rewind hands a prompt back, a fork branches from any turn's last message,
  // the newest included.
  it('gives each tab the list computed for it', () => {
    expect(visiblePoints(points, REWIND)).toEqual([beforeTool, afterTool]);
    expect(visiblePoints(points, FORK)).toHaveLength(3);
  });

  it('survives a session with no points', () => {
    expect(visiblePoints(null, REWIND)).toEqual([]);
    expect(visiblePoints({}, FORK)).toEqual([]);
  });
});

describe('pointLabel', () => {
  it('does not call a background event the user words', () => {
    expect(pointLabel({ role: 'user' }, t)).toBe('you');
    expect(pointLabel({ role: 'event' }, t)).toBe('event');
    expect(pointLabel({ role: 'assistant' }, t)).toBe('agent');
  });
});

describe('consequenceText', () => {
  it('counts what a rewind removes and names what it leaves behind', () => {
    expect(consequenceText(beforeTool, REWIND, t))
      .toBe('prompt comes back · removes 4 messages · leaves in place: Write');
    expect(consequenceText(afterTool, REWIND, t))
      .toBe('prompt comes back · removes 2 messages · nothing outside the conversation ran');
  });

  // A fork takes nothing from the original, so counting losses would be false.
  // The same span still matters, from the other side: the copy will not know
  // that work happened.
  it('frames a fork as what the new session will not know, never as loss', () => {
    const got = consequenceText(beforeTool, FORK, t);
    expect(got).toBe('new session starts here · will not know about: Write');
    expect(got).not.toContain('removes');
  });

  it('says a fork from the head simply copies the conversation', () => {
    expect(consequenceText(head, FORK, t)).toBe('copies this conversation up to here into a new session');
  });

  it('marks a tool that never reported back', () => {
    const interrupted = { messages: 1, tools: [{ name: 'Bash', interrupted: true }] };
    expect(consequenceText(interrupted, REWIND, t)).toContain('Bash (interrupted)');
  });
});

describe('moveReport', () => {
  it('warns after a rewind that the effects are still in place', () => {
    const got = moveReport({ tools: [{ name: 'Write' }] }, REWIND, false, t);
    expect(got).toContain('Write');
    expect(got).toContain('does not undo');
  });

  // The rewind wording would be wrong here: nothing was undone or dropped.
  it('says after a fork that the original is unchanged', () => {
    const got = moveReport({ tools: [{ name: 'Write' }] }, FORK, false, t);
    expect(got).toContain('unchanged');
    expect(got).toContain('Write');
    expect(got).not.toContain('does not undo');
  });

  it('says plainly when nothing outside the conversation ran', () => {
    expect(moveReport({}, REWIND, false, t)).toContain('nothing left over');
    expect(moveReport({}, FORK, false, t)).toContain('unchanged');
  });

  // The user is looking at a composer, so where the prompt went is not
  // something to leave them guessing.
  it('accounts for the prompt a rewind took back', () => {
    expect(moveReport({ prompt: 'try again' }, REWIND, true, t)).toContain('back in the composer');
    expect(moveReport({ prompt: 'try again' }, REWIND, false, t)).toContain('already held a draft');
    expect(moveReport({ prompt: 'look', attachments: 2 }, REWIND, true, t)).toContain('2 images did not come back');
    // A fork takes nothing back, so it says nothing about a composer.
    expect(moveReport({ prompt: 'try again' }, FORK, false, t)).not.toContain('composer');
  });
});
