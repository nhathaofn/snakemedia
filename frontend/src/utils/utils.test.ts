import { describe, expect, it } from 'vitest';
import { Theme } from '../types';
import { errorMessage } from './errors';
import { formatBytes, truncate } from './formatters';
import { resolveTheme } from './theme';
import { THEME_OPTIONS } from '../constants';

describe('theme', () => {
  it('resolves system theme from OS preference', () => {
    expect(resolveTheme(Theme.System, true)).toBe(Theme.Dark);
    expect(resolveTheme(Theme.System, false)).toBe(Theme.Light);
    expect(resolveTheme(Theme.Light, true)).toBe(Theme.Light);
  });

  it('offers exactly one settings option per backend theme', () => {
    const offered = THEME_OPTIONS.map((o) => o.value).sort();
    expect(offered).toEqual(Object.values(Theme).sort());
  });
});

describe('formatters', () => {
  it('formats bytes', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1536, 1)).toBe('1.5 KB');
  });

  it('truncates long text only', () => {
    expect(truncate('short', 10)).toBe('short');
    expect(truncate('a long sentence', 6)).toBe('a long...');
  });
});

describe('errorMessage', () => {
  it('handles errors, strings and objects', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom');
    expect(errorMessage('plain')).toBe('plain');
    expect(errorMessage({ a: 1 })).toBe('{"a":1}');
  });
});
