import { describe, expect, it } from 'vitest';
import { missingKeys } from '@buildmax/gui';
import { desktopMessages, translate } from '.';

describe('Desktop message catalog', () => {
  it('translates every key into Chinese', () => {
    expect(missingKeys(desktopMessages)).toEqual([]);
  });

  it('keeps placeholders identical across languages', () => {
    const placeholders = (text) => [...new Set(JSON.stringify(text ?? '').match(/\{\w+\}/g))].sort();
    for (const key of Object.keys(desktopMessages.en)) {
      expect(placeholders(desktopMessages['zh-CN'][key]), key).toEqual(placeholders(desktopMessages.en[key]));
    }
  });

  it('chooses the English plural form by count', () => {
    expect(translate('en', 'shell.panes', { count: 1 })).toBe('1 pane');
    expect(translate('en', 'shell.panes', { count: 3 })).toBe('3 panes');
    expect(translate('zh-CN', 'shell.panes', { count: 3 })).toBe('3 个窗格');
  });
});
