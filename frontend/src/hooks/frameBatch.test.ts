import { describe, expect, it, vi } from 'vitest';
import { collectByIdOnNextFrame, onNextFrame } from './frameBatch';

describe('onNextFrame', () => {
  it('applies everything scheduled in one frame, in order', () => {
    vi.useFakeTimers();
    const seen: number[] = [];
    onNextFrame(() => seen.push(1));
    onNextFrame(() => seen.push(2));
    expect(seen).toEqual([]);
    vi.advanceTimersByTime(16);
    expect(seen).toEqual([1, 2]);
    onNextFrame(() => seen.push(3));
    vi.advanceTimersByTime(16);
    expect(seen).toEqual([1, 2, 3]);
    vi.useRealTimers();
  });

  it('keeps the newest event per ID in the original insertion order', () => {
    vi.useFakeTimers();
    const applied = vi.fn();
    const collect = collectByIdOnNextFrame<string | null>(applied);
    collect('first', 'queued');
    collect('second', 'queued');
    collect('first', 'done');
    collect('second', null);
    vi.advanceTimersByTime(16);
    expect(applied).toHaveBeenCalledExactlyOnceWith(
      new Map([
        ['first', 'done'],
        ['second', null],
      ]),
    );
    vi.useRealTimers();
  });

  it('supports flush, clear(id), and clearAll without stale frame rollback', () => {
    vi.useFakeTimers();
    const applied = vi.fn();
    const collect = collectByIdOnNextFrame<string | null>(applied);

    collect('job-1', 'processing');
    collect('job-2', 'processing');
    // Direct RPC cancels job-1 before the next frame -> clear drops stale event.
    collect.clear('job-1');
    vi.advanceTimersByTime(16);
    expect(applied).toHaveBeenCalledExactlyOnceWith(new Map([['job-2', 'processing']]));

    applied.mockClear();
    collect('job-3', 'queued');
    // Synchronous flush commits immediately; subsequent rAF tick is a no-op.
    collect.flush();
    expect(applied).toHaveBeenCalledExactlyOnceWith(new Map([['job-3', 'queued']]));
    vi.advanceTimersByTime(16);
    expect(applied).toHaveBeenCalledTimes(1);

    applied.mockClear();
    collect('job-4', 'queued');
    collect.clearAll();
    vi.advanceTimersByTime(16);
    expect(applied).not.toHaveBeenCalled();
    vi.useRealTimers();
  });
});
