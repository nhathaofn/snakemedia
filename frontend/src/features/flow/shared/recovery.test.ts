import { describe, expect, it } from 'vitest';
import { RUN_START } from '../../../stores';
import { fromPreviousRun } from './recovery';
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

describe('fromPreviousRun', () => {
  const before = new Date(Date.parse(RUN_START) - 60_000).toISOString();
  const after = new Date(Date.parse(RUN_START) + 60_000).toISOString();

  it('hides finished results made before this run', () => {
    expect(fromPreviousRun(item(before))).toBe(true);
    expect(fromPreviousRun(item(after))).toBe(false);
  });

  it('never hides a pending job or one without a time', () => {
    expect(fromPreviousRun(item(before, true))).toBe(false);
    expect(fromPreviousRun(item(''))).toBe(false);
  });
});
