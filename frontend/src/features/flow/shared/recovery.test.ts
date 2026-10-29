import { describe, expect, it } from 'vitest';
import { RUN_START, runAge } from '../../../stores';
import { itemRunAge } from './recovery';
import type { RecoverableItem } from './recovery';

const item = (createdAt: string, pending = false) =>
  ({
    id: 'v',
    jobId: 'j',
    prompt: 'p',
    aspectRatio: '16:9',
    createdAt,
    pending,
  }) as RecoverableItem;

describe('itemRunAge', () => {
  const after = new Date(Date.parse(RUN_START) + 60_000).toISOString();
  const before = new Date(Date.parse(RUN_START) - 60_000).toISOString();

  it('counts results made in this run as this run', () => {
    expect(itemRunAge(item(after))).toBe('current');
  });

  it('never hides a pending job or one without a time', () => {
    expect(itemRunAge(item(before, true))).toBe('current');
    expect(itemRunAge(item(''))).toBe('current');
  });
});

describe('runAge', () => {
  const previousStart = '2026-10-27T09:00:00.000Z';
  const start = '2026-10-28T10:00:00.000Z';

  it('offers only the run just before this one', () => {
    expect(runAge('2026-10-27T10:00:00.000Z', true, start, previousStart)).toBe('previous');
    expect(runAge('2026-10-26T10:00:00.000Z', true, start, previousStart)).toBe('older');
    expect(runAge('2026-10-28T11:00:00.000Z', true, start, previousStart)).toBe('current');
  });

  it('has no earlier run on a first start', () => {
    expect(runAge('2026-10-26T10:00:00.000Z', true, start, start)).toBe('older');
  });
});
