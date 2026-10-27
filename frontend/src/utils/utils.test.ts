import { describe, expect, it } from 'vitest';
import { AccountProvider, AccountSource, AccountStatus, Theme } from '../types';
import type { Account } from '../types';
import { accountReady } from './accounts';
import { errorMessage } from './errors';
import { formatBytes } from './formatters';
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
});

describe('errorMessage', () => {
  it('handles errors, strings and objects', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom');
    expect(errorMessage('plain')).toBe('plain');
    expect(errorMessage({ a: 1 })).toBe('{"a":1}');
  });
});

describe('account helpers', () => {
  const now = new Date('2026-10-26T12:00:00Z').getTime();
  const account = (patch: Partial<Account> = {}) =>
    ({
      id: 'a',
      provider: AccountProvider.Muse,
      name: 'A',
      source: AccountSource.Browser,
      status: AccountStatus.SignedIn,
      enabled: true,
      usage: [],
      proxy: '',
      createdAt: '',
      updatedAt: '',
      ...patch,
    }) as Account;

  it('judges readiness from status, switch and pause', () => {
    expect(accountReady(account(), now)).toBe(true);
    expect(accountReady(account({ enabled: false }), now)).toBe(false);
    expect(accountReady(account({ status: AccountStatus.SignedOut }), now)).toBe(false);
    expect(accountReady(account({ pausedUntil: '2026-10-26T13:00:00Z' }), now)).toBe(false);
    expect(accountReady(account({ pausedUntil: '2026-10-26T11:00:00Z' }), now)).toBe(true);
  });
});
